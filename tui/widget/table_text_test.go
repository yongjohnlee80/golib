package widget

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

const tableDoc = "intro\n| name | n |\n|:---|--:|\n| bob | 10 |\n| alice | 9 |\nafter"

func tableCore(t *testing.T, doc string) (*EditorCore, *TableText) {
	t.Helper()
	c := NewEditorCore()
	c.SetValue(doc)
	tt, ok := TableAt(c, 3)
	if !ok {
		t.Fatalf("no table at line 3 of %q", doc)
	}
	return c, tt
}

func TestTableAt_FindsTheWholeTable(t *testing.T) {
	c := NewEditorCore()
	c.SetValue(tableDoc)
	for ln := 1; ln <= 4; ln++ {
		tt, ok := TableAt(c, ln)
		if !ok {
			t.Fatalf("line %d: no table", ln)
		}
		if top, end := tt.Lines(); top != 1 || end != 4 {
			t.Errorf("line %d: lines %d-%d, want 1-4", ln, top, end)
		}
	}
	for _, ln := range []int{0, 5} {
		if _, ok := TableAt(c, ln); ok {
			t.Errorf("line %d is off the table", ln)
		}
	}
	c.SetValue("| a | b |\n| c | d |")
	if _, ok := TableAt(c, 0); ok {
		t.Error("rows without a delimiter row are not a table")
	}
}

func TestTableText_ReadsCellsAndAlignment(t *testing.T) {
	_, tt := tableCore(t, tableDoc)
	want := []TableField{{Title: "name", Align: style.AlignLeft}, {Title: "n", Align: style.AlignRight}}
	if got := tt.Fields(); !reflect.DeepEqual(got, want) {
		t.Errorf("fields %+v, want %+v", got, want)
	}
	if tt.Len() != 2 || tt.Cell(1, 0) != "alice" || tt.Cell(0, 1) != "10" {
		t.Errorf("Len %d, cells %q %q", tt.Len(), tt.Cell(1, 0), tt.Cell(0, 1))
	}
	if tt.Cell(0, 5) != "" || tt.Cell(9, 0) != "" {
		t.Error("a cell off the table is not empty")
	}
}

// Format pads every column to its widest cell, places each cell as its column is aligned, writes
// the delimiter row from the alignment, and is one undo step.
func TestTableText_FormatIsOneUndoStep(t *testing.T) {
	c, tt := tableCore(t, tableDoc)
	if !tt.Format() {
		t.Fatal("Format refused")
	}
	want := "intro\n| name  |   n |\n| ----- | --: |\n| bob   |  10 |\n| alice |   9 |\nafter"
	if got := c.Value(); got != want {
		t.Fatalf("formatted:\n%s\nwant:\n%s", got, want)
	}
	c.Undo()
	if got := c.Value(); got != tableDoc {
		t.Errorf("one undo left:\n%s", got)
	}
}

func TestTableText_EditsTheTable(t *testing.T) {
	c, tt := tableCore(t, tableDoc)
	if !tt.SetCell(0, 0, "a|b") || tt.Cell(0, 0) != `a\|b` {
		t.Fatalf("SetCell: %q; a | must be escaped to stay one cell", tt.Cell(0, 0))
	}
	if !tt.InsertRow(2) || tt.Len() != 3 || tt.Cell(2, 0) != "" {
		t.Errorf("InsertRow at the end: Len %d", tt.Len())
	}
	if !tt.DeleteRow(0) || tt.Len() != 2 || tt.Cell(0, 0) != "alice" {
		t.Errorf("DeleteRow 0: first row %q", tt.Cell(0, 0))
	}
	if !tt.InsertField(1, TableField{Title: "city", Align: style.AlignCenter}) {
		t.Fatal("InsertField refused")
	}
	if f := tt.Fields(); len(f) != 3 || f[1].Title != "city" || f[1].Align != style.AlignCenter || tt.Cell(0, 2) != "9" {
		t.Errorf("after InsertField: %+v, row 0 col 2 %q", f, tt.Cell(0, 2))
	}
	if !tt.SetAlign(0, style.AlignRight) || tt.Fields()[0].Align != style.AlignRight {
		t.Error("SetAlign did not take")
	}
	if !tt.DeleteField(1) || len(tt.Fields()) != 2 {
		t.Error("DeleteField did not take")
	}
	if top, end := tt.Lines(); c.LineAt(end+1) != "after" || c.LineAt(top-1) != "intro" {
		t.Errorf("the table's lines %d-%d no longer sit between intro and after", top, end)
	}
	for _, bad := range []bool{tt.SetCell(9, 0, "x"), tt.InsertRow(-1), tt.DeleteRow(9), tt.InsertField(9, TableField{}), tt.SetAlign(9, style.AlignLeft)} {
		if bad {
			t.Error("an edit off the table was taken")
		}
	}
	c.SetValue("| a |\n|---|\n| 1 |")
	one, _ := TableAt(c, 0)
	if one.DeleteField(0) {
		t.Error("the last column was deleted; a table needs one")
	}
}

func TestTableText_SortsItsRows(t *testing.T) {
	c, tt := tableCore(t, tableDoc)
	if !tt.SortBy(1, false) || tt.Cell(0, 0) != "alice" || tt.Cell(1, 0) != "bob" {
		t.Fatalf("by n: %q %q; 9 before 10 as numbers", tt.Cell(0, 0), tt.Cell(1, 0))
	}
	c.Undo()
	if c.Value() != tableDoc {
		t.Error("the sort is not one undo step")
	}
}

func TestTableText_ReadOnlyRefuses(t *testing.T) {
	c, tt := tableCore(t, tableDoc)
	c.SetReadOnly(true)
	if tt.Format() || tt.SetCell(0, 0, "x") || c.Value() != tableDoc {
		t.Error("a read-only buffer was edited")
	}
}

// A TableCore over the text: the same operations, through the same core a results table uses.
func TestTableText_UnderATableCore(t *testing.T) {
	_, tt := tableCore(t, tableDoc)
	core := NewTableCore(tt)
	if !core.Editable() || core.Len() != 2 || core.Cell(1, 0) != "alice" {
		t.Fatalf("core over the text: editable %v, Len %d", core.Editable(), core.Len())
	}
	core.SortBy(1, false)
	if core.Cell(0, 0) != "alice" || tt.Cell(0, 0) != "bob" {
		t.Error("the core's sort is a view; the text keeps its order until SortBy writes it")
	}
}

func TestMarkdownDelimiterAligns(t *testing.T) {
	got, ok := MarkdownDelimiterAligns("| --- | :-- | :-: | --: |")
	want := []style.Align{style.AlignLeft, style.AlignLeft, style.AlignCenter, style.AlignRight}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("%v %v, want %v", got, ok, want)
	}
	if _, ok := MarkdownDelimiterAligns("| a | b |"); ok {
		t.Error("a row of text read as a delimiter row")
	}
}
