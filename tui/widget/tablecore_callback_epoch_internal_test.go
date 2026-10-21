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

// A filter whose open edit the owner answered, when it was dropped, with new rows applies nothing to
// them: the new source shows every row.
func TestATableFilterAppliesNothingToRowsItsDroppedEditReplaced(t *testing.T) {
	var c *TableCore
	replaced := false
	c, _, _, _ = csvTable("name,age\nann,1\nbo,20\n", TableOnEditDropped(func() {
		if !replaced {
			replaced = true
			c.SetSource(people())
		}
	}))
	c.SetCursor(TablePos{0, 1})
	if !c.EditCell("") {
		t.Fatal("no field opened")
	}
	c.SetFilter(where(t, c, "age > 10"))
	if shown, loaded := c.Filtered(); !replaced || shown != loaded || loaded != 3 {
		t.Errorf("replaced %v; %d of %d rows shown, want all 3 of the new source", replaced, shown, loaded)
	}
}

// A press on a row's number or a field's title whose cursor callback replaced the source starts no
// drag and selects nothing of the new rows.
func TestATableGutterOrTitlePressStartsNoDragOverRowsItsCallbackReplaced(t *testing.T) {
	for _, hit := range []TableHit{TableHitGutter, TableHitHeader} {
		var c *TableCore
		armed, replaced := false, false
		c = NewTableCore(people(), TableOnCursor(func(int) {
			if armed && !replaced {
				replaced = true
				c.SetSource(people())
			}
		}))
		c.SetCursor(TablePos{2, 0}) // so the press moves the cursor, and the owner hears it
		armed = true
		c.PressAt(TablePos{Row: 0, Col: 1}, hit)
		if _, _, sel := c.Selection(); !replaced || c.Dragging() || sel {
			t.Errorf("hit %v: replaced %v; dragging %v, selection %v over the new rows", hit, replaced, c.Dragging(), sel)
		}
	}
}

// A press whose cursor callback reorders the rows in place and says so (Changed) starts no drag over
// them: Changed ends the rows' epoch as a new source does. Rows a pager delivers at rest leave the
// next press and drag as they were.
func TestATableChangedInPlaceEndsThePressOverTheOldRows(t *testing.T) {
	press := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 3, Count: 1}
	motion := tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 1, Y: 4}
	t.Run("reordered in place from the callback", func(t *testing.T) {
		src := people()
		var c *TableCore
		reordered := false
		c = NewTableCore(src, TableOnCursor(func(int) {
			if c != nil && !reordered {
				reordered = true
				src.rows[0], src.rows[1] = src.rows[1], src.rows[0]
				c.Changed()
			}
		}))
		view := NewDataTable(c)
		view.Layout(tui.Tight(tui.Size{W: 40, H: 10}))
		view.HandleEvent(press)
		if !reordered || c.Dragging() {
			t.Fatalf("reordered %v; dragging %v over the reordered rows", reordered, c.Dragging())
		}
		view.HandleEvent(motion)
		if _, _, ok := c.Selection(); ok {
			t.Error("the motion selected the reordered rows")
		}
	})
	t.Run("rows a pager delivered at rest, the control", func(t *testing.T) {
		p := &pagedSource{rowsSource: rowsSource{
			fields: []TableField{{Title: "n"}, {Title: "r"}},
			rows:   [][]string{{"0", "r0"}, {"1", "r1"}, {"2", "r2"}},
		}, total: 6}
		c := NewTableCore(p)
		view := NewDataTable(c)
		view.Layout(tui.Tight(tui.Size{W: 40, H: 10}))
		p.FetchMore(3)
		p.arrive(c) // Changed, at rest
		view.HandleEvent(press)
		view.HandleEvent(motion)
		if _, _, ok := c.Selection(); !c.Dragging() || !ok {
			t.Errorf("after rows arrived: dragging %v, selection %v; want a drag selecting", c.Dragging(), ok)
		}
	})
}

// A cut whose copy callback (TableOnCopy) replaced the source keeps the yank in the register and
// cuts nothing: not through the old source's committer at the new rows' cursor. The unchanged-source
// and refused-yank controls are TestCutYanksThenDeletesOrEmpties and
// TestACutPastTheCopyLimitDeletesNothing.
func TestACutStopsWhenItsCopyCallbackReplacedTheSource(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n"
	var c *TableCore
	copied := ""
	c, _, core, _ := csvTable(text, TableOnCopy(func(s string) {
		if copied == "" {
			copied = s
			other, _ := csvOn("x,y\n7,8\n9,0\n", ',')
			c.SetSource(other)
		}
	}))
	c.SetCursor(TablePos{1, 0})
	c.Cut(false)
	if copied != "3" || core.Value() != text {
		t.Errorf("copied %q; the old document is %q, want it untouched", copied, core.Value())
	}
	if reg, _ := c.register().Register(); reg != "3" {
		t.Errorf("register %q, want the yank 3 kept", reg)
	}
}
