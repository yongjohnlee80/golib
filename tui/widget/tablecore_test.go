package widget

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// rowsSource is an in-memory TableSource; pagedSource adds a pager that loads a page of rows each
// time it is asked, when the test says the page has arrived.
type rowsSource struct {
	fields []TableField
	rows   [][]string
}

func (s *rowsSource) Fields() []TableField     { return s.fields }
func (s *rowsSource) Len() int                 { return len(s.rows) }
func (s *rowsSource) Cell(row, col int) string { return s.rows[row][col] }

type pagedSource struct {
	rowsSource
	total, asked, inFlight int
}

func (p *pagedSource) More() bool { return len(p.rows) < p.total }
func (p *pagedSource) FetchMore(n int) {
	if p.inFlight > 0 {
		return
	}
	p.asked++
	p.inFlight = n
}

// arrive delivers the page asked for, as a pager's answer would, and tells the core.
func (p *pagedSource) arrive(c *TableCore) {
	for range p.inFlight {
		if len(p.rows) >= p.total {
			break
		}
		i := len(p.rows)
		p.rows = append(p.rows, []string{strconv.Itoa(i), "r" + strconv.Itoa(i)})
	}
	p.inFlight = 0
	c.Changed()
}

type fakeTableLayout struct {
	revealed TablePos
	changed  int
}

func (l *fakeTableLayout) Reveal(row, col int)  { l.revealed = TablePos{row, col} }
func (l *fakeTableLayout) PageRows() int        { return 5 }
func (l *fakeTableLayout) Measure(s string) int { return len([]rune(s)) }
func (l *fakeTableLayout) Changed()             { l.changed++ }

func people() *rowsSource {
	return &rowsSource{
		fields: []TableField{{Title: "name"}, {Title: "age"}, {Title: "city", Width: 6}},
		rows: [][]string{
			{"carol", "41", "Seoul"},
			{"alice", "9", "Busan"},
			{"bob", "100", "Incheon"},
		},
	}
}

func key(code rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: code} }
func text(s string) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Text: s} }

func TestTableCore_MovesAndClamps(t *testing.T) {
	c := NewTableCore(people())
	l := &fakeTableLayout{}
	c.Bind(nil, l)
	for _, k := range []tui.KeyEvent{text("j"), text("l"), key(tui.KeyDown), key(tui.KeyDown), text("l"), text("l")} {
		c.HandleKey(k)
	}
	if got := c.Cursor(); got != (TablePos{2, 2}) {
		t.Fatalf("cursor %+v, want the last cell {2 2}", got)
	}
	if l.revealed != (TablePos{2, 2}) {
		t.Errorf("revealed %+v, want the cursor's cell", l.revealed)
	}
	c.HandleKey(text("g"))
	c.HandleKey(text("0"))
	if got := c.Cursor(); got != (TablePos{0, 0}) {
		t.Errorf("g 0: cursor %+v, want {0 0}", got)
	}
	c.HandleKey(key(tui.KeyPageDown))
	c.HandleKey(text("$"))
	if got := c.Cursor(); got != (TablePos{2, 2}) {
		t.Errorf("PgDn $: cursor %+v, want clamped to {2 2}", got)
	}
	if c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'j', Mods: tui.ModCtrl}) {
		t.Error("a Ctrl chord was taken; it is the application's")
	}
}

// Sorting orders the loaded rows as a view: the source keeps its order, numbers sort as numbers,
// and the cursor stays on the row it was on.
func TestTableCore_SortsTheLoadedRows(t *testing.T) {
	src := people()
	c := NewTableCore(src, TableSortable(true))
	c.SetCursor(TablePos{1, 1}) // alice, age 9
	c.HandleKey(text("s"))
	if got := c.Rows(0, 3); !reflect.DeepEqual(got, [][]string{
		{"alice", "9", "Busan"}, {"carol", "41", "Seoul"}, {"bob", "100", "Incheon"},
	}) {
		t.Fatalf("by age: %v; 9 < 41 < 100 as numbers", got)
	}
	if c.Cell(c.Cursor().Row, 0) != "alice" {
		t.Errorf("cursor left alice's row: %+v", c.Cursor())
	}
	c.HandleKey(text("s"))
	if col, desc, _ := c.Sorted(); col != 1 || !desc || c.Cell(0, 0) != "bob" {
		t.Errorf("second s: sorted %d desc=%v, first %q; want age descending, bob first", col, desc, c.Cell(0, 0))
	}
	c.HandleKey(text("s"))
	if col, _, _ := c.Sorted(); col != -1 || c.Cell(0, 0) != "carol" {
		t.Errorf("third s: sorted by %d, first %q; want unsorted, source order", col, c.Cell(0, 0))
	}
	if src.rows[0][0] != "carol" {
		t.Error("sorting changed the source's order")
	}
}

func TestTableCore_SortKeyNeedsSortable(t *testing.T) {
	c := NewTableCore(people())
	if c.HandleKey(text("s")) {
		t.Error("s sorted a table that is not sortable")
	}
}

