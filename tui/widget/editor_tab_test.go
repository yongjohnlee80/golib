package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// A tab takes four cells in the terminal editor, as in the native one: text after it starts four
// columns in, the cursor on the text after it sits there, a click there lands on it, and a
// wrapped line counts it.
func TestATabTakesFourCells(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("\tx\n\t\tyy")))
	h := startApp(t, newShell(ed), 30, 4)
	defer h.stop()
	h.inject(click(0, 0))
	h.settle()
	rows := strings.Split(h.grid(), "\n")
	if rows[0][:5] != "    x" || rows[1][:10] != "        yy" {
		t.Fatalf("tabs drawn as:\n%s", h.grid())
	}
	// the cursor on the x (Normal mode, line 0 col 1) is at cell 4
	h.onLoop(func() { ed.Core().SetLine(0, 1) })
	var x, y int
	h.onLoop(func() { x, y, _ = ed.Cursor() })
	if x != 4 || y != 0 {
		t.Fatalf("the cursor after a tab is at cell %d,%d, want 4,0", x, y)
	}
	// a click on the second y of line 1 (cell 9) lands on its column, 3
	h.inject(click(9, 1))
	h.settle()
	var at widget.TextPos
	h.onLoop(func() { at = ed.Core().Cursor() })
	if at != (widget.TextPos{Line: 1, Col: 3}) {
		t.Fatalf("a click at cell 9 of line 1 went to %v, want 1:3", at)
	}
}

// Wrapped, a tab's four cells count toward the row's width.
func TestAWrappedLineCountsItsTabs(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("\tabcdefgh")), widget.WithEditorWrap(widget.WrapSoft))
	h := startApp(t, newShell(ed), 8, 4)
	defer h.stop()
	h.settle()
	rows := strings.Split(h.grid(), "\n")
	if rows[0] != "    abcd" || !strings.HasPrefix(rows[1], "efgh") {
		t.Fatalf("a wrapped tabbed line:\n%s", h.grid())
	}
}

// With indentation marks on, a leading tab shows as an arrow in its first cell.
func TestALeadingTabIsMarkedAsAnArrow(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("\tx")))
	h := startApp(t, newShell(ed), 20, 3)
	defer h.stop()
	h.onLoop(func() { ed.Core().SetIndentMarks(widget.IndentMarksAll) })
	h.settle()
	if row := strings.Split(h.grid(), "\n")[0]; !strings.HasPrefix(row, "→   x") {
		t.Fatalf("a marked tab: %q", row)
	}
}

// In a viewport narrower than a tab, a wrapped line's tab still takes its row alone, clipped to
// the row, as any cluster wider than a row does; the text after it starts the next row, and the
// cursor follows it there.
func TestATabWiderThanTheRowIsClipped(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("\tx")), widget.WithEditorWrap(widget.WrapSoft))
	h := startApp(t, newShell(ed), 3, 4)
	defer h.stop()
	h.settle()
	rows := strings.Split(h.grid(), "\n")
	if rows[0] != "   " || !strings.HasPrefix(rows[1], "x") {
		t.Fatalf("a tab wider than the row:\n%s", h.grid())
	}
	h.onLoop(func() { ed.Core().SetLine(0, 1) })
	var x, y int
	h.onLoop(func() { x, y, _ = ed.Cursor() })
	if x != 0 || y != 1 {
		t.Fatalf("the cursor on the x is at %d,%d, want 0,1", x, y)
	}
}

// A wrapped line's character that starts a row has the cursor on that row: the cursor is where its
// character is drawn, not past the end of the row before.
func TestTheCursorIsOnTheRowItsCharacterStarts(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("abcdefgh ij")), widget.WithEditorWrap(widget.WrapSoft))
	h := startApp(t, newShell(ed), 4, 5)
	defer h.stop()
	h.settle()
	for _, tc := range []struct{ col, x, y int }{{3, 3, 0}, {4, 0, 1}, {7, 3, 1}, {9, 1, 2}, {10, 2, 2}} {
		h.onLoop(func() { ed.Core().SetLine(0, tc.col) })
		var x, y int
		h.onLoop(func() { x, y, _ = ed.Cursor() })
		if x != tc.x || y != tc.y {
			t.Errorf("the cursor at column %d is at %d,%d, want %d,%d:\n%s", tc.col, x, y, tc.x, tc.y, h.grid())
		}
	}
}
