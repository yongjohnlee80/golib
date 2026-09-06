package widget

import (
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ErrClosed is returned by writes to a Writer whose BufferView has been
// unmounted (or never mounted).
var ErrClosed = errors.New("widget: buffer view closed")

// BufferView is the lazygit-class log/pager panel: an
// append-oriented, ring-bounded line buffer with scrollback, follow-tail,
// soft wrapping, and a bounded SGR-only ANSI interpreter feeding styled
// cells.
//
// The widget value is loop-owned like every other widget. The one
// sanctioned any-goroutine surface is the SEPARATE handle returned by
// Writer(): safe from any goroutine, bounded pending bytes (writes block
// when the loop lags — never unbounded buffering, mirroring
// ingestor/writer.go's semaphore model), ordered delivery, ErrClosed after
// unmount. *BufferView itself deliberately does NOT implement io.Writer
// (rev 1, Lector Q2).
//
// Keys (focused): Up/Down/PgUp/PgDn/Home/End scroll; End resumes
// follow-tail. The wheel scrolls. BufferView never consumes printable keys
// — it is a viewer.
type BufferView struct {
	Base
	maxLines    int
	follow      bool
	passthrough bool

	lines []bline
	head  int // ring head: live lines are lines[head:]

	interp   sgrInterp
	utf8Tail []byte // partial trailing rune between chunks

	scrollRow int // first visible wrapped row (ignored while following)
	totalRows int // wrapped rows at the last layout
	w, h      int

	alive bool
	wr    *bufWriter

	textSt style.Style
}

var _ tui.Focusable = (*BufferView)(nil)

// bline is one buffered line: styled spans.
type bline struct {
	spans []span
}

type span struct {
	text string
	st   style.Style
}

// BufferViewOption customizes a BufferView under construction.
type BufferViewOption func(*BufferView)

// WithMaxLines bounds the ring (default 10_000); beyond it the oldest
// lines drop.
func WithMaxLines(n int) BufferViewOption {
	if n < 1 {
		panic("widget: WithMaxLines: n must be >= 1")
	}
	return func(v *BufferView) { v.maxLines = n }
}

// WithFollowTail sets the initial follow-tail state (default true).
func WithFollowTail(fl bool) BufferViewOption {
	return func(v *BufferView) { v.follow = fl }
}

// WithANSIPassthrough toggles the SGR interpreter (default true). Off,
// escapes are stripped instead — for untrusted or plain streams.
func WithANSIPassthrough(fl bool) BufferViewOption {
	return func(v *BufferView) { v.passthrough = fl }
}

// NewBufferView builds an empty view.
func NewBufferView(opts ...BufferViewOption) *BufferView {
	v := &BufferView{
		maxLines:    10_000,
		follow:      true,
		passthrough: true,
		lines:       []bline{{}},
	}
	for _, o := range opts {
		if o != nil {
			o(v)
		}
	}
	v.interp.passthrough = v.passthrough
	v.wr = newBufWriter(v)
	return v
}

// Init binds the concurrent writer handle to the App and registers its
// close at unmount. Re-entrant across remounts.
func (v *BufferView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	v.alive = true
	v.wr.bind(ctx.App())
	ctx.OnUnmount(func() {
		v.alive = false
		v.wr.close()
	})
}

// Writer returns the concurrent io.Writer handle feeding this view. The
// HANDLE is the cross-goroutine surface; the BufferView value itself
// remains loop-owned. Contract: safe from any goroutine; bounded pending
// bytes (writes block when the loop lags); ordered delivery; ErrClosed
// after the view unmounts. Bytes are handed to the loop via the program
// lane; parsing and cell conversion happen on the loop
// goroutine.
//
// Do not write from the loop goroutine itself while the budget is
// exhausted: a blocked loop can never drain its own pending bytes. Loop
// code writing small amounts (well under the budget) is fine.
func (v *BufferView) Writer() io.Writer { return v.wr }

// AcceptsFocus implements tui.Focusable (scrollable viewer).
func (v *BufferView) AcceptsFocus() bool { return true }

// Following reports the follow-tail state.
func (v *BufferView) Following() bool { return v.follow }

// Clear drops the buffer (loop goroutine).
func (v *BufferView) Clear() {
	v.lines = []bline{{}}
	v.head = 0
	v.scrollRow = 0
	v.MarkDirty()
}

// LineCount returns the buffered line count (including a partial trailing
// line).
func (v *BufferView) LineCount() int { return len(v.lines) - v.head }

// PlainText returns the buffered content as unstyled text, one line per
// buffered line — the copy-to-clipboard payload. Loop goroutine only.
func (v *BufferView) PlainText() string {
	var sb strings.Builder
	for i := 0; i < v.LineCount(); i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		for _, sp := range v.line(i).spans {
			sb.WriteString(sp.text)
		}
	}
	return sb.String()
}

