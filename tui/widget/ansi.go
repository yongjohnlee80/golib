package widget

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui/style"
)

// ANSI SGR Stream Interpreter & Escape Filter Architecture
//
// sgrInterp implements a streaming, chunk-boundary-resilient ECMA-48 / ANSI SGR
// (Select Graphic Rendition) interpreter specifically optimized for terminal log
// streaming and command output pagers (e.g. [BufferView]).
//
// # Subsystem Role & Responsibilities
//
//  1. ANSI SGR Parsing: Interprets standard 16-color ANSI, 256-color palette (38;5 / 48;5),
//     24-bit TrueColor RGB (38;2;r;g;b / 48;2;r;g;b), and text attribute modifications
//     (bold, faint, italic, underline, blink, reverse, strikethrough).
//  2. Escape Stripping: Strips terminal control sequences (cursor repositioning, window
//     title OSC strings, private DEC modes, bracketed paste toggles) that could corrupt
//     the TUI display canvas.
//  3. Chunk & Fragment Boundary Resilience: Safely buffers split escape sequences across
//     incoming I/O chunk boundaries (e.g. "\x1b[" delivered in chunk 1, "31m" in chunk 2).
//  4. Bounded Allocation & Denial-of-Service Defense: Caps pending escape sequences at
//     [maxEscape] bytes (128 bytes). Malformed or endless escape streams are dropped
//     without unbounded memory growth.
//  5. Terminal Carriage Return (\r) State Machine: Implements terminal-style progress
//     bar overwrites where a bare '\r' triggers line replacement ([sgrCarriage]), while
//     '\r\n' is preserved as a standard newline ([sgrNewline]).
//
// # Data Flow Pipeline
//
//	Incoming Raw Stream Chunks ([]byte from io.Reader)
//	                      │
//	                      ▼
//	               sgrInterp.feed(chunk, emit)
//	                      │
//	       ┌──────────────┴──────────────┐
//	       ▼                             ▼
//	Regular Text & C0            Escape Sequence (0x1b)
//	       │                             │
//	       ├── Bare '\r' ──> sgrCarriage ├── CSI '[' ──> Parse SGR / Strip
//	       ├── '\n'      ──> sgrNewline  ├── OSC ']' ──> Strip BEL / ST
//	       ├── '\t'      ──> 4 Spaces    └── 2-Byte  ──> Strip (ESC c, etc.)
//	       └── Printable ──> sgrText(text, style)
//
// # Architectural Invariants
//
//  1. Pure Loop-Goroutine Concurrency Contract:
//     sgrInterp is strictly loop-goroutine-owned and stateful. It is called from the
//     main application loop during [BufferView.ingest] and contains no locks.
//     Thread-safe ingestion from background goroutines is handled by [bufWriter].
//  2. Bounded Memory Invariant:
//     The pending escape buffer never exceeds [maxEscape] (128 bytes). Sequences
//     exceeding this limit reset parser mode to [escNone] and discard accumulated bytes.
//  3. Clean State Reset on Clear:
//     Resetting style via SGR 0 ('\x1b[0m' or bare '\x1b[m') clears [style.Style] back
//     to its empty zero-value, matching terminal default attributes.
//  4. Separation of Escape and UTF-8 Boundaries:
//     The interpreter buffers split escape fragments ([sgrInterp.esc]). Partial multi-byte
//     UTF-8 rune fragments are carried by the caller ([BufferView.utf8Tail]) to maintain
//     clear separation of concerns.
//
// # Usage Examples
//
//  1. Basic streaming parser usage:
//
//     interp := &sgrInterp{passthrough: true}
//     interp.feed([]byte("\x1b[1;32mSUCCESS\x1b[0m: done\n"), func(ev sgrEvent) {
//     switch ev.kind {
//     case sgrText:
//     // ev.text contains text; ev.st carries applied Style (Green + Bold)
//     renderText(ev.text, ev.st)
//     case sgrNewline:
//     advanceLine()
//     case sgrCarriage:
//     rewindCurrentLine()
//     }
//     })
//
//  2. Disabling ANSI color passthrough (strip all styling):
//
//     // passthrough=false strips escape codes without applying SGR styling:
//     cleanInterp := &sgrInterp{passthrough: false}
//     cleanInterp.feed([]byte("\x1b[31mError text\x1b[0m"), func(ev sgrEvent) {
//     // ev.st is style.Style{} (empty), ev.text is "Error text"
//     })
type sgrInterp struct {
	// passthrough controls whether SGR styles are captured (true) or stripped (false).
	// loop-goroutine-owned: immutable during feed execution.
	passthrough bool

	// st represents the active style state accumulated across SGR sequences.
	// loop-goroutine-owned.
	st style.Style

	// esc holds partially ingested escape sequence bytes across chunk boundaries.
	// loop-goroutine-owned: bounded by maxEscape.
	esc []byte

	// mode tracks the active escape parser state machine (escNone, escStarted, escCSI, escOSC).
	// loop-goroutine-owned.
	mode escMode

	// pendingCR indicates a bare '\r' was seen; retained across chunk boundaries
	// to fold '\r\n' into a single sgrNewline, or resolved to sgrCarriage upon
	// encountering any non-'\n' byte.
	// loop-goroutine-owned.
	pendingCR bool
}

