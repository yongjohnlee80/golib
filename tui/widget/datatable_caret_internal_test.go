package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// The open field's caret is the table's state, not its last paint's: Cursor reports it before any
// paint, and follows what is typed without one (Render is pure, tui-widget-data-S5).
func TestADataTableCaretIsItsStateNotItsLastPaint(t *testing.T) {
	src, _ := csvOn("name,note\nann,one\n", ',')
	c := NewTableCore(src)
	dt := NewDataTable(c)
	dt.Layout(tui.Tight(tui.Size{W: 40, H: 8}))
	c.SetCursor(TablePos{0, 1})
	if !c.EditCell("") {
		t.Fatal("no field opened")
	}
	x, y, ok := dt.Cursor()
	if !ok {
		t.Fatal("no caret reported for an open field before a paint")
	}
	c.Editing().Field.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: 'z', Text: "z"})
	x2, y2, ok2 := dt.Cursor()
	if !ok2 || y2 != y || x2 != x+1 {
		t.Errorf("after typing z the caret is %d,%d (%v), want %d,%d", x2, y2, ok2, x+1, y)
	}
}