// line returns live line i.
func (v *BufferView) line(i int) *bline { return &v.lines[v.head+i] }

// ScrollTo scrolls logical line into view at the top and disengages
// follow-tail (loop goroutine).
func (v *BufferView) ScrollTo(line int) {
	line = max(0, min(line, v.LineCount()-1))
	rows := 0
	for i := 0; i < line; i++ {
		rows += v.rowsOf(i)
	}
	v.scrollRow = rows
	v.setFollow(false)
	v.clampScroll()
	v.MarkDirty()
}

// setFollow flips follow-tail, emitting FollowTailChangedEvent on change.
func (v *BufferView) setFollow(fl bool) {
	if v.follow == fl {
		return
	}
	v.follow = fl
	v.publish(FollowTailChangedEvent{Owner: v.NodeID(), Following: fl})
}

// contentWidth is the wrap width (minus the indicator column when
// scrollable).
func (v *BufferView) contentWidth() int {
	w := v.w
	if v.totalRows > v.h {
		w--
	}
	return max(w, 1)
}

// rowsOf is the wrapped height of live line i at the current width.
func (v *BufferView) rowsOf(i int) int {
	return len(wrapRanges(clusters(v.line(i).text()), v.contentWidth(), v.measure))
}

// text joins a line's spans.
func (l *bline) text() string {
	switch len(l.spans) {
	case 0:
		return ""
	case 1:
		return l.spans[0].text
	}
	n := 0
	for _, sp := range l.spans {
		n += len(sp.text)
	}
	out := make([]byte, 0, n)
	for _, sp := range l.spans {
		out = append(out, sp.text...)
	}
	return string(out)
}

// recount recomputes totalRows (called from Layout).
func (v *BufferView) recount() {
	// Two passes: the indicator column changes the wrap width when the
	// content overflows.
	count := func(w int) int {
		total := 0
		for i := 0; i < v.LineCount(); i++ {
			total += len(wrapRanges(clusters(v.line(i).text()), w, v.measure))
		}
		return total
	}
	total := count(max(v.w, 1))
	if total > v.h && v.w > 1 {
		total = count(v.w - 1)
	}
	v.totalRows = total
}

func (v *BufferView) maxScroll() int { return max(v.totalRows-v.h, 0) }

func (v *BufferView) clampScroll() {
	v.scrollRow = max(0, min(v.scrollRow, v.maxScroll()))
}

// scrollBy moves the viewport and manages follow-tail engagement.
func (v *BufferView) scrollBy(delta int) {
	if v.follow {
		v.scrollRow = v.maxScroll()
	}
	v.scrollRow += delta
	v.clampScroll()
	v.setFollow(v.scrollRow >= v.maxScroll())
	v.MarkDirty()
}

