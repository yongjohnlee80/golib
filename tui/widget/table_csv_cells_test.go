package widget

import "testing"

// TestTableCSVCommitCellsDeletesRecordsKeepingTheGaps: deleting records, alone or with other
// changes, removes each record's bytes and its line ending and nothing else: every gap stays, one
// undo gives the text back.
func TestTableCSVCommitCellsDeletesRecordsKeepingTheGaps(t *testing.T) {
	const text = "\nh\n\na\n\n\nb\nc\n\n\"d\nd\"\ne"
	for _, tc := range []struct {
		name string
		op   TableCellsOp
		want string
	}{
		{"only a deletion", TableCellsOp{DeleteRows: []int{1}}, "\nh\n\na\n\n\nc\n\n\"d\nd\"\ne"},
		{"the first", TableCellsOp{DeleteRows: []int{0}}, "\nh\n\n\n\nb\nc\n\n\"d\nd\"\ne"},
		{"adjacent", TableCellsOp{DeleteRows: []int{1, 2}}, "\nh\n\na\n\n\n\n\"d\nd\"\ne"},
		{"apart", TableCellsOp{DeleteRows: []int{0, 3}}, "\nh\n\n\n\nb\nc\n\ne"},
		{"the last, with no line ending", TableCellsOp{DeleteRows: []int{4}}, "\nh\n\na\n\n\nb\nc\n\n\"d\nd\"\n"},
		{"every record", TableCellsOp{DeleteRows: []int{0, 1, 2, 3, 4}}, "\nh\n\n\n\n\n"},
		{"a delete and an insert in its place", TableCellsOp{DeleteRows: []int{1}, InsertRows: []int{1},
			Cells: []TableCell{{Row: 1, Col: 0, Text: "B"}}}, "\nh\n\na\nB\n\n\nc\n\n\"d\nd\"\ne"}, // an insert follows the record before it, as InsertRow's
		{"the last deleted, one inserted after it", TableCellsOp{DeleteRows: []int{4}, InsertRows: []int{5},
			Cells: []TableCell{{Row: 4, Col: 0, Text: "E"}}}, "\nh\n\na\n\n\nb\nc\n\n\"d\nd\"\nE\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, c := csvOn(text, ',')
			if !src.CommitCells(tc.op) {
				t.Fatal("CommitCells refused")
			}
			if got := c.Value(); got != tc.want {
				t.Fatalf("\n got %q\nwant %q", got, tc.want)
			}
			undoes(t, c, text)
		})
	}
}

// TestTableCSVCommitCellsSetsManyCellsInOneStep: cells over several records, a title, a short
// record padded up to its cell and no further, an inserted field's cells, in one write.
func TestTableCSVCommitCellsSetsManyCellsInOneStep(t *testing.T) {
	const text = "a,b,c\n1,2,3\n4\n7,8,9\n"
	src, c := csvOn(text, ',')
	if !src.CommitCells(TableCellsOp{Cells: []TableCell{{-1, 0, "A"}, {0, 1, "two"}, {1, 1, "five"}, {2, 2, "nine"}}}) {
		t.Fatal("refused")
	}
	if want := "A,b,c\n1,two,3\n4,five\n7,8,nine\n"; c.Value() != want {
		t.Fatalf("\n got %q\nwant %q", c.Value(), want)
	}
	undoes(t, c, text)
	c.Undo()

	if !src.CommitCells(TableCellsOp{InsertFields: []TableFieldAt{{At: 1, Field: TableField{Title: "x"}}},
		DeleteFields: []int{2}, Cells: []TableCell{{0, 1, "X"}}}) {
		t.Fatal("refused")
	}
	if want := "a,x,b\n1,X,2\n4,\n7,,8\n"; c.Value() != want { // a short record padded to the field inserted, as InsertField pads
		t.Fatalf("fields moved:\n got %q\nwant %q", c.Value(), want)
	}
	undoes(t, c, text)
}

// TestTableCSVCommitCellsRefusesWhole: an op with a bad step anywhere writes nothing.
func TestTableCSVCommitCellsRefusesWhole(t *testing.T) {
	const text = "a,b\n1,2\n3,4\n"
	for _, op := range []TableCellsOp{
		{DeleteRows: []int{0, 0}},
		{DeleteRows: []int{2}},
		{DeleteFields: []int{0, 1}},
		{InsertRows: []int{3}},
		{InsertFields: []TableFieldAt{{At: 3}}},
		{DeleteRows: []int{0}, Cells: []TableCell{{0, 0, "x"}, {1, 0, "past the last row"}}},
		{Cells: []TableCell{{0, 0, "x"}, {0, 2, "past the last field"}}},
	} {
		src, c := csvOn(text, ',')
		rev := c.Revision()
		if src.CommitCells(op) || c.Value() != text || c.Revision() != rev {
			t.Fatalf("%+v: wrote %q", op, c.Value())
		}
	}
	src, c := csvOn(text, ',')
	rev := c.Revision()
	if !src.CommitCells(TableCellsOp{Cells: []TableCell{{0, 0, "1"}}}) || c.Revision() != rev {
		t.Fatal("an op that changes nothing wrote")
	}
}
