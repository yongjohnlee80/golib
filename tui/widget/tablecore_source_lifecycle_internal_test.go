package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A double press across a new source does not pair, though the second lands on the row index the
// first did; with the source unchanged it activates the row once.
func TestADataTableDoublePressDoesNotPairAcrossANewSource(t *testing.T) {
	press := func(view *DataTable, count int) {
		view.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 3, Count: count})
	}
	for name, between := range map[string]func(c *TableCore){
		"unchanged source, the control": func(*TableCore) {},
		"a new source":                  func(c *TableCore) { c.SetSource(people()) },
	} {
		t.Run(name, func(t *testing.T) {
			var acts []int
			c := NewTableCore(people(), TableOnActivate(func(r int) { acts = append(acts, r) }))
			view := NewDataTable(c)
			view.Layout(tui.Tight(tui.Size{W: 40, H: 10}))
			press(view, 1)
			between(c)
			press(view, 2)
			want := 1
			if name == "a new source" {
				want = 0
			}
			if len(acts) != want {
				t.Errorf("activations %v, want %d", acts, want)
			}
		})
	}
}

// A source that shrinks, empties and refills, each time followed by Changed, keeps the cursor on a
// row it has: Enter activates that row, and nothing while there are none.
func TestATableShrunkEmptiedAndRefilledActsOnRowsItHas(t *testing.T) {
	src := people()
	var acts []int
	c := NewTableCore(src, TableOnActivate(func(r int) { acts = append(acts, r) }))
	c.Bind(nil, &fakeTableLayout{})
	pressCore := func(codes ...rune) {
		for _, code := range codes {
			c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
		}
	}
	pressCore(tui.KeyDown, tui.KeyDown, tui.KeyEnter)
	if len(acts) != 1 || acts[0] != 2 {
		t.Fatalf("the control: j, j, Enter activated %v, want [2]", acts)
	}
	src.rows = src.rows[:1]
	c.Changed()
	if got := c.Cursor().Row; got != 0 {
		t.Fatalf("shrunk to one row: the cursor is on row %d", got)
	}
	pressCore(tui.KeyEnter)
	src.rows = nil
	c.Changed()
	pressCore(tui.KeyEnter, tui.KeyDown, tui.KeyEnter)
	src.rows = [][]string{{"dan", "7", "Ulsan"}, {"eve", "8", "Daegu"}}
	c.Changed()
	pressCore(tui.KeyEnter, tui.KeyDown, tui.KeyEnter)
	if got := acts[1:]; len(got) != 3 || got[0] != 0 || got[1] != 0 || got[2] != 1 {
		t.Errorf("shrunk, emptied, refilled: activations %v, want [0 0 1]", got)
	}
}

// An owner whose activation replaces the source leaves the core on the new rows: the next keys act
// on them.
func TestATableActivationThatReplacesTheSourceLeavesTheNewRows(t *testing.T) {
	var c *TableCore
	var acts []int
	replaced := false
	fresh := &rowsSource{fields: people().fields, rows: [][]string{{"dan", "7", "Ulsan"}, {"eve", "8", "Daegu"}}}
	c = NewTableCore(people(), TableOnActivate(func(r int) {
		acts = append(acts, r)
		if !replaced {
			replaced = true
			c.SetSource(fresh)
		}
	}))
	c.Bind(nil, &fakeTableLayout{})
	for _, code := range []rune{tui.KeyDown, tui.KeyDown, tui.KeyEnter, tui.KeyDown, tui.KeyEnter} {
		c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
	}
	if len(acts) != 2 || acts[0] != 2 || acts[1] != 1 || c.Len() != 2 {
		t.Errorf("activations %v over %d rows, want [2 1] over the new source's 2", acts, c.Len())
	}
}

// An owner whose sort callback replaces the source leaves the new rows unsorted, in the source's
// order: the sort it heard of belonged to the rows it replaced.
func TestATableSortCallbackThatReplacesTheSourceLeavesTheNewRowsUnsorted(t *testing.T) {
	var c *TableCore
	var heard [][]TableSortKey
	replaced := false
	c = NewTableCore(people(), TableOnSort(func(keys []TableSortKey) {
		heard = append(heard, keys)
		if !replaced {
			replaced = true
			c.SetSource(people())
		}
	}))
	c.Bind(nil, &fakeTableLayout{})
	c.SortKeys([]TableSortKey{{Col: 0}})
	keys, _ := c.SortedKeys()
	if !replaced || keys != nil || c.Cell(0, 0) != "carol" {
		t.Errorf("replaced %v; keys %v, first row %q; want no sort and carol first", replaced, keys, c.Cell(0, 0))
	}
	if len(heard) == 0 || len(heard[0]) != 1 {
		t.Errorf("the owner heard %v, want the sort it asked for first", heard)
	}
}
