package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

const sampleTable = "| Column1 | Column2 |\n| --- | --- |\n| Item1.1 | Item2.1 |"

func cellTab(c *widget.EditorCore) { c.HandleKey(tui.KeyEvent{Code: tui.KeyTab}) }
func cellShiftTab(c *widget.EditorCore) {
	c.HandleKey(tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift})
}

func TestTabWalksATablesCellsAndTypingReplacesOne(t *testing.T) {
	c, _ := coreWith(t, sampleTable, widget.CoreTableEditing(true))
	keys(c, "i") // Insert at the first column
	cellTab(c)
	if got := c.TextIn(cellSelection(t, c)); got != "Column2" {
		t.Fatalf("Tab from the first cell selected %q, want Column2", got)
	}
	keys(c, "Name")
	if got := c.Value(); got != "| Column1 | Name |\n| --- | --- |\n| Item1.1 | Item2.1 |" {
		t.Fatalf("typing over the cell: %q", got)
	}
	cellTab(c) // past the row's end, over the delimiter row
	if got := c.TextIn(cellSelection(t, c)); got != "Item1.1" || c.Cursor().Line != 2 {
		t.Fatalf("Tab past the row's end: %q at %v", got, c.Cursor())
	}
	cellShiftTab(c)
	if got := c.TextIn(cellSelection(t, c)); got != "Name" {
		t.Fatalf("Shift+Tab back over the delimiter row: %q", got)
	}
	cellTab(c)
	c.HandleKey(tui.KeyEvent{Code: tui.KeyBackspace})
	if got := c.Value(); got != "| Column1 | Name |\n| --- | --- |\n|  | Item2.1 |" {
		t.Fatalf("Backspace over the cell: %q", got)
	}
	// past the table's last cell: a new row
	cellTab(c)
	cellTab(c)
	keys(c, "x")
	if got := c.Value(); got != "| Column1 | Name |\n| --- | --- |\n|  | Item2.1 |\n| x  |   |" {
		t.Fatalf("Tab past the last cell: %q", got)
	}
	// one undo per edit, never per Tab
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
	if _, ok := c.Selection(); ok {
		t.Fatal("Escape left a selection")
	}
}

func TestOffATableTabDoesWhatItAlwaysDid(t *testing.T) {
	c, _ := coreWith(t, "plain", widget.CoreTableEditing(true))
	keys(c, "i")
	cellTab(c)
	if got := c.Value(); got != "\tplain" {
		t.Fatalf("Tab off a table: %q", got)
	}
	off, _ := coreWith(t, sampleTable)
	keys(off, "i")
	cellTab(off)
	if got := off.Value()[:1]; got != "\t" {
		t.Fatalf("Tab with table editing off: %q", off.Value())
	}
}

func TestAnArrowDropsTheCellsSelection(t *testing.T) {
	c, _ := coreWith(t, sampleTable, widget.CoreTableEditing(true))
	keys(c, "i")
	cellTab(c)
	c.HandleKey(tui.KeyEvent{Code: tui.KeyLeft})
	keys(c, "!")
	if got := c.Value()[:22]; got != "| Column1 | Column!2 |" {
		t.Fatalf("after Left, typing replaced the old selection: %q", c.Value())
	}
}

// cellSelection is a table cell's selection: in Insert mode it is the core's only selection, read
// through Selected.
func cellSelection(t *testing.T, c *widget.EditorCore) widget.TextRange {
	t.Helper()
	ln := c.Cursor().Line
	line := []rune(c.LineAt(ln))
	from, to := -1, -1
	for col := range line {
		if c.Selected(ln, col) {
			if from < 0 {
				from = col
			}
			to = col + 1
		}
	}
	if from < 0 {
		t.Fatalf("no cell selected on line %d: %q", ln, c.LineAt(ln))
	}
	return widget.TextRange{From: widget.TextPos{Line: ln, Col: from}, To: widget.TextPos{Line: ln, Col: to}}
}

// Table editing turned on at run time walks the cells; turned off, it drops a cell's selection.
// Shift+Tab on the first cell has nowhere back to go and stays.
func TestTableEditingSwitchesAtRunTimeAndStopsAtTheFirstCell(t *testing.T) {
	c, _ := coreWith(t, sampleTable)
	if c.TableEditing() {
		t.Fatal("table editing on without being asked")
	}
	c.SetTableEditing(true)
	if !c.TableEditing() {
		t.Fatal("SetTableEditing(true) did not turn it on")
	}
	keys(c, "i")
	cellShiftTab(c)
	if got := c.Cursor(); got.Line != 0 {
		t.Fatalf("Shift+Tab on the first cell moved to %v", got)
	}
	if c.Value() != sampleTable {
		t.Fatalf("Shift+Tab on the first cell changed the text: %q", c.Value())
	}
	cellTab(c)
	if got := c.TextIn(cellSelection(t, c)); got != "Column2" {
		t.Fatalf("Tab selected %q, want Column2", got)
	}
	c.SetTableEditing(false)
	if c.TableEditing() {
		t.Error("SetTableEditing(false) left it on")
	}
	if _, ok := c.Selection(); ok {
		t.Error("turning table editing off kept the cell's selection")
	}
}
