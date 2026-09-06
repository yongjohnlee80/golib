package widget

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui/style"
)

// sgrInterp is BufferView's bounded, SGR-only ANSI interpreter: colors
// (ANSI-16 / ANSI-256 / truecolor) and
// bold/faint/italic/underline/blink/reverse/strikethrough map directly to
// style properties; every other escape (cursor movement, modes, OSC) is
// recognized and stripped. State machines and pending buffers are bounded:
// an escape sequence longer than maxEscape is abandoned and dropped.
type sgrInterp struct {
	passthrough bool        // false = strip escapes AND ignore SGR
	st          style.Style // current SGR state
	esc         []byte      // pending escape bytes (bounded)
	mode        escMode
	pendingCR   bool // a bare \r was seen; \n turns it into a newline
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

// sgrEvent is one interpreter output.
type sgrEvent struct {
	kind sgrKind
	text string      // kind == sgrText
	st   style.Style // kind == sgrText
}

type sgrKind uint8

const (
	sgrText sgrKind = iota
	sgrNewline
	sgrCarriage // bare \r: overwrite the current line
)

// feed consumes one chunk, invoking emit per event. Chunks may split
// escape sequences and UTF-8 runes at any byte; the caller carries rune
// fragments (the interpreter carries escape fragments).
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

// applySGR folds one SGR parameter string into the current style.
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

// extendedColor decodes the 38/48 extended color forms (5;n and 2;r;g;b),
// returning the color, the parameters consumed, and validity.
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
