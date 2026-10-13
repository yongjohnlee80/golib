package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// viewportEditor is a soft-wrapped Editor laid out 10 cells wide and h rows tall, over lines of 3,
// 1, 2, 1 and 1 rows.
func viewportEditor(t *testing.T, h int) *Editor {
	t.Helper()
	text := strings.Repeat("a", 25) + "\nb\n" + strings.Repeat("c", 15) + "\nd\ne"
	e := NewEditor(WithInitialText(text), WithEditorWrap(WrapSoft))
	e.Layout(tui.Constraints{MaxW: 10, MaxH: h})
	v := e.view().settled(e.core.buf.lines)
	for i, want := range []int{3, 1, 2, 1, 1} {
		if got := wrapRowsOfLine(e.core.buf.lines, i, v); got != want {
			t.Fatalf("the fixture's line %d takes %d rows, want %d", i, got, want)
		}
	}
	return e
}

func shownLines(e *Editor) [2]int {
	f, l := e.cells.VisibleLines()
	return [2]int{f, l}
}

// The visible lines are the top one to the last whose first row is in the view, counting wrapped
// rows: a line cut off at the bottom is in, and a line taller than the view is alone.
func TestCellLayoutVisibleLinesCountWrappedRows(t *testing.T) {
	e := viewportEditor(t, 6)
	if got := shownLines(e); got != [2]int{0, 2} {
		t.Fatalf("top 0, 6 rows: %v, want [0 2] (line 2 starts on row 4 and is cut off)", got)
	}
	e.cells.top = 1
	if got := shownLines(e); got != [2]int{1, 4} {
		t.Fatalf("top 1: %v, want [1 4]", got)
	}
	e = viewportEditor(t, 2)
	if got := shownLines(e); got != [2]int{0, 0} {
		t.Fatalf("a line taller than the view: %v, want [0 0]", got)
	}
	e = viewportEditor(t, 4)
	if got := shownLines(e); got != [2]int{0, 1} {
		t.Fatalf("4 rows: %v, want [0 1]", got)
	}
	e.cells.h = 0
	e.core.buf.ln = 3
	if got := shownLines(e); got != [2]int{3, 3} {
		t.Fatalf("before a layout: %v, want the cursor's line", got)
	}
}

// ScrollLines moves the top by lines, down no further than shows the last line at the bottom.
func TestCellLayoutScrollLinesStopsAtTheEnd(t *testing.T) {
	e := viewportEditor(t, 6)
	e.cells.ScrollLines(1)
	if e.cells.top != 1 {
		t.Fatalf("down 1: top %d", e.cells.top)
	}
	e.cells.ScrollLines(10)
	if e.cells.top != 1 {
		t.Fatalf("down 10: top %d, want 1, where line 4 is on the bottom row", e.cells.top)
	}
	e.cells.ScrollLines(-10)
	if e.cells.top != 0 {
		t.Fatalf("up 10: top %d", e.cells.top)
	}
	e.cells.top = 3 // past the lowest top (a shrunk text): down does not pull it back up
	e.cells.ScrollLines(1)
	if e.cells.top != 3 {
		t.Fatalf("down from past the end: top %d, want 3", e.cells.top)
	}
	e.cells.h = 0
	e.cells.ScrollLines(-2)
	if e.cells.top != 3 {
		t.Fatalf("with no size: top %d, want it left", e.cells.top)
	}
}

// Through a real Editor: H M L read its wrapped view, and Ctrl-d scrolls it with the cursor.
func TestEditorViewKeysUseItsLayout(t *testing.T) {
	e := viewportEditor(t, 6)
	press := func(k tui.KeyEvent) {
		e.HandleEvent(k)
		e.Layout(tui.Constraints{MaxW: 10, MaxH: 6})
	}
	press(tui.KeyEvent{Kind: tui.KeyPress, Code: 'L', Text: "L"})
	if ln, _ := e.Line(); ln != 2 {
		t.Fatalf("L: line %d, want 2", ln)
	}
	press(tui.KeyEvent{Kind: tui.KeyPress, Code: 'M', Text: "M"})
	if ln, _ := e.Line(); ln != 1 {
		t.Fatalf("M: line %d, want 1", ln)
	}
	press(tui.KeyEvent{Kind: tui.KeyPress, Code: 'H', Text: "H"})
	if ln, _ := e.Line(); ln != 0 {
		t.Fatalf("H: line %d, want 0", ln)
	}
	press(tui.KeyEvent{Kind: tui.KeyPress, Code: 'd', Mods: tui.ModCtrl}) // half of 6 rows: 3
	if ln, _ := e.Line(); ln != 3 || e.cells.top != 1 {
		t.Fatalf("Ctrl-d: line %d top %d, want line 3 top 1", ln, e.cells.top)
	}
	press(tui.KeyEvent{Kind: tui.KeyPress, Code: 'u', Mods: tui.ModCtrl})
	if ln, _ := e.Line(); ln != 0 || e.cells.top != 0 {
		t.Fatalf("Ctrl-u: line %d top %d, want line 0 top 0", ln, e.cells.top)
	}
}
