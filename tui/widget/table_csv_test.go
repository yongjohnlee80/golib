package widget

import (
	"slices"
	"strings"
	"testing"
)

// csvOn is a TableCSV over a core holding text.
func csvOn(text string, comma rune) (*TableCSV, *EditorCore) {
	c := NewEditorCore(CoreInitialText(text))
	return NewTableCSV(c, comma), c
}

// undoes checks that one undo gives back before, and one redo after.
func undoes(t *testing.T, c *EditorCore, before string) {
	t.Helper()
	after := c.Value()
	c.Undo()
	if got := c.Value(); got != before {
		t.Fatalf("one undo: %q, want %q", got, before)
	}
	c.Redo()
	if got := c.Value(); got != after {
		t.Fatalf("redo: %q, want %q", got, after)
	}
}

func TestTableCSVReadsRecordsAndFields(t *testing.T) {
	src, _ := csvOn("name,note\nann,\"two\nlines\"\nbob,\"a, b\",extra\ncy\n", ',')
	if got := src.Fields(); len(got) != 3 || got[0].Title != "name" || got[1].Title != "note" || got[2].Title != "column 3" {
		t.Fatalf("fields: %+v", got)
	}
	if src.Len() != 3 || src.Cell(0, 1) != "two\nlines" || src.Cell(1, 1) != "a, b" || src.Cell(1, 2) != "extra" ||
		src.Cell(2, 1) != "" || src.ReadOnly() != "" {
		t.Fatalf("cells: %d %q %q %q %q, read only %q", src.Len(), src.Cell(0, 1), src.Cell(1, 1), src.Cell(1, 2), src.Cell(2, 1), src.ReadOnly())
	}
	tsv, _ := csvOn("a\tb\n1\t\"x\ty\"\n", '\t')
	if tsv.Cell(0, 1) != "x\ty" {
		t.Fatalf("a quoted tab in TSV: %q", tsv.Cell(0, 1))
	}
}

// TestTableCSVWritesOnlyTheRecordEdited (ADR 1792303119 result 2): a cell set rewrites its record's
// bytes and nothing else, one undo step; an unchanged value writes nothing.
func TestTableCSVWritesOnlyTheRecordEdited(t *testing.T) {
	const text = "h1,h2\n\"q\"\"uoted\",x\n\"multi\nline\",y\nlast,z"
	src, c := csvOn(text, ',')
	if !src.SetCell(1, 1, "Y") {
		t.Fatal("SetCell refused")
	}
	if want := "h1,h2\n\"q\"\"uoted\",x\n\"multi\nline\",Y\nlast,z"; c.Value() != want {
		t.Fatalf("after SetCell:\n%q\nwant\n%q", c.Value(), want)
	}
	undoes(t, c, text)
	c.Undo()

	rev := c.Revision()
	if !src.SetCell(0, 0, "q\"uoted") || c.Revision() != rev {
		t.Fatal("setting a cell to its own value wrote")
	}
	if !src.SetCell(-1, 0, "H1") || c.Value() != "H1,h2\n\"q\"\"uoted\",x\n\"multi\nline\",y\nlast,z" {
		t.Fatalf("a title: %q", c.Value())
	}
}

