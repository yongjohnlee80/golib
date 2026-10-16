package widget

import (
	"slices"
	"strconv"
	"testing"
)

// where is text parsed over c's fields; the test fails when it does not parse.
func where(t *testing.T, c *TableCore, text string) TablePredicate {
	t.Helper()
	p, err := ParseTableWhere(text, c.Fields())
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return p
}

// letters is a source of rows (a, 1) … (e, 5): names and numbers, without kinds.
func letters() *rowsSource {
	s := &rowsSource{fields: []TableField{{Title: "name"}, {Title: "n"}}}
	for i, l := range []string{"a", "b", "c", "d", "e"} {
		s.rows = append(s.rows, []string{l, strconv.Itoa(i + 1)})
	}
	return s
}

// The display filters, then sorts: a sort keeps the filter, the filter keeps the sort, and the
// predicate reads each row's own cells whatever its place in the sort.
func TestTableFilter_FiltersThenSorts(t *testing.T) {
	c := NewTableCore(people()) // carol 41, alice 9, bob 100
	c.SetFilter(where(t, c, "age > 10"))
	if got := ids(c); !slices.Equal(got, []string{"carol", "bob"}) {
		t.Errorf("filtered: %v", got)
	}
	c.SortKeys([]TableSortKey{{Col: 1, Desc: true}})
	if got := ids(c); !slices.Equal(got, []string{"bob", "carol"}) {
		t.Errorf("filtered, then sorted by age DESC: %v", got)
	}
	c.SortKeys([]TableSortKey{{Col: 1}}) // alice 9, carol 41, bob 100
	c.SetFilter(where(t, c, "name = 'carol' OR name = 'bob'"))
	if got := ids(c); !slices.Equal(got, []string{"carol", "bob"}) {
		t.Errorf("sorted by age, filtered by name: %v", got)
	}
	c.SetFilter(nil)
	if got := ids(c); !slices.Equal(got, []string{"alice", "carol", "bob"}) {
		t.Errorf("the filter cleared: %v, want every row, still sorted", got)
	}
	if shown, loaded := c.Filtered(); shown != 3 || loaded != 3 {
		t.Errorf("Filtered %d of %d without a filter", shown, loaded)
	}
}

// Rows that arrive later are filtered and merged into the sort, and Filtered counts both.
func TestTableFilter_RowsArrivingLaterAreFiltered(t *testing.T) {
	p := &pagedSource{rowsSource: rowsSource{fields: []TableField{{Title: "n"}, {Title: "r"}}}, total: 30}
	c := NewTableCore(p, TableFetchAhead(5))
	p.arrive(c) // 0..4; asked again at once
	c.SetFilter(where(t, c, "n >= 3 AND n < 12"))
	if got := ids(c); !slices.Equal(got, []string{"3", "4"}) {
		t.Fatalf("filtered: %v", got)
	}
	p.arrive(c) // 5..9
	if got := ids(c); !slices.Equal(got, []string{"3", "4", "5", "6", "7", "8", "9"}) {
		t.Errorf("after rows arrived: %v, want the new ones filtered too", got)
	}
	if shown, loaded := c.Filtered(); shown != 7 || loaded != 10 {
		t.Errorf("Filtered %d of %d, want 7 of 10", shown, loaded)
	}
	c.SortKeys([]TableSortKey{{Col: 0, Desc: true}})
	c.SetCursor(TablePos{Row: 6}) // row 3, the last shown; its place among the 10 loaded rows is 6
	p.arrive(c)                   // 10..14
	if got := ids(c); !slices.Equal(got, []string{"11", "10", "9", "8", "7", "6", "5", "4", "3"}) {
		t.Errorf("after rows arrived under a sort: %v, want them filtered and merged", got)
	}
	if c.Len() != 9 {
		t.Errorf("Len %d, want the 9 rows shown", c.Len())
	}
}

