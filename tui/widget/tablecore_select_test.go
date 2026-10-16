package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// grid4 is four rows of three fields: r0c0 … r3c2.
func grid4() *rowsSource {
	s := &rowsSource{fields: []TableField{{Title: "a"}, {Title: "b"}, {Title: "c"}}}
	for r := range 4 {
		row := make([]string, 3)
		for c := range row {
			row[c] = "r" + string(rune('0'+r)) + "c" + string(rune('0'+c))
		}
		s.rows = append(s.rows, row)
	}
	return s
}

func keys(c *TableCore, ks ...tui.KeyEvent) {
	for _, k := range ks {
		c.HandleKey(k)
	}
}

// v selects cells in reading order, V whole rows, Ctrl+V the rectangle, each from the anchor at
// row 0 field 1 to the cursor at row 2 field 0; the copy follows the kind.
func TestTableSelect_KindsCopyTheirCells(t *testing.T) {
	for _, c := range []struct {
		name  string
		start tui.KeyEvent
		kind  TableSelectKind
		copy  string
		in    []TablePos
		out   []TablePos
	}{
		{"v", text("v"), TableSelectCells, "r0c1\tr0c2\nr1c0\tr1c1\tr1c2\nr2c0",
			[]TablePos{{0, 1}, {0, 2}, {1, 0}, {1, 2}, {2, 0}}, []TablePos{{0, 0}, {2, 1}, {3, 0}}},
		{"V", text("V"), TableSelectRows, "r0c0\tr0c1\tr0c2\nr1c0\tr1c1\tr1c2\nr2c0\tr2c1\tr2c2",
			[]TablePos{{0, 0}, {0, 2}, {2, 2}}, []TablePos{{3, 0}, {0, 3}}},
		{"Ctrl+V", ctrlV, TableSelectBlock, "r0c0\tr0c1\nr1c0\tr1c1\nr2c0\tr2c1",
			[]TablePos{{0, 0}, {2, 1}}, []TablePos{{0, 2}, {1, 2}, {3, 0}}},
	} {
		core := NewTableCore(grid4())
		core.SetCursor(TablePos{0, 1})
		keys(core, c.start, text("j"), text("j"), text("h"))
		if core.SelectionKind() != c.kind {
			t.Errorf("%s: kind %v, want %v", c.name, core.SelectionKind(), c.kind)
		}
		if got := core.CopyText(); got != c.copy {
			t.Errorf("%s: copied %q, want %q", c.name, got, c.copy)
		}
		for _, p := range c.in {
			if !core.InSelection(p.Row, p.Col) {
				t.Errorf("%s: %+v not selected", c.name, p)
			}
		}
		for _, p := range c.out {
			if core.InSelection(p.Row, p.Col) {
				t.Errorf("%s: %+v selected", c.name, p)
			}
		}
		if from, to, ok := core.Selection(); !ok || from != (TablePos{0, 0}) || to != (TablePos{2, 1}) {
			t.Errorf("%s: Selection %+v %+v %v, want the anchor's and cursor's corners", c.name, from, to, ok)
		}
	}
	// within one row, v is the fields between
	core := NewTableCore(grid4())
	core.SetCursor(TablePos{1, 2})
	keys(core, text("v"), text("h"))
	if got := core.CopyText(); got != "r1c1\tr1c2" {
		t.Errorf("v on one row: %q", got)
	}
}

// v, V and Ctrl+V switch a selection's kind keeping its anchor, the same key again ends it, and
// SetAnchor means cells.
func TestTableSelect_KeysSwitchAndEnd(t *testing.T) {
	c := NewTableCore(grid4())
	keys(c, text("V"), text("j"), ctrlV)
	if c.SelectionKind() != TableSelectBlock || c.CopyText() != "r0c0\nr1c0" {
		t.Errorf("V then Ctrl+V: kind %v, %q", c.SelectionKind(), c.CopyText())
	}
	keys(c, text("v"))
	if c.SelectionKind() != TableSelectCells {
		t.Errorf("then v: kind %v", c.SelectionKind())
	}
	keys(c, text("v"))
	if _, _, ok := c.Selection(); ok {
		t.Error("v on a v selection kept it")
	}
	c.Select(TableSelectRows, TablePos{0, 0})
	c.SetAnchor(&TablePos{0, 0})
	if c.SelectionKind() != TableSelectCells {
		t.Errorf("SetAnchor made a %v selection", c.SelectionKind())
	}
	c.SelectAll()
	if c.SelectionKind() != TableSelectRows || c.Cursor().Row != 0 || !c.InSelection(3, 2) {
		t.Errorf("SelectAll: kind %v, cursor %+v", c.SelectionKind(), c.Cursor())
	}
}