type escMode uint8

const (
	escNone escMode = iota
	escStarted
	escCSI
	escOSC
)

// maxEscape bounds one pending escape sequence.
const maxEscape = 128

// sgrEvent is one interpreter output unit emitted to the feed callback.
type sgrEvent struct {
	kind sgrKind
	text string      // text payload when kind == sgrText
	st   style.Style // visual style when kind == sgrText
}

// sgrKind enumerates the distinct output tokens produced by sgrInterp.
type sgrKind uint8

const (
	// sgrText denotes a span of text rendered with the associated style.Style.
	sgrText sgrKind = iota

	// sgrNewline denotes a line break ('\n' or '\r\n').
	sgrNewline

	// sgrCarriage denotes a bare carriage return ('\r') indicating an in-place line overwrite.
	sgrCarriage
)

// feed consumes one chunk of input bytes and calls emit for every parsed text or control event.
//
// Chunks may split escape sequences and multi-byte UTF-8 runes at arbitrary byte offsets.
// Escape fragments are preserved internally within p.esc.
//
// Concurrency: loop-goroutine-owned (must only be called from the application loop).
func (p *sgrInterp) feed(b []byte, emit func(sgrEvent)) {
	var run []byte
	flush := func() {
		if len(run) > 0 {
			emit(sgrEvent{kind: sgrText, text: string(run), st: p.st})
			run = run[:0]
		}
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch p.mode {
		case escNone:
			// A pending bare \r commits as an overwrite unless the next
			// byte turns it into a \r\n newline. The flag survives chunk
			// boundaries.
			if p.pendingCR {
				p.pendingCR = false
				if c == '\n' {
					flush()
					emit(sgrEvent{kind: sgrNewline})
					continue
				}
				flush()
				emit(sgrEvent{kind: sgrCarriage})
			}
			switch {
			case c == 0x1b:
				flush()
				p.mode = escStarted
				p.esc = p.esc[:0]
			case c == '\n':
				flush()
				emit(sgrEvent{kind: sgrNewline})
			case c == '\r':
				p.pendingCR = true
			case c == '\t':
				run = append(run, ' ', ' ', ' ', ' ')
			case c < 0x20 || c == 0x7f:
				// Other C0 controls are dropped.
			default:
				run = append(run, c)
			}
		case escStarted:
			switch c {
			case '[':
				p.mode = escCSI
			case ']':
				p.mode = escOSC
			default:
				// Two-byte escape (ESC c, ESC =, charset selects, …): strip.
				p.mode = escNone
			}
		case escCSI:
			p.esc = append(p.esc, c)
			if c >= 0x40 && c <= 0x7e { // final byte
				if c == 'm' && p.passthrough {
					p.applySGR(string(p.esc[:len(p.esc)-1]))
				}
				p.mode = escNone
				p.esc = p.esc[:0]
			} else if len(p.esc) > maxEscape {
				p.mode = escNone
				p.esc = p.esc[:0]
			}
		case escOSC:
			// Terminated by BEL or ST (ESC \); bounded.
			p.esc = append(p.esc, c)
			if c == 0x07 || (len(p.esc) >= 2 && p.esc[len(p.esc)-2] == 0x1b && c == '\\') {
				p.mode = escNone
				p.esc = p.esc[:0]
			} else if len(p.esc) > maxEscape {
				p.mode = escNone
				p.esc = p.esc[:0]
			}
		}
	}
	flush()
}