// The cursor stays on its source row while it is shown, and goes to the first row when the
// filter hides it; TableOnCursor hears only a real move.
func TestTableFilter_KeepsOrMovesTheCursor(t *testing.T) {
	var told []int
	c := NewTableCore(letters(), TableOnCursor(func(r int) { told = append(told, r) }))
	c.SetCursor(TablePos{Row: 3, Col: 1}) // d
	told = nil
	c.SetFilter(where(t, c, "n > 2")) // c d e
	if got := c.Cursor(); got != (TablePos{Row: 1, Col: 1}) || c.Cell(got.Row, 0) != "d" {
		t.Errorf("cursor %+v on %q, want row 1, still on d, in its field", got, c.Cell(got.Row, 0))
	}
	if len(told) != 0 {
		t.Errorf("TableOnCursor told %v; the cursor never left d", told)
	}
	c.SetFilter(where(t, c, "n <> 4")) // a b c e: d hidden
	if got := c.Cursor(); got.Row != 0 || c.Cell(0, 0) != "a" {
		t.Errorf("cursor %+v, want row 0 once its row is hidden", got)
	}
	if !slices.Equal(told, []int{0}) {
		t.Errorf("TableOnCursor told %v, want [0]", told)
	}
	c.SetFilter(where(t, c, "n > 0"))
	c.SelectSourceRow(4)
	c.SetFilter(where(t, c, "name LIKE '_'")) // every row
	if c.SourceRow(c.Cursor().Row) != 4 {
		t.Errorf("a filter keeping every row moved the cursor off e: %d", c.SourceRow(c.Cursor().Row))
	}
	c.SetFilter(where(t, c, "n < 3"))
	c.SelectSourceRow(4) // hidden
	if c.Cursor().Row != 0 {
		t.Errorf("SelectSourceRow of a hidden row: cursor %+v, want row 0", c.Cursor())
	}
}

// A filter change ends a selection, and the selection, the row numbers and the copies take the
// rows shown.
func TestTableFilter_SelectionAndCopiesTakeTheShownRows(t *testing.T) {
	c := NewTableCore(letters(), TableRowNumbers(true))
	keys(c, text("V"), text("j"))
	c.SetFilter(where(t, c, "n IS NOT NULL")) // no kinds: every row
	if _, _, ok := c.Selection(); ok {
		t.Error("a filter change kept the selection")
	}
	c.SetFilter(where(t, c, "name <> 'b' AND name <> 'd'")) // a c e
	c.SelectAll()
	if got := c.CopyText(); got != "a\t1\nc\t3\ne\t5" {
		t.Errorf("all rows copied as %q, want the shown rows only", got)
	}
	if !c.InSelection(2, 0) || c.InSelection(3, 0) {
		t.Error("InSelection does not run over the shown rows")
	}
	if got := c.Rows(0, 10); len(got) != 3 || got[1][0] != "c" {
		t.Errorf("Rows %v", got)
	}
	c.SetAnchor(nil)
	c.SetCursor(TablePos{Row: 1, Col: 1})
	if got := c.CopyText(); got != "3" {
		t.Errorf("the cursor's cell copied as %q, want c's 3", got)
	}
	big := &rowsSource{fields: []TableField{{Title: "n"}}}
	for i := range 12 {
		big.rows = append(big.rows, []string{strconv.Itoa(i)})
	}
	c.SetSource(big)
	if c.RowNumberDigits() != 2 {
		t.Fatalf("12 rows: %d digits", c.RowNumberDigits())
	}
	c.SetFilter(where(t, c, "n < 9"))
	if c.RowNumberDigits() != 1 {
		t.Errorf("9 rows shown of 12: %d digits, want the shown count's", c.RowNumberDigits())
	}
}