// HandleEvent implements the viewer key/mouse contract.
func (v *BufferView) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease {
			return false
		}
		switch e.Code {
		case tui.KeyUp:
			v.scrollBy(-1)
			return true
		case tui.KeyDown:
			v.scrollBy(1)
			return true
		case tui.KeyPageUp:
			v.scrollBy(-max(v.h, 1))
			return true
		case tui.KeyPageDown:
			v.scrollBy(max(v.h, 1))
			return true
		case tui.KeyHome:
			v.scrollRow = 0
			v.setFollow(v.maxScroll() == 0)
			v.MarkDirty()
			return true
		case tui.KeyEnd:
			v.scrollRow = v.maxScroll()
			v.setFollow(true) // End resumes follow (less +F)
			v.MarkDirty()
			return true
		case 'y':
			// Copy the whole buffer to the system clipboard (OSC 52); a
			// raw-mode TUI blocks terminal-native selection, so the viewer
			// owns copy. No-op when the backend lacks clipboard support.
			if e.Mods == 0 && v.Context() != nil {
				v.Context().CopyToClipboard(v.PlainText())
				return true
			}
		}
		return false
	case tui.MouseEvent:
		if e.Kind != tui.MouseWheel {
			return false
		}
		switch e.Button {
		case tui.WheelUp:
			v.scrollBy(-1)
			return true
		case tui.WheelDown:
			v.scrollBy(1)
			return true
		}
		return false
	}
	return false
}

// ingest runs on the loop goroutine: parse chunk (carrying partial UTF-8
// runes between chunks), append spans/lines, enforce the ring.
func (v *BufferView) ingest(chunk []byte) {
	if len(v.utf8Tail) > 0 {
		chunk = append(v.utf8Tail, chunk...)
		v.utf8Tail = nil
	}
	if tail := incompleteTail(chunk); tail > 0 {
		v.utf8Tail = append([]byte(nil), chunk[len(chunk)-tail:]...)
		chunk = chunk[:len(chunk)-tail]
	}
	v.interp.feed(chunk, func(ev sgrEvent) {
		last := &v.lines[len(v.lines)-1]
		switch ev.kind {
		case sgrNewline:
			v.lines = append(v.lines, bline{})
		case sgrCarriage:
			last.spans = nil // overwrite the current (partial) line
		case sgrText:
			if n := len(last.spans); n > 0 && last.spans[n-1].st == ev.st {
				last.spans[n-1].text += ev.text
			} else {
				last.spans = append(last.spans, span{text: ev.text, st: ev.st})
			}
		}
	})
	// Ring: drop the oldest lines beyond MaxLines; scrollback positions
	// adjust (approximately, at the last known width).
	over := v.LineCount() - v.maxLines
	if over > 0 {
		if !v.follow {
			dropped := 0
			for i := 0; i < over; i++ {
				dropped += v.rowsOf(i)
			}
			v.scrollRow = max(v.scrollRow-dropped, 0)
		}
		v.head += over
		if v.head > 4096 && v.head*2 > len(v.lines) {
			v.lines = append([]bline(nil), v.lines[v.head:]...)
			v.head = 0
		}
	}
	v.MarkDirty()
	if v.h > 0 {
		v.RequestLayout() // totalRows changed; re-derive scroll bounds
	}
}

// incompleteTail returns how many trailing bytes of b form an incomplete
// UTF-8 rune (0 when the chunk ends on a boundary).
func incompleteTail(b []byte) int {
	n := len(b)
	for back := 1; back <= 4 && back <= n; back++ {
		c := b[n-back]
		if c < 0x80 {
			return 0 // ASCII tail: complete
		}
		if c >= 0xc0 { // rune start
			runeLen := 0
			switch {
			case c >= 0xf0:
				runeLen = 4
			case c >= 0xe0:
				runeLen = 3
			default:
				runeLen = 2
			}
			if back < runeLen {
				return back // incomplete
			}
			return 0
		}
	}
	return 0
}

// Layout is greedy on both axes; it re-derives the wrapped row count and
// scroll bounds.
func (v *BufferView) Layout(c tui.Constraints) tui.Size {
	v.w = boundedMax(c.MaxW, max(c.MinW, 1))
	v.h = boundedMax(c.MaxH, max(c.MinH, 1))
	v.recount()
	v.clampScroll()
	return c.Constrain(tui.Size{W: v.w, H: v.h})
}

