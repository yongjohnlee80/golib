package widget

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// kindedSource is a TableSource that knows its cells' kinds: kinds[row][col] is the byte.
type kindedSource struct {
	rowsSource
	kinds []string
}

func (k *kindedSource) Kind(row, col int) TableCellKind { return TableCellKind(k.kinds[row][col]) }

// kinded is rows of (id, value) where value's kind is the row's own; id is text.
func kinded(rows ...[3]string) *kindedSource {
	s := &kindedSource{rowsSource: rowsSource{fields: []TableField{{Title: "id"}, {Title: "v"}}}}
	for _, r := range rows {
		s.rows = append(s.rows, []string{r[0], r[1]})
		s.kinds = append(s.kinds, "t"+r[2])
	}
	return s
}

// ids is the id field down the display rows.
func ids(c *TableCore) []string {
	out := make([]string, c.Len())
	for r := range out {
		out[r] = c.Cell(r, 0)
	}
	return out
}

// NULLs come last whichever way the field sorts; DESC reverses the rest.
func TestTableSort_NullsLastBothWays(t *testing.T) {
	src := kinded(
		[3]string{"a", "NULL", "z"}, [3]string{"b", "2", "n"}, [3]string{"c", "NULL", "z"},
		[3]string{"d", "1", "n"}, [3]string{"e", "3", "n"})
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"d", "b", "e", "a", "c"}) {
		t.Errorf("ascending: %v, want 1 2 3 then the NULLs in source order", got)
	}
	c.SortKeys([]TableSortKey{{Col: 1, Desc: true}})
	if got := ids(c); !slices.Equal(got, []string{"e", "b", "d", "a", "c"}) {
		t.Errorf("descending: %v, want 3 2 1 then the NULLs, still last and in source order", got)
	}
}

// Numbers sort as exact decimals from their literal: two that one float64 cannot tell apart keep
// their order, and a number longer than an int64 too.
func TestTableSort_NumbersAreExactDecimals(t *testing.T) {
	src := kinded(
		[3]string{"hi", "12345678901234567890.124", "n"},
		[3]string{"lo", "12345678901234567890.123", "n"},
		[3]string{"neg", "-5e-1", "n"},
		[3]string{"big", "123456789012345678901234567890", "n"},
		[3]string{"nan", "NaN", "n"},
		[3]string{"two", " 2 ", "n"})
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"neg", "two", "lo", "hi", "big", "nan"}) {
		t.Errorf("ascending: %v; want -0.5, 2, …890.123 before …890.124, the 30 digits, then NaN", got)
	}
	c.SortKeys([]TableSortKey{{Col: 1, Desc: true}})
	if got := ids(c); !slices.Equal(got, []string{"nan", "big", "hi", "lo", "two", "neg"}) {
		t.Errorf("descending: %v", got)
	}
}

// One field holding several kinds orders them number, bool, text, JSON; false before true; and
// rows equal on the key keep the source's order.
func TestTableSort_MixedKindsAndStable(t *testing.T) {
	src := kinded(
		[3]string{"j", `{"a":1}`, "j"}, [3]string{"t1", "pear", "t"}, [3]string{"true", "true", "b"},
		[3]string{"n", "10", "n"}, [3]string{"t2", "apple", "t"}, [3]string{"false", "false", "b"},
		[3]string{"t3", "pear", "t"}, [3]string{"z", "", "z"}, [3]string{"t4", "pear", "t"})
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"n", "false", "true", "t2", "t1", "t3", "t4", "j", "z"}) {
		t.Errorf("mixed: %v; want the number, false, true, the text (pears in source order), JSON, NULL", got)
	}
}

// A sort asks a pager for nothing, though it lands the cursor near the end of the loaded rows: SortKeys,
// SortBy and the s key alike. Moving the cursor there afterwards does ask.
func TestTableSort_FetchesNothing(t *testing.T) {
	p := &pagedSource{rowsSource: rowsSource{fields: []TableField{{Title: "n"}, {Title: "r"}}}, total: 1000}
	c := NewTableCore(p, TableFetchAhead(10), TableSortable(true))
	p.arrive(c) // 0..9; asked again
	p.arrive(c) // 10..19
	if p.asked != 2 {
		t.Fatalf("asked %d, want 2", p.asked)
	}
	c.SortKeys([]TableSortKey{{Col: 0, Desc: true}}) // row 0, the cursor's, sorts last: display row 19
	if c.Cursor().Row != 19 || p.asked != 2 {
		t.Errorf("SortKeys: cursor row %d, asked %d; want 19 and no ask", c.Cursor().Row, p.asked)
	}
	c.SortBy(-1, false)
	c.SortBy(0, true)
	if c.Cursor().Row != 19 || p.asked != 2 {
		t.Errorf("SortBy: cursor row %d, asked %d", c.Cursor().Row, p.asked)
	}
	c.SortKeys(nil)
	keys(c, text("s"), text("s")) // up, then down: the cursor's row 0 lands on display row 19
	if c.Cursor().Row != 19 || p.asked != 2 {
		t.Errorf("the s key: cursor row %d, asked %d", c.Cursor().Row, p.asked)
	}
	keys(c, text("k"))
	if p.asked != 3 {
		t.Errorf("a move near the end did not ask: %d", p.asked)
	}
}