// Counts multiply moves; gg and G go to the first and last row, [n]G and [n]gg to row n; w and b
// move by fields across rows; 0 after a count is part of it.
func TestTableSelect_CountsGGAndFields(t *testing.T) {
	big := &rowsSource{fields: []TableField{{Title: "a"}, {Title: "b"}, {Title: "c"}}}
	for range 30 {
		big.rows = append(big.rows, []string{"x", "y", "z"})
	}
	c := NewTableCore(big)
	keys(c, text("5"), text("j"))
	if c.Cursor().Row != 5 {
		t.Errorf("5j: row %d", c.Cursor().Row)
	}
	keys(c, text("1"), text("0"), key(tui.KeyDown))
	if c.Cursor().Row != 15 {
		t.Errorf("10↓: row %d", c.Cursor().Row)
	}
	keys(c, text("3"), text("k"), text("2"), text("l"))
	if c.Cursor() != (TablePos{12, 2}) {
		t.Errorf("3k 2l: %+v", c.Cursor())
	}
	keys(c, text("G"))
	if c.Cursor().Row != 29 {
		t.Errorf("G: row %d", c.Cursor().Row)
	}
	keys(c, text("g"), text("g"))
	if c.Cursor().Row != 0 {
		t.Errorf("gg: row %d", c.Cursor().Row)
	}
	keys(c, text("7"), text("G"))
	if c.Cursor().Row != 6 {
		t.Errorf("7G: row %d, want 6 (row 7 counted from 1)", c.Cursor().Row)
	}
	keys(c, text("2"), text("0"), text("g"), text("g"))
	if c.Cursor().Row != 19 {
		t.Errorf("20gg: row %d", c.Cursor().Row)
	}
	c.SetCursor(TablePos{4, 1})
	keys(c, text("3"), text("w"))
	if c.Cursor() != (TablePos{5, 1}) {
		t.Errorf("3w from row 4 field 1: %+v, want row 5 field 1", c.Cursor())
	}
	keys(c, text("b"), text("b"))
	if c.Cursor() != (TablePos{4, 2}) {
		t.Errorf("bb: %+v, want row 4 field 2", c.Cursor())
	}
	keys(c, text("9"), text("9"), text("9"), text("w"))
	if c.Cursor() != (TablePos{29, 2}) {
		t.Errorf("999w: %+v, want the last cell", c.Cursor())
	}
	keys(c, text("0"))
	if c.Cursor() != (TablePos{29, 0}) {
		t.Errorf("0 with no count: %+v, want the first field", c.Cursor())
	}
}

// g alone waits: the next key, not a g, drops it and runs as itself, and moves nothing to row 0.
func TestTableSelect_GAloneWaits(t *testing.T) {
	c := NewTableCore(grid4())
	c.SetCursor(TablePos{3, 0})
	if !c.HandleKey(text("g")) {
		t.Fatal("g was not taken")
	}
	if c.Cursor().Row != 3 {
		t.Fatalf("g alone jumped to row %d", c.Cursor().Row)
	}
	keys(c, text("k"))
	if c.Cursor().Row != 2 {
		t.Errorf("g then k: row %d, want 2: k ran, the g dropped", c.Cursor().Row)
	}
	keys(c, text("3"), text("g"), text("j"))
	if c.Cursor().Row != 3 {
		t.Errorf("3g then j: row %d, want 3: the count went with the g", c.Cursor().Row)
	}
	keys(c, text("g"))
	if !c.HandleKey(key(tui.KeyEscape)) {
		t.Error("Esc dropping a pending g was not taken")
	}
	if c.HandleKey(key(tui.KeyEscape)) {
		t.Error("Esc with nothing pending and no selection was taken")
	}
}

