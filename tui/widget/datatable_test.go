package widget_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// gridSource is an in-memory TableSource for DataTable's cases.
type gridSource struct {
	fields []widget.TableField
	rows   [][]string
}

func (g *gridSource) Fields() []widget.TableField { return g.fields }
func (g *gridSource) Len() int                    { return len(g.rows) }
func (g *gridSource) Cell(r, c int) string        { return g.rows[r][c] }

func numbered(n int) *gridSource {
	g := &gridSource{fields: []widget.TableField{
		{Title: "id", Width: 4}, {Title: "name", Width: 8}, {Title: "n", Width: 5, Align: style.AlignRight},
	}}
	for i := range n {
		g.rows = append(g.rows, []string{strconv.Itoa(i), "row" + strconv.Itoa(i), strconv.Itoa(i * 10)})
	}
	return g
}

func dataTable(t *testing.T, src widget.TableSource, w, h int, opts ...widget.TableOption) (*harness, *shell, *widget.DataTable) {
	t.Helper()
	dt := widget.NewDataTable(widget.NewTableCore(src, opts...))
	sh := newShell(dt)
	hn := startApp(t, sh, w, h)
	hn.barrier(sh)
	return hn, sh, dt
}

func tableCursor(h *harness, dt *widget.DataTable) (pos widget.TablePos) {
	h.onLoop(func() { pos = dt.Core().Cursor() })
	return pos
}

func TestDataTable_HeaderAndAlignedCells(t *testing.T) {
	h, _, _ := dataTable(t, numbered(3), 30, 6)
	head, row := h.row(0), h.row(2)
	if !strings.HasPrefix(head, "id    name      n") {
		t.Errorf("header %q", head)
	}
	if strings.Index(head, "name") != strings.Index(row, "row1") {
		t.Errorf("name misaligned:\n%q\n%q", head, row)
	}
	at := strings.Index(head, " n") + 1 // the n column: 5 wide, right-aligned
	if got := row[at : at+5]; got != "   10" {
		t.Errorf("the n column holds %q, want 10 right-aligned in 5", got)
	}
}

// Moving right past the width scrolls the fields, and down past the height scrolls the rows.
func TestDataTable_ScrollsToTheCursor(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(20), 12, 5)
	h.onLoop(func() {
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "l"})
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "l"})
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "G"})
	})
	h.barrier(sh)
	if got := tableCursor(h, dt); got != (widget.TablePos{Row: 19, Col: 2}) {
		t.Fatalf("cursor %+v", got)
	}
	if !strings.Contains(h.row(0), "n") || strings.Contains(h.row(0), "id") {
		t.Errorf("header %q: want the last field shown, the first scrolled off", h.row(0))
	}
	if !strings.Contains(h.grid(), "190") {
		t.Errorf("the last row is not shown:\n%s", h.grid())
	}
}

// A press puts the cursor on the cell under it; a second press activates the row; a press on the
// header is consumed and moves nothing; motion goes on to the ancestors.
func TestDataTable_PressesAndPassThrough(t *testing.T) {
	activated := -1
	h, sh, dt := dataTable(t, numbered(5), 30, 6, widget.TableOnActivate(func(r int) { activated = r }))
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 7, Y: 3})
	h.settle()
	if got := tableCursor(h, dt); got != (widget.TablePos{Row: 2, Col: 1}) {
		t.Errorf("press at (7,3): cursor %+v, want row 2 field 1", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 2},
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 2})
	h.settle()
	h.onLoop(func() {})
	if activated != 1 {
		t.Errorf("a double press on row 1 activated %d", activated)
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.settle()
	if got := tableCursor(h, dt); got.Row != 1 {
		t.Errorf("a header press moved the cursor to %+v", got)
	}
	before := len(sh.bubbledMice())
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 3, Y: 3})
	h.settle()
	if len(sh.bubbledMice()) == before {
		t.Error("motion over the body was swallowed; an ancestor's drag needs it")
	}
}

// The wheel scrolls the rows and leaves the cursor where it is, and a layout keeps the scroll.
func TestDataTable_WheelScrollsWithoutTheCursor(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(30), 30, 6)
	for range 2 {
		h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 3})
	}
	h.settle()
	h.onLoop(func() { dt.RequestLayout() }) // a layout pass at the same size keeps the scroll
	h.barrier(sh)
	if got := tableCursor(h, dt); got.Row != 0 {
		t.Errorf("the wheel moved the cursor to %+v", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(h.row(1)), "6") {
		t.Errorf("first row shown %q, want row 6 after two wheel steps of 3", h.row(1))
	}
}

func TestDataTable_SortMarkAndEmptyText(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(3), 40, 6, widget.TableSortable(true))
	h.onLoop(func() {
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "s"})
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "s"})
	})
	h.barrier(sh)
	if !strings.Contains(h.row(0), "id ▼") || !strings.HasPrefix(strings.TrimSpace(h.row(1)), "2") {
		t.Errorf("sorted by id descending: header %q, first row %q", h.row(0), h.row(1))
	}
	h.onLoop(func() {
		dt.Core().SetSource(&gridSource{fields: []widget.TableField{{Title: "x"}}})
		dt.SetEmptyText("Nothing here")
	})
	h.barrier(sh)
	h.wantContains("Nothing here")
}