// A decimal whose exponent is out of the filter's range (1e999999999) is not a number to the sort:
// it is read at once, never expanded, and sorts as text does, with kinds (after the numbers) and
// without (by its text).
func TestTableSort_AnOutOfRangeExponentIsText(t *testing.T) {
	start := time.Now()
	src := kinded([3]string{"huge", "1e999999999", "n"}, [3]string{"two", "2", "n"}, [3]string{"one", "1", "n"})
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"one", "two", "huge"}) {
		t.Errorf("kinded: %v; want 1, 2, then the unparsed", got)
	}
	c.SetSource(&rowsSource{fields: []TableField{{Title: "id"}, {Title: "v"}}, rows: [][]string{
		{"5", "5"}, {"huge", "1e999999999"}, {"4", "4"},
	}})
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"huge", "4", "5"}) {
		t.Errorf("without kinds: %v; want 1e999999999 by its text, before the digits it outsorts", got)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("sorting took %v", d)
	}
}

// The sort reads numbers as the WHERE filter does: a fraction or a base prefix, which big.Rat
// would read, is not a decimal; valid decimals still sort exactly, with kinds and without.
func TestTableSort_NumbersAreTheFiltersDecimals(t *testing.T) {
	src := kinded(
		[3]string{"hi", "12345678901234567890.124", "n"}, [3]string{"half", "1/2", "n"},
		[3]string{"lo", "12345678901234567890.123", "n"}, [3]string{"hex", "0x10", "n"},
		[3]string{"one", "1", "n"})
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"one", "lo", "hi", "hex", "half"}) {
		t.Errorf("kinded: %v; want 1, …123, …124, then the unparsed by their text", got)
	}
	c.SetSource(&rowsSource{fields: []TableField{{Title: "id"}, {Title: "v"}}, rows: [][]string{
		{"hi", "12345678901234567890.124"}, {"lo", "12345678901234567890.123"}, {"two", " 2 "},
	}})
	c.SortKeys([]TableSortKey{{Col: 1}})
	if got := ids(c); !slices.Equal(got, []string{"two", "lo", "hi"}) {
		t.Errorf("without kinds: %v; want 2, …123, …124, exactly", got)
	}
}

// Several keys apply in order: the second breaks the first's ties, the third the second's.
func TestTableSort_SeveralKeysApplyInOrder(t *testing.T) {
	src := &kindedSource{
		rowsSource: rowsSource{
			fields: []TableField{{Title: "id"}, {Title: "dept"}, {Title: "pay"}},
			rows: [][]string{
				{"1", "ops", "10"}, {"2", "dev", "30"}, {"3", "ops", "30"},
				{"4", "dev", "30"}, {"5", "ops", "20"}, {"6", "dev", "5"},
			},
		},
		kinds: []string{"ntn", "ntn", "ntn", "ntn", "ntn", "ntn"},
	}
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1}, {Col: 2, Desc: true}, {Col: 0, Desc: true}})
	if got := ids(c); !slices.Equal(got, []string{"4", "2", "6", "3", "5", "1"}) {
		t.Errorf("dept, pay DESC, id DESC: %v", got)
	}
	keys, partial := c.SortedKeys()
	if !reflect.DeepEqual(keys, []TableSortKey{{Col: 1}, {Col: 2, Desc: true}, {Col: 0, Desc: true}}) || partial {
		t.Errorf("SortedKeys %+v partial=%v", keys, partial)
	}
	if col, desc, _ := c.Sorted(); col != 1 || desc {
		t.Errorf("Sorted %d %v, want the first key", col, desc)
	}
	// without kinds the same keys order by compareCells, numbers still as numbers
	plain := &rowsSource{fields: src.fields, rows: src.rows}
	c.SetSource(plain)
	c.SortKeys([]TableSortKey{{Col: 1}, {Col: 2, Desc: true}, {Col: 0, Desc: true}})
	if got := ids(c); !slices.Equal(got, []string{"4", "2", "6", "3", "5", "1"}) {
		t.Errorf("without kinds: %v", got)
	}
}