// A press is a command boundary: a count and a pending g are dropped, and a selection ends.
func TestTableSelect_APressDropsACountAndAPendingG(t *testing.T) {
	big := &rowsSource{fields: []TableField{{Title: "a"}, {Title: "b"}}}
	for range 20 {
		big.rows = append(big.rows, []string{"x", "y"})
	}
	c := NewTableCore(big)
	keys(c, text("5"))
	c.PressAt(TablePos{2, 0}, TableHitCell)
	c.EndDrag()
	keys(c, text("j"))
	if c.Cursor().Row != 3 {
		t.Errorf("5, press on row 2, j: row %d, want 3: the count dropped", c.Cursor().Row)
	}
	keys(c, text("g"))
	c.PressAt(TablePos{10, 0}, TableHitCell)
	c.EndDrag()
	keys(c, text("g"))
	if c.Cursor().Row != 10 {
		t.Errorf("g, press on row 10, g: row %d, want 10: the first g dropped", c.Cursor().Row)
	}
	keys(c, text("V"))
	c.PressAt(TablePos{12, 1}, TableHitCell)
	if _, _, ok := c.Selection(); ok {
		t.Error("a press kept the selection")
	}
	c.PressAt(TablePos{}, TableHitNone)
	if c.Dragging() || c.Cursor().Row != 12 {
		t.Errorf("a press on nothing: dragging %v, cursor %+v", c.Dragging(), c.Cursor())
	}
}

// Ctrl+V is the one Ctrl chord the table takes.
func TestTableSelect_CtrlVIsTheOneCtrlChord(t *testing.T) {
	c := NewTableCore(grid4())
	if !c.HandleKey(ctrlV) || c.SelectionKind() != TableSelectBlock {
		t.Error("Ctrl+V was not taken as a block")
	}
	for _, k := range []tui.KeyEvent{
		{Kind: tui.KeyPress, Code: 'c', Mods: tui.ModCtrl},
		{Kind: tui.KeyPress, Code: 'y', Mods: tui.ModCtrl},
		{Kind: tui.KeyPress, Code: 'v', Mods: tui.ModAlt},
		{Kind: tui.KeyPress, Code: 'v', Mods: tui.ModCtrl | tui.ModAlt},
		{Kind: tui.KeyRelease, Code: 'v', Mods: tui.ModCtrl},
	} {
		if c.HandleKey(k) {
			t.Errorf("%+v was taken; it is the application's", k)
		}
	}
}

// The pointer's drag: a cell press selects nothing until it leaves the pressed cell, then cells;
// a gutter press selects its row and a drag widens it by rows; a header press selects its field
// over every row and a drag across the titles widens it.
func TestTableSelect_DragsSelectByWhatWasPressed(t *testing.T) {
	c := NewTableCore(grid4())
	c.PressAt(TablePos{1, 1}, TableHitCell)
	c.DragTo(TablePos{1, 1})
	if _, _, ok := c.Selection(); ok || !c.Dragging() {
		t.Fatalf("a drag still on the pressed cell selected (or no drag): dragging %v", c.Dragging())
	}
	c.DragTo(TablePos{2, 0})
	if c.SelectionKind() != TableSelectCells || c.CopyText() != "r1c1\tr1c2\nr2c0" {
		t.Errorf("a drag off the cell: kind %v, %q", c.SelectionKind(), c.CopyText())
	}
	c.EndDrag()
	if c.Dragging() {
		t.Error("EndDrag left the drag")
	}
	c.DragTo(TablePos{3, 2})
	if c.Cursor() != (TablePos{2, 0}) {
		t.Error("a drag after EndDrag moved the cursor")
	}

	c.PressAt(TablePos{2, 2}, TableHitGutter)
	if c.SelectionKind() != TableSelectRows || c.CopyText() != "r2c0\tr2c1\tr2c2" {
		t.Errorf("a gutter press: kind %v, %q", c.SelectionKind(), c.CopyText())
	}
	c.DragTo(TablePos{3, 0})
	if c.SelectionKind() != TableSelectRows || c.CopyText() != "r2c0\tr2c1\tr2c2\nr3c0\tr3c1\tr3c2" {
		t.Errorf("a gutter drag: kind %v, %q", c.SelectionKind(), c.CopyText())
	}
	c.EndDrag()

	c.PressAt(TablePos{Col: 1}, TableHitHeader)
	if c.SelectionKind() != TableSelectBlock || c.CopyText() != "r0c1\nr1c1\nr2c1\nr3c1" {
		t.Errorf("a header press: kind %v, %q", c.SelectionKind(), c.CopyText())
	}
	c.DragTo(TablePos{Row: 2, Col: 2})
	if c.CopyText() != "r0c1\tr0c2\nr1c1\tr1c2\nr2c1\tr2c2\nr3c1\tr3c2" {
		t.Errorf("a header drag: %q", c.CopyText())
	}
}
