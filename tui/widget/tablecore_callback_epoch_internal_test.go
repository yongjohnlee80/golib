package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// replacingCore is a core over people whose cursor callback, once armed, replaces the source with
// a fresh one, the first time only. armed starts true.
func replacingCore(opts ...TableOption) (c *TableCore, replaced *bool) {
	replaced = new(bool)
	c = NewTableCore(people(), append([]TableOption{TableOnCursor(func(int) {
		if c != nil && !*replaced {
			*replaced = true
			c.SetSource(people())
		}
	})}, opts...)...)
	return c, replaced
}

// A press whose cursor callback replaced the source starts no drag, so the motion after it selects
// nothing of the new rows. With the source unchanged, the same press and motion select the cells.
func TestATablePressStartsNoDragOverRowsItsCallbackReplaced(t *testing.T) {
	press := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 3, Count: 1}
	motion := tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 1, Y: 4}
	t.Run("unchanged source, the control", func(t *testing.T) {
		c := NewTableCore(people())
		view := NewDataTable(c)
		view.Layout(tui.Tight(tui.Size{W: 40, H: 10}))
		view.HandleEvent(press)
		if !c.Dragging() {
			t.Fatal("a press on a cell started no drag")
		}
		view.HandleEvent(motion)
		if _, _, ok := c.Selection(); !ok {
			t.Error("a drag off the pressed cell selected nothing")
		}
	})
	t.Run("a source the callback replaced", func(t *testing.T) {
		c, replaced := replacingCore()
		view := NewDataTable(c)
		view.Layout(tui.Tight(tui.Size{W: 40, H: 10}))
		view.HandleEvent(press)
		if !*replaced || c.Dragging() {
			t.Fatalf("replaced %v; dragging %v after the source was replaced", *replaced, c.Dragging())
		}
		view.HandleEvent(motion)
		if _, _, ok := c.Selection(); ok {
			t.Error("the motion after the abandoned press selected rows of the new source")
		}
	})
}

// A right press on a row's number whose cursor callback replaced the source selects no row of it.
func TestATableMenuPressSelectsNothingOfRowsItsCallbackReplaced(t *testing.T) {
	c, replaced := replacingCore(TableMenuSelects(true))
	c.selectForMenu(TablePos{Row: 2}, TableHitGutter)
	if _, _, ok := c.Selection(); !*replaced || ok {
		t.Errorf("replaced %v; a row of the new source selected %v", *replaced, ok)
	}
}