// Render paints the visible wrapped rows with their ANSI-derived styles.
func (v *BufferView) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	w := v.contentWidth()
	start := v.scrollRow
	if v.follow {
		start = v.maxScroll()
	}
	row := 0
	for i := 0; i < v.LineCount(); i++ {
		ln := v.line(i)
		rows := v.paintLine(s, ln, row-start, w)
		row += rows
		if row-start >= sz.H {
			break
		}
	}
	if v.totalRows > sz.H {
		paintScrollIndicator(s, sz.W-1, sz.H, start, max(v.totalRows-sz.H, 1)+1)
	}
}

// paintLine wraps one line (word-aware, matching recount) and paints its
// rows at viewport-relative yOff, preserving per-span styles. Returns the
// wrapped row count.
func (v *BufferView) paintLine(s tui.Surface, ln *bline, yOff, w int) int {
	// Flatten to per-cluster styles, then wrap by cluster ranges.
	var cls []string
	var sts []style.Style
	for _, sp := range ln.spans {
		st := sp.st.Inherit(v.textSt)
		for c := range tui.Graphemes(sp.text) {
			cls = append(cls, c)
			sts = append(sts, st)
		}
	}
	ranges := wrapRanges(cls, w, s.StringWidth)
	h := s.Size().H
	for r, rr := range ranges {
		y := yOff + r
		if y < 0 || y >= h {
			continue
		}
		x := 0
		for i := rr[0]; i < rr[1]; i++ {
			s.SetCell(x, y, cls[i], sts[i])
			x += s.StringWidth(cls[i])
		}
	}
	return len(ranges)
}

// --- the concurrent writer handle ---

// writerBudget bounds pending (loop-unprocessed) bytes per view;
// writerChunk is the enqueue granularity.
const (
	writerBudget = 256 << 10
	writerChunk  = 32 << 10
)

// bufWriter is the separate any-goroutine handle behind BufferView.Writer
// (rev 1). writeMu serializes whole Write calls (order = acquisition
// order); mu guards the byte budget and closed flag.
type bufWriter struct {
	view *BufferView // touched only inside app.Update closures (loop)

	writeMu sync.Mutex

	mu      sync.Mutex
	cond    *sync.Cond
	app     *tui.App
	pending int
	closed  bool
}

func newBufWriter(v *BufferView) *bufWriter {
	w := &bufWriter{view: v, closed: true}
	w.cond = sync.NewCond(&w.mu)
	return w
}

// bind opens the handle against the owning App (called from Init, loop
// goroutine).
func (w *bufWriter) bind(app *tui.App) {
	w.mu.Lock()
	w.app = app
	w.closed = false
	w.mu.Unlock()
}

// close marks the handle closed and wakes blocked writers (unmount hook).
func (w *bufWriter) close() {
	w.mu.Lock()
	w.closed = true
	w.cond.Broadcast()
	w.mu.Unlock()
}

// release returns quota after the loop ingests a chunk.
func (w *bufWriter) release(n int) {
	w.mu.Lock()
	w.pending -= n
	w.cond.Broadcast()
	w.mu.Unlock()
}

// Write implements io.Writer per the §2.4 handle contract: any-goroutine,
// bounded pending bytes (blocks when the loop lags), ordered, ErrClosed
// after unmount.
func (w *bufWriter) Write(p []byte) (int, error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	total := 0
	for len(p) > 0 {
		n := min(len(p), writerChunk)
		chunk := append([]byte(nil), p[:n]...)

		w.mu.Lock()
		for !w.closed && w.pending > 0 && w.pending+n > writerBudget {
			w.cond.Wait()
		}
		if w.closed {
			w.mu.Unlock()
			return total, ErrClosed
		}
		w.pending += n
		app := w.app
		w.mu.Unlock()

		view := w.view
		app.Update(func() {
			w.release(len(chunk))
			if view.alive {
				view.ingest(chunk)
			}
		})
		total += n
		p = p[n:]
	}
	return total, nil
}