// A pager is asked for rows when the cursor nears the end of those loaded, once until they arrive;
// a new source with none loaded is asked at once.
func TestTableCore_AsksThePagerAhead(t *testing.T) {
	p := &pagedSource{rowsSource: rowsSource{fields: []TableField{{Title: "n"}, {Title: "r"}}}, total: 25}
	c := NewTableCore(p, TableFetchAhead(10))
	if p.asked != 1 {
		t.Fatalf("a new pager with no rows was asked %d times, want once", p.asked)
	}
	p.arrive(c) // rows 0..9; the cursor at 0 is within 10 of the end: the next page is asked for
	if p.asked != 2 {
		t.Fatalf("after the first page: asked %d times, want 2", p.asked)
	}
	c.Move(1, 0)
	c.Move(1, 0)
	if p.asked != 2 {
		t.Errorf("moves while a page is in flight asked again: %d", p.asked)
	}
	p.arrive(c) // rows 10..19; the cursor at 2 is not within 10 of 20
	if p.asked != 2 {
		t.Errorf("asked with the cursor far from the end: %d", p.asked)
	}
	c.SetCursor(TablePos{Row: 15})
	if p.asked != 3 {
		t.Errorf("the cursor 5 from the end did not ask: %d", p.asked)
	}
	p.arrive(c) // rows 20..24: all of them
	c.SetCursor(TablePos{Row: 24})
	if p.asked != 3 || p.More() {
		t.Errorf("asked %d times after the last row; More=%v", p.asked, p.More())
	}
}

// A sort while rows remain says so, and rows that arrive later join the sort.
func TestTableCore_ASortOfAPartialResultSaysSoAndMerges(t *testing.T) {
	p := &pagedSource{rowsSource: rowsSource{fields: []TableField{{Title: "n"}, {Title: "r"}}}, total: 4}
	c := NewTableCore(p, TableFetchAhead(2), TableSortable(true))
	p.arrive(c) // 0, 1
	c.SortBy(0, true)
	if _, _, partial := c.Sorted(); !partial {
		t.Error("a sort with rows still to load is not marked partial")
	}
	p.arrive(c) // 2, 3
	if got := []string{c.Cell(0, 0), c.Cell(1, 0), c.Cell(2, 0), c.Cell(3, 0)}; !reflect.DeepEqual(got, []string{"3", "2", "1", "0"}) {
		t.Errorf("after the rest arrived: %v, want them merged into the descending sort", got)
	}
	if _, _, partial := c.Sorted(); partial {
		t.Error("a sort of every row is still marked partial")
	}
}

func TestTableCore_CopiesTheSelectionAsTSV(t *testing.T) {
	c := NewTableCore(people())
	if got := c.CopyText(); got != "carol" {
		t.Errorf("no selection: %q, want the cursor's cell", got)
	}
	c.HandleKey(text("v"))
	c.HandleKey(text("j"))
	c.HandleKey(text("l"))
	if got := c.CopyText(); got != "carol\t41\nalice\t9" {
		t.Errorf("a 2x2 range: %q", got)
	}
	if !c.InSelection(1, 1) || c.InSelection(2, 0) {
		t.Error("InSelection disagrees with the range")
	}
	c.HandleKey(key(tui.KeyEscape))
	if _, _, ok := c.Selection(); ok {
		t.Error("Esc kept the selection")
	}
	if c.HandleKey(key(tui.KeyEscape)) {
		t.Error("Esc with no selection was taken; it is the application's then")
	}
}

func TestTableCore_ActivatesTheSourceRow(t *testing.T) {
	got := -1
	c := NewTableCore(people(), TableSortable(true), TableOnActivate(func(row int) { got = row }))
	c.SortBy(1, false) // by age: alice (source row 1) first; the cursor stays on carol
	c.SetCursor(TablePos{})
	c.HandleKey(key(tui.KeyEnter))
	if got != 1 {
		t.Errorf("activated source row %d, want 1 (alice's)", got)
	}
}

func TestTableCore_WidthsInTheLayoutsUnits(t *testing.T) {
	c := NewTableCore(people())
	c.Bind(nil, &fakeTableLayout{})
	if w := c.Width(0); w != 5 {
		t.Errorf("name: %d, want 5 (carol, alice)", w)
	}
	if w := c.Width(2); w != 6 {
		t.Errorf("city: %d, want its declared 6", w)
	}
	long := &rowsSource{fields: []TableField{{Title: "t"}}, rows: [][]string{{string(make([]rune, 100))}}}
	c.SetSource(long)
	if w := c.Width(0); w != tableMeasureMax {
		t.Errorf("a long cell: %d, want the cap %d", w, tableMeasureMax)
	}
}

func TestTableCore_ANilSourceIsEmpty(t *testing.T) {
	c := NewTableCore(nil, TableSortable(true))
	c.HandleKey(text("j"))
	c.HandleKey(text("s"))
	if c.Len() != 0 || c.Cell(0, 0) != "" || c.CopyText() != "" || c.Copy() {
		t.Error("an empty core answered with content")
	}
}
