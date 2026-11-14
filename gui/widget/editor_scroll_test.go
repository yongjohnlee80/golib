package widget

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// sixtyLines is a text of 60 numbered lines.
func sixtyLines() string {
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// told is a log of the tops an editor tells, safe to read from the test.
type told struct {
	mu   sync.Mutex
	tops []int
}

func (l *told) add(top int) { l.mu.Lock(); l.tops = append(l.tops, top); l.mu.Unlock() }
func (l *told) all() []int  { l.mu.Lock(); defer l.mu.Unlock(); return append([]int(nil), l.tops...) }

// The first line shown is read and set, clamped to the text, and told to the host once at each
// change, after the view is drawn: SetTopLine, a key's reveal; a draw with the top where it was
// tells nothing. A SetTopLine before the first draw is held and applied then.
func TestTheEditorsTopLineIsSetReadAndTold(t *testing.T) {
	var log told
	h := startEditor(t, 40, 12, WithCore(editor.CoreInitialText(sixtyLines())), WithEditorOnScroll(log.add))
	top := func() (n int) {
		h.onLoop(func() { n = h.e.TopLine() })
		return n
	}
	h.onLoop(func() { h.e.SetTopLine(20) })
	h.paint()
	if got := top(); got != 20 {
		t.Fatalf("SetTopLine(20): top %d", got)
	}
	h.until("20 told", func() bool { a := log.all(); return len(a) > 0 && a[len(a)-1] == 20 })
	n := len(log.all())
	h.paint()
	h.paint()
	h.onLoop(func() {})
	if got := len(log.all()); got != n {
		t.Fatalf("draws with the top unmoved told %v", log.all()[n:])
	}
	h.onLoop(func() { h.e.SetTopLine(1000) })
	h.paint()
	if got := top(); got < 40 || got > 59 {
		t.Fatalf("SetTopLine(1000): top %d, want the lowest top that shows the last line", got)
	}
	h.onLoop(func() { h.e.SetTopLine(-5) })
	h.paint()
	if got := top(); got != 0 {
		t.Fatalf("SetTopLine(-5): top %d", got)
	}
	h.until("0 told", func() bool { a := log.all(); return len(a) > 0 && a[len(a)-1] == 0 })
	h.keys(tui.KeyEvent{Kind: tui.KeyPress, Code: 'G', Text: "G"}) // the cursor to the last line: revealed
	h.barrier()
	h.paint()
	h.until("the reveal told", func() bool { a := log.all(); return len(a) > 0 && a[len(a)-1] > 30 })

}

// A SetTopLine made before the editor is first drawn is held, and the first draw puts that line at
// the view's top: the view's own first line, as painted, is it.
func TestASetTopLineBeforeTheFirstDrawIsApplied(t *testing.T) {
	early := NewEditor(WithCore(editor.CoreInitialText(sixtyLines())))
	early.SetTopLine(15)
	if early.TopLine() != 15 {
		t.Fatalf("a SetTopLine before any draw: top %d", early.TopLine())
	}
	h := startWith(t, early, 40, 12)
	h.paint()
	var first int
	h.onLoop(func() { first, _ = h.e.layout.VisibleLines() })
	if first != 15 {
		t.Fatalf("the first draw shows line %d at the top, want the 15 set before it", first)
	}
}

// A line's own look fills its whole row under its text, in the colour its style names, resolved
// for the page; the other lines keep the page. The caret on such a line is drawn on its look.
func TestALineStyleFillsTheLinesRow(t *testing.T) {
	tint := style.RGB(0x30, 0x60, 0x30)
	want := color.NRGBA{R: 0x30, G: 0x60, B: 0x30, A: 0xff}
	h := startEditor(t, 40, 12, WithCore(editor.CoreInitialText("plain\nmarked\nplain")),
		WithEditorLineStyle(func(line int) (style.Style, bool) { return style.New().Background(tint), line == 1 }))
	h.onLoop(func() { h.e.core.SetLine(1, 0) })
	rc := h.paint()
	var rows []gui.Rect
	for _, c := range rc.Calls {
		if c.Op == "FillRect" && c.Brush.Color == want {
			rows = append(rows, c.Rect)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("%d fills in the line's look, want one row: %v", len(rows), rows)
	}
	if rows[0].W < float32(h.e.body.w*cellW)/2 {
		t.Fatalf("the look's fill %v is not the row's width", rows[0])
	}
	var bg color.NRGBA
	h.onLoop(func() { bg = h.e.body.caretBG })
	if bg != want {
		t.Fatalf("the caret on the marked line drawn on %v, want its look", bg)
	}
	h.onLoop(func() { h.e.SetLineStyle(nil) })
	rc = h.paint()
	for _, c := range rc.Calls {
		if c.Op == "FillRect" && c.Brush.Color == want {
			t.Fatal("the look stayed after SetLineStyle(nil)")
		}
	}
}

// In the cell fallback too, SetTopLine never sets the top so low that the last line is above the
// bottom row: 60 lines in 12 rows top out at 48.
func TestSetTopLineClampsInTheCellFallback(t *testing.T) {
	e := NewEditor(WithCore(editor.CoreInitialText(sixtyLines())))
	e.body.Layout(tui.Constraints{MaxW: 40, MaxH: 12})
	e.SetTopLine(1000)
	if got := e.TopLine(); got != 48 {
		t.Fatalf("SetTopLine(1000) in 12 cell rows: top %d, want 48", got)
	}
	e.SetTopLine(-3)
	if got := e.TopLine(); got != 0 {
		t.Fatalf("SetTopLine(-3): top %d", got)
	}
}