// Without kinds a field orders as it always has: numbers as numbers when both are, else text, and
// "NULL" is only text.
func TestTableSort_WithoutKindsIsUnchanged(t *testing.T) {
	src := &rowsSource{fields: []TableField{{Title: "v"}}, rows: [][]string{{"10"}, {"NULL"}, {"9"}, {"abc"}, {"-1"}}}
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 0}})
	if got := ids(c); !slices.Equal(got, []string{"-1", "9", "10", "NULL", "abc"}) {
		t.Errorf("without kinds: %v", got)
	}
}

// Rows arriving under a kinded sort are merged into it.
func TestTableSort_RowsArrivingMergeIntoAKindedSort(t *testing.T) {
	src := kinded([3]string{"b", "2", "n"}, [3]string{"n", "", "z"})
	c := NewTableCore(src)
	c.SortKeys([]TableSortKey{{Col: 1, Desc: true}})
	src.rows = append(src.rows, []string{"c", "3"}, []string{"a", "1"})
	src.kinds = append(src.kinds, "tn", "tn")
	c.Changed()
	if got := ids(c); !slices.Equal(got, []string{"c", "b", "a", "n"}) {
		t.Errorf("after rows arrived: %v", got)
	}
}

// s sorts by one key, replacing a sort of several, and TableOnSort hears every change once.
func TestTableSort_TheSKeyReplacesSeveralKeysAndIsHeard(t *testing.T) {
	var heard [][]TableSortKey
	c := NewTableCore(people(), TableSortable(true), TableOnSort(func(k []TableSortKey) { heard = append(heard, k) }))
	c.SortKeys([]TableSortKey{{Col: 0}, {Col: 1}})
	c.SortKeys([]TableSortKey{{Col: 0}, {Col: 1}}) // the same: nothing to hear
	c.SetCursor(TablePos{0, 0})
	c.HandleKey(text("s"))
	c.HandleKey(text("s"))
	c.HandleKey(text("s"))
	want := [][]TableSortKey{{{Col: 0}, {Col: 1}}, {{Col: 0}}, {{Col: 0, Desc: true}}, nil}
	if !reflect.DeepEqual(heard, want) {
		t.Errorf("heard %+v, want %+v", heard, want)
	}
	c.SortKeys([]TableSortKey{{Col: 2}})
	heard = nil
	c.SetSource(people())
	if !reflect.DeepEqual(heard, [][]TableSortKey{nil}) {
		t.Errorf("a new source ending the sort was heard as %+v", heard)
	}
}

// A field's sort mark is its key's direction and place; the header's note, "(loaded rows)", is on
// the first key alone, and only while rows remain to load.
func TestASortMarkIsItsKeysDirectionAndPlace(t *testing.T) {
	p := &pagedSource{rowsSource: rowsSource{fields: []TableField{{Title: "n"}, {Title: "r"}}}, total: 30}
	c := NewTableCore(p, TableFetchAhead(5))
	p.arrive(c)
	c.SortKeys([]TableSortKey{{Col: 1, Desc: true}, {Col: 0}})
	for _, tc := range []struct {
		col   int
		mark  string
		place int
		ok    bool
		note  string
	}{
		{1, "▼", 0, true, "(loaded rows)"},
		{0, "▲", 1, true, ""},
		{2, "", 0, false, ""},
	} {
		mark, place, ok := c.SortMark(tc.col)
		if mark != tc.mark || place != tc.place || ok != tc.ok {
			t.Errorf("SortMark(%d) = %q, %d, %v; want %q, %d, %v", tc.col, mark, place, ok, tc.mark, tc.place, tc.ok)
		}
		if note := c.HeaderNote(tc.col); note != tc.note {
			t.Errorf("HeaderNote(%d) = %q, want %q", tc.col, note, tc.note)
		}
	}
	whole := NewTableCore(&rowsSource{fields: p.fields, rows: p.rows}) // every row loaded
	whole.SortKeys([]TableSortKey{{Col: 1, Desc: true}})
	if note := whole.HeaderNote(1); note != "" {
		t.Errorf("every row loaded: HeaderNote(1) = %q, want none", note)
	}
}