// applySGR parses an SGR parameter string (e.g. "1;31;48;5;42") and applies the
// resulting color and text attribute mutations onto p.st.
func (p *sgrInterp) applySGR(params string) {
	if params == "" {
		p.st = style.Style{}
		return
	}
	parts := strings.Split(params, ";")
	nums := make([]int, 0, len(parts))
	for _, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			n = 0
		}
		nums = append(nums, n)
	}
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		switch {
		case n == 0:
			p.st = style.Style{}
		case n == 1:
			p.st = p.st.Bold(true)
		case n == 2:
			p.st = p.st.Faint(true)
		case n == 3:
			p.st = p.st.Italic(true)
		case n == 4:
			p.st = p.st.Underline(true)
		case n == 5:
			p.st = p.st.Blink(true)
		case n == 7:
			p.st = p.st.Reverse(true)
		case n == 9:
			p.st = p.st.Strikethrough(true)
		case n == 22:
			p.st = p.st.Bold(false).Faint(false)
		case n == 23:
			p.st = p.st.Italic(false)
		case n == 24:
			p.st = p.st.Underline(false)
		case n == 25:
			p.st = p.st.Blink(false)
		case n == 27:
			p.st = p.st.Reverse(false)
		case n == 29:
			p.st = p.st.Strikethrough(false)
		case n >= 30 && n <= 37:
			p.st = p.st.Foreground(style.ANSI(n - 30))
		case n >= 90 && n <= 97:
			p.st = p.st.Foreground(style.ANSI(n - 90 + 8))
		case n == 39:
			p.st = p.st.Foreground(style.Default())
		case n >= 40 && n <= 47:
			p.st = p.st.Background(style.ANSI(n - 40))
		case n >= 100 && n <= 107:
			p.st = p.st.Background(style.ANSI(n - 100 + 8))
		case n == 49:
			p.st = p.st.Background(style.Default())
		case n == 38 || n == 48:
			col, used, ok := extendedColor(nums[i+1:])
			i += used
			if !ok {
				continue
			}
			if n == 38 {
				p.st = p.st.Foreground(col)
			} else {
				p.st = p.st.Background(col)
			}
		}
	}
}

// extendedColor decodes the 38/48 extended color forms (5;n for ANSI-256 and 2;r;g;b for TrueColor RGB),
// returning the parsed style.Color, the count of parameters consumed, and whether the sequence was valid.
func extendedColor(rest []int) (style.Color, int, bool) {
	if len(rest) >= 2 && rest[0] == 5 {
		n := rest[1]
		if n < 0 || n > 255 {
			return style.Color{}, 2, false
		}
		return style.ANSI256(n), 2, true
	}
	if len(rest) >= 4 && rest[0] == 2 {
		r, g, b := rest[1], rest[2], rest[3]
		if r > 255 || g > 255 || b > 255 {
			return style.Color{}, 4, false
		}
		return style.RGB(uint8(r), uint8(g), uint8(b)), 4, true
	}
	return style.Color{}, len(rest), false
}