// A filter keeping no row empties the table: no cursor row, nothing selected or copied. The next
// filter puts the cursor on its first row.
func TestTableFilter_TheEmptyView(t *testing.T) {
	var told []int
	c := NewTableCore(letters(), TableOnCursor(func(r int) { told = append(told, r) }))
	c.SetCursor(TablePos{Row: 3, Col: 1}) // d
	keys(c, text("v"), text("k"))
	told = nil
	c.SetFilter(where(t, c, "n > 99"))
	if c.Len() != 0 || c.Cell(0, 0) != "" || c.CopyText() != "" || len(c.Rows(0, 5)) != 0 {
		t.Errorf("the empty view: Len %d, CopyText %q", c.Len(), c.CopyText())
	}
	if _, _, ok := c.Selection(); ok || c.InSelection(0, 0) {
		t.Error("the empty view kept a selection")
	}
	keys(c, text("V"))
	c.SelectAll()
	keys(c, ctrlV)
	if _, _, ok := c.Selection(); ok || c.CopyText() != "" {
		t.Errorf("the empty view selected: copy %q", c.CopyText())
	}
	if shown, loaded := c.Filtered(); shown != 0 || loaded != 5 {
		t.Errorf("Filtered %d of %d", shown, loaded)
	}
	c.SetFilter(where(t, c, "n > 1")) // b c d e: d was the cursor's row before the empty view
	if got := c.Cursor().Row; got != 0 || c.Cell(0, 0) != "b" {
		t.Errorf("after the empty view the cursor is on row %d, want 0", got)
	}
	if !slices.Equal(told, []int{1}) {
		t.Errorf("TableOnCursor told %v, want [1], b", told)
	}
	c.SetFilter(where(t, c, "n > 99"))
	c.SetFilter(nil)
	if got := c.Cursor().Row; got != 0 {
		t.Errorf("the filter cleared after the empty view: row %d, want 0", got)
	}
}

// A filter asks a pager for nothing the unfiltered view would not: hiding rows does not bring the
// cursor near the end of the loaded rows, but a shown row near their end does.
func TestTableFilter_FetchesNothing(t *testing.T) {
	p := &pagedSource{rowsSource: rowsSource{fields: []TableField{{Title: "n"}, {Title: "r"}}}, total: 1000}
	c := NewTableCore(p, TableFetchAhead(10))
	p.arrive(c) // 0..9; asked again
	p.arrive(c) // 10..19; the cursor at 0 is far from 20
	if p.asked != 2 {
		t.Fatalf("asked %d, want 2", p.asked)
	}
	c.SetFilter(where(t, c, "n < 3")) // three rows shown of twenty
	keys(c, text("G"), text("k"), text("G"), text("g"), text("g"))
	c.SetFilter(where(t, c, "n = 1"))
	if p.asked != 2 {
		t.Errorf("a filter hiding most rows asked the pager: %d", p.asked)
	}
	c.SetFilter(where(t, c, "n > 15")) // 16..19: the cursor lands within 10 of the end
	if p.asked != 2 {
		t.Errorf("a filter change asked the pager: %d", p.asked)
	}
	keys(c, text("j")) // a move does: the cursor's row, 17, is within 10 of the end
	if p.asked != 3 {
		t.Errorf("a move to a shown row near the end of the loaded rows did not ask: %d", p.asked)
	}
}

// A predicate naming a field the source lacks is dropped, when set and when the field goes.
func TestTableFilter_AFilterOnAMissingFieldIsDropped(t *testing.T) {
	c := NewTableCore(people())
	p := where(t, c, "city = 'Seoul'")
	c.SetSource(letters()) // two fields
	c.SetFilter(p)
	if shown, _ := c.Filtered(); shown != 5 {
		t.Errorf("a filter on field 3 of 2 kept %d rows", shown)
	}
	src := people()
	c.SetSource(src)
	c.SetFilter(where(t, c, "city = 'Seoul'"))
	if c.Len() != 1 {
		t.Fatalf("Len %d", c.Len())
	}
	src.fields = src.fields[:2]
	for i := range src.rows {
		src.rows[i] = src.rows[i][:2]
	}
	c.Changed()
	if c.Len() != 3 {
		t.Errorf("after its field was removed the filter kept %d rows, want it dropped", c.Len())
	}
	c.SetFilter(where(t, c, "age > 10"))
	c.SetSource(people())
	if c.Len() != 3 {
		t.Errorf("a new source kept the filter: %d rows", c.Len())
	}
}