// TestTableCSVKeepsTheGaps (r1 R4, result 2a): blank lines before the header and between records
// belong to no record. Editing or deleting the first, a middle and the last record leaves every
// gap and every neighbouring record byte-identical, and one undo restores the whole text; a
// blank-looking line inside a quoted field is its record's; a CRLF file is the core's LF text; a
// last record with no line ending stays without one.
func TestTableCSVKeepsTheGaps(t *testing.T) {
	const text = "\n\nh,v\n\na,1\n\n\nb,\"two\n\nlines\"\n\nc,3"
	for _, c := range []struct {
		name string
		do   func(*TableCSV) bool
		want string
	}{
		{"edit the first", func(s *TableCSV) bool { return s.SetCell(0, 1, "x") }, "\n\nh,v\n\na,x\n\n\nb,\"two\n\nlines\"\n\nc,3"},
		{"edit the middle", func(s *TableCSV) bool { return s.SetCell(1, 0, "B") }, "\n\nh,v\n\na,1\n\n\nB,\"two\n\nlines\"\n\nc,3"},
		{"edit the last", func(s *TableCSV) bool { return s.SetCell(2, 1, "x") }, "\n\nh,v\n\na,1\n\n\nb,\"two\n\nlines\"\n\nc,x"},
		{"edit the header", func(s *TableCSV) bool { return s.SetCell(-1, 0, "H") }, "\n\nH,v\n\na,1\n\n\nb,\"two\n\nlines\"\n\nc,3"},
		{"delete the first", func(s *TableCSV) bool { return s.DeleteRow(0) }, "\n\nh,v\n\n\n\nb,\"two\n\nlines\"\n\nc,3"},
		{"delete the middle", func(s *TableCSV) bool { return s.DeleteRow(1) }, "\n\nh,v\n\na,1\n\n\n\nc,3"},
		{"delete the last", func(s *TableCSV) bool { return s.DeleteRow(2) }, "\n\nh,v\n\na,1\n\n\nb,\"two\n\nlines\"\n\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src, core := csvOn(text, ',')
			if src.Len() != 3 || src.Cell(1, 1) != "two\n\nlines" {
				t.Fatalf("read: %d records, %q", src.Len(), src.Cell(1, 1))
			}
			if !c.do(src) {
				t.Fatal("refused")
			}
			if got := core.Value(); got != c.want {
				t.Fatalf("\n got %q\nwant %q", got, c.want)
			}
			undoes(t, core, text)
		})
	}

	src, core := csvOn("h,v\r\na,1\r\n\r\nb,2\r\n", ',')
	if !src.SetCell(1, 1, "x") || core.Value() != "h,v\na,1\n\nb,x\n" {
		t.Fatalf("a CRLF file, as the core holds it: %q", core.Value())
	}
	src, core = csvOn("only,record", ',')
	if src.Len() != 0 || !src.SetCell(-1, 1, "R") || core.Value() != "only,R" {
		t.Fatalf("the only record, no line ending: %q", core.Value())
	}
}

// TestTableCSVCommitsACompoundAsOneStep (R2): a new row and a new field with a cell set are one
// write and one undo step; the row takes the line ending before it; a field added rewrites every
// record but keeps the gaps; a refusal writes nothing.
func TestTableCSVCommitsACompoundAsOneStep(t *testing.T) {
	const text = "h,v\na,1\n\nb,2\n"
	src, c := csvOn(text, ',')
	if !src.Commit(TableOp{InsertRow: 2, InsertField: 2, Field: TableField{Title: "w"}, Row: 2, Col: 2, Text: "new"}) {
		t.Fatal("the corner refused")
	}
	if want := "h,v,w\na,1,\n\nb,2,\n,,new\n"; c.Value() != want {
		t.Fatalf("the corner:\n got %q\nwant %q", c.Value(), want)
	}
	undoes(t, c, text)
	c.Undo()

	if !src.Commit(TableOp{InsertRow: 0, InsertField: -1, Row: 0, Col: 1, Text: "top"}) || c.Value() != "h,v\n,top\na,1\n\nb,2\n" {
		t.Fatalf("a row at the top: %q", c.Value())
	}
	c.Undo()

	src, c = csvOn("h,v\na,1", ',') // no line ending at the end: the new row's break goes before it
	if !src.Commit(TableOp{InsertRow: 1, InsertField: -1, Row: 1, Col: 0, Text: "z"}) || c.Value() != "h,v\na,1\nz," {
		t.Fatalf("a row after a last record with no line ending: %q", c.Value())
	}

	src, c = csvOn(text, ',')
	rev := c.Revision()
	for _, op := range []TableOp{
		{InsertRow: 9, InsertField: -1, Row: 0, Col: 0},
		{InsertRow: -1, InsertField: 5, Row: 0, Col: 0},
		{InsertRow: -1, InsertField: -1, Row: 0, Col: 7},
		{InsertRow: -1, InsertField: -1, Row: 4, Col: 0},
	} {
		if src.Commit(op) || c.Revision() != rev {
			t.Fatalf("%+v: accepted or wrote", op)
		}
	}
	if !src.DeleteField(1) || c.Value() != "h\na\n\nb\n" {
		t.Fatalf("DeleteField: %q", c.Value())
	}
	if src.DeleteField(0) {
		t.Fatal("the last field deleted")
	}
	if src.SetAlign(0, 0) {
		t.Fatal("CSV took an alignment")
	}
}

// TestTableCSVIsLossless (R1): values with newlines, tabs, quotes, delimiters and non-ASCII text
// are read exactly, written exactly, and a cell set to what it holds writes nothing.
func TestTableCSVIsLossless(t *testing.T) {
	values := []string{"line\nbreak", "tab\there", "say \"hi\"", "a, b", "ünï 字", " lead", ""}
	var b strings.Builder
	b.WriteString("v\n")
	for range values {
		b.WriteString("x\n")
	}
	src, c := csvOn(b.String(), ',')
	for i, v := range values {
		if !src.SetCell(i, 0, v) {
			t.Fatalf("%q refused", v)
		}
	}
	again := NewTableCSV(NewEditorCore(CoreInitialText(c.Value())), ',')
	for i, v := range values {
		if got := again.Cell(i, 0); got != v {
			t.Errorf("row %d read back %q, want %q", i, got, v)
		}
	}
	rev := c.Revision()
	for i, v := range values {
		src.SetCell(i, 0, v)
	}
	if c.Revision() != rev {
		t.Fatal("setting each cell to its own value wrote")
	}
}

// TestTableCSVReadOnly: a parse error (strict quoting), a read-only core, and a source past its
// limits refuse every edit and write nothing; a Raw edit is read at the next Sync.
func TestTableCSVReadOnly(t *testing.T) {
	for _, text := range []string{"h\n\"open\nmore\n", "h\n5\" disk\n"} { // unterminated; a bare quote
		src, c := csvOn(text, ',')
		if src.ReadOnly() == "" || src.SetCell(0, 0, "x") || src.InsertRow(0) || src.DeleteRow(0) || c.Value() != text {
			t.Fatalf("%q: read only %q, yet edited to %q", text, src.ReadOnly(), c.Value())
		}
	}
	src, c := csvOn("h\na\n", ',')
	c.SetReadOnly(true)
	if src.SetCell(0, 0, "x") || c.Value() != "h\na\n" {
		t.Fatal("a read-only core was edited")
	}
	c.SetReadOnly(false)
	c.SetValue("h\nb\nc\n")
	if !src.Sync() || src.Len() != 2 || src.Cell(1, 0) != "c" {
		t.Fatalf("Sync after a Raw change: %d records", src.Len())
	}
	long, _ := csvOn("h\n"+strings.Repeat("x", maxCSVLine+1)+"\n", ',')
	if long.ReadOnly() == "" || long.SetCell(0, 0, "y") {
		t.Fatal("a line past the limit is writable")
	}
}

// TestTableCSVInsertsAndDeletesRows: InsertRow writes an empty record of every field; DeleteRow
// removes one record's bytes alone.
func TestTableCSVInsertsAndDeletesRows(t *testing.T) {
	src, c := csvOn("a,b\n1,2\n3,4\n", ',')
	if !src.InsertRow(1) || c.Value() != "a,b\n1,2\n,\n3,4\n" {
		t.Fatalf("InsertRow: %q", c.Value())
	}
	if !src.DeleteRow(1) || c.Value() != "a,b\n1,2\n3,4\n" {
		t.Fatalf("DeleteRow: %q", c.Value())
	}
	if !src.InsertField(1, TableField{Title: "m"}) || c.Value() != "a,m,b\n1,,2\n3,,4\n" {
		t.Fatalf("InsertField: %q", c.Value())
	}
	if got := slices.Collect(func(yield func(string) bool) {
		for _, f := range src.Fields() {
			if !yield(f.Title) {
				return
			}
		}
	}); !slices.Equal(got, []string{"a", "m", "b"}) {
		t.Fatalf("fields after: %q", got)
	}
}
