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
	head, row := h.row(0), h.row(3)
	if !strings.HasPrefix(head, "id    name      n") {
		t.Errorf("header %q", head)
	}
	if rule := h.row(1); rule != strings.Repeat("─", 30) {
		t.Errorf("the rule under the titles %q, want ─ across the view", rule)
	}
	if a := cellAttrs(h, 0, 0); a.Mask&tui.AttrBold == 0 || a.Mask&tui.AttrUnderline != 0 {
		t.Errorf("the id title drawn %+v, want bold and ruled off, not underlined", a)
	}
	if strings.Index(head, "name") != strings.Index(row, "row1") {
		t.Errorf("name misaligned:\n%q\n%q", head, row)
	}
	at := strings.Index(head, " n ") + 1 // the n column: 5 wide, right-aligned
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

// A filter that shrinks the rows under a scrolled view shows as many of them as fit: the cursor's
// row is not left alone at the top with the rows before it scrolled off.
func TestDataTable_AFilterScrollsBackToFitTheRows(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(40), 30, 6)
	h.onLoop(func() { dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "G"}) })
	h.barrier(sh)
	h.onLoop(func() {
		p, err := widget.ParseTableWhere("id >= 37", dt.Core().Fields())
		if err != nil {
			t.Error(err)
		}
		dt.Core().SetFilter(p)
	})
	h.barrier(sh)
	if got := tableCursor(h, dt); got.Row != 2 {
		t.Fatalf("cursor %+v, want row 2, still on row39", got)
	}
	for _, s := range []string{"row37", "row38", "row39"} {
		if !strings.Contains(h.grid(), s) {
			t.Errorf("%s is not shown:\n%s", s, h.grid())
		}
	}
}

// A double press activates once, on the row pressed twice: a third press does not activate again,
// and a press on the same screen cell after the wheel moved another row under it does not pair
// (tui-widget-data-D4), as in List and Tree.
func TestDataTable_ADoublePressPairsOnTheSameRowOnce(t *testing.T) {
	var acts []int
	h, _, _ := dataTable(t, numbered(20), 30, 6, widget.TableOnActivate(func(r int) { acts = append(acts, r) }))
	press := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 2}
	h.inject(press, press, press)
	h.settle()
	h.onLoop(func() {})
	if len(acts) != 1 || acts[0] != 1 {
		t.Errorf("a triple press on row 1 activated %v, want [1]", acts)
	}

	acts = nil
	// a press elsewhere starts the App's count afresh, so the second press on the cell counts 2
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 4})
	h.settle()
	h.inject(press, tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 2}, press)
	h.settle()
	h.onLoop(func() {})
	if len(acts) != 0 {
		t.Errorf("presses on one cell with the wheel between them, on two rows, activated %v", acts)
	}
}

// A press puts the cursor on the cell under it; a second press activates the row; a press on the
// header is consumed and selects that field, the cursor on its first row; motion goes on to the
// ancestors.
func TestDataTable_PressesAndPassThrough(t *testing.T) {
	activated := -1
	h, sh, dt := dataTable(t, numbered(5), 30, 6, widget.TableOnActivate(func(r int) { activated = r }))
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 7, Y: 4})
	h.settle()
	if got := tableCursor(h, dt); got != (widget.TablePos{Row: 2, Col: 1}) {
		t.Errorf("press at (7,4): cursor %+v, want row 2 field 1", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 3},
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 3})
	h.settle()
	h.onLoop(func() {})
	if activated != 1 {
		t.Errorf("a double press on row 1 activated %d", activated)
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.settle()
	if got := tableCursor(h, dt); got != (widget.TablePos{Row: 0, Col: 0}) {
		t.Errorf("a header press on the id field put the cursor on %+v, want its first row", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})
	h.settle()
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
	if !strings.HasPrefix(strings.TrimSpace(h.row(2)), "6") {
		t.Errorf("first row shown %q, want row 6 after two wheel steps of 3", h.row(2))
	}
}

// A selection is filled with the secondary fill by default, so a region shows in colour, and the
// cursor's cell keeps its own look inside it. A press on the rule under the titles is no hit.
func TestDataTable_ASelectionIsFilledAndTheRuleTakesNoPress(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(3), 30, 6)
	plain := cellAttrs(h, 6, 3) // row 1's name, before any selection
	h.onLoop(func() {
		dt.HandleEvent(key('v'))
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('l'))
	})
	h.barrier(sh)
	sel, cursor := cellAttrs(h, 0, 2), cellAttrs(h, 6, 3) // row 0's id; the cursor on row 1's name
	if sel.BG == plain.BG || sel.BG.Kind == tui.CellColorDefault {
		t.Errorf("a selected cell drawn %+v, want a fill unlike a plain cell's %+v", sel, plain)
	}
	if cursor == sel || cursor.Mask&tui.AttrReverse == 0 {
		t.Errorf("the cursor's cell inside the selection drawn %+v, want the cursor's reverse, not the fill %+v", cursor, sel)
	}
	h.onLoop(func() { dt.HandleEvent(key(tui.KeyEscape)) })
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 1})
	h.settle()
	if got := tableCursor(h, dt); got != (widget.TablePos{Row: 1, Col: 1}) {
		t.Errorf("a press on the rule moved the cursor to %+v", got)
	}
	if _, _, ok := tableSelected(h, dt); ok {
		t.Error("a press on the rule selected")
	}
}

func TestDataTable_SortMarkAndEmptyText(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(3), 40, 6, widget.TableSortable(true))
	h.onLoop(func() {
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "s"})
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "s"})
	})
	h.barrier(sh)
	if !strings.Contains(h.row(0), "id ▼") || !strings.HasPrefix(strings.TrimSpace(h.row(2)), "2") {
		t.Errorf("sorted by id descending: header %q, first row %q", h.row(0), h.row(2))
	}
	h.onLoop(func() {
		dt.Core().SetSource(&gridSource{fields: []widget.TableField{{Title: "x"}}})
		dt.SetEmptyText("Nothing here")
	})
	h.barrier(sh)
	h.wantContains("Nothing here")
}

// boundTable is a layout a native backend binds: it answers in its own units (ten per rune), and
// records what the core asks of it. Read and written on the loop.
type boundTable struct {
	revealed []widget.TablePos
	changed  int
}

func (b *boundTable) Reveal(row, col int) {
	b.revealed = append(b.revealed, widget.TablePos{Row: row, Col: col})
}
func (b *boundTable) PageRows() int        { return 7 }
func (b *boundTable) Measure(s string) int { return 10 * len([]rune(s)) }
func (b *boundTable) Changed()             { b.changed++ }

// placingTable is a bound layout that places the pointer: rows 20 high under a header as high,
// fields 100 wide.
type placingTable struct {
	boundTable
	at       [][2]float32
	scrolled int
}

func (p *placingTable) CellAt(x, y float32) (widget.TablePos, bool, bool) {
	p.at = append(p.at, [2]float32{x, y})
	if y < 20 {
		return widget.TablePos{}, true, true
	}
	return widget.TablePos{Row: int(y/20) - 1, Col: int(x / 100)}, false, x < 200
}

func (p *placingTable) ScrollRows(n int) { p.scrolled += n }

// pixelBackend is a test backend whose cells are 10 by 20 pixels, as a native window's are.
type pixelBackend struct{ *tui.TestBackend }

func (pixelBackend) CellPixels() tui.CellPixels { return tui.CellPixels{W: 10, H: 20} }

// A bound layout replaces the cell layout: the cells under the table are left blank for the
// backend to draw over, the core reveals and pages through the bound layout, and the widths are
// measured in its units. A layout that does not place the pointer lets it go on.
func TestDataTable_ABoundLayoutReplacesTheCells(t *testing.T) {
	dt := widget.NewDataTable(widget.NewTableCore(numbered(20)))
	if dt.BoundLayout() != widget.TableLayout(dt) {
		t.Fatalf("an unbound table draws with %T, want its own cell layout", dt.BoundLayout())
	}
	b := &boundTable{}
	dt.BindLayout(b)
	dt.BindLayout(nil) // nil keeps the layout bound
	sh := newShell(dt)
	h := startApp(t, sh, 30, 6)
	h.barrier(sh)
	if got := strings.TrimSpace(h.grid()); got != "" {
		t.Errorf("the cells under a bound layout hold text:\n%s", h.grid())
	}
	h.onLoop(func() {
		if dt.BoundLayout() != widget.TableLayout(b) {
			t.Errorf("the bound layout is %T", dt.BoundLayout())
		}
		if dt.Theme() == nil {
			t.Error("no theme after the first paint")
		}
		if w := dt.Core().Width(0); w != 40 {
			t.Errorf("a field 4 cells wide is %d in the bound layout's units, want 40", w)
		}
		if w := dt.Core().Width(1); w != 80 {
			t.Errorf("a field 8 cells wide is %d, want 80", w)
		}
		b.revealed, b.changed = nil, 0
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageDown})
		if got := dt.Core().Cursor(); got.Row != 7 {
			t.Errorf("PgDn moved to row %d, want the bound layout's page of 7", got.Row)
		}
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Text: "G"})
		if n := len(b.revealed); n != 2 || b.revealed[n-1] != (widget.TablePos{Row: 19}) {
			t.Errorf("the bound layout was asked to reveal %v, want rows 7 then 19", b.revealed)
		}
		if b.changed == 0 {
			t.Error("the bound layout was not asked to repaint")
		}
		if dt.Styles().Cursor == (style.Style{}) || dt.EmptyText() != "No rows" {
			t.Errorf("styles %+v, empty text %q: want the defaults", dt.Styles(), dt.EmptyText())
		}
	})
	before := len(sh.bubbledMice())
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 3})
	h.settle()
	if got := tableCursor(h, dt); got.Row != 19 {
		t.Errorf("a press under a layout that does not place the pointer moved the cursor to %+v", got)
	}
	if len(sh.bubbledMice()) == before {
		t.Error("a press the bound layout cannot place was swallowed")
	}
}

// A bound layout that places the pointer is given it in pixels: a press puts the cursor on its
// cell, a second activates, past the fields does nothing, and the wheel scrolls it.
func TestDataTable_ABoundLayoutPlacesThePointer(t *testing.T) {
	activated := -1
	dt := widget.NewDataTable(widget.NewTableCore(numbered(20), widget.TableOnActivate(func(r int) { activated = r })))
	p := &placingTable{}
	dt.BindLayout(p)
	sh := newShell(dt)
	tb := tui.NewTestBackend(30, 6)
	h := startAppOpts(t, sh, 30, 6, tui.WithBackend(pixelBackend{tb}))
	h.tb = tb // the harness scripts and reads the backend the app runs on
	h.barrier(sh)
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 12, Y: 2, SubX: 0.5, SubY: 0.25})
	h.settle()
	h.onLoop(func() {
		if len(p.at) != 1 || p.at[0] != [2]float32{125, 45} {
			t.Errorf("the layout placed %v, want the press at (125, 45) pixels", p.at)
		}
		if got := dt.Core().Cursor(); got != (widget.TablePos{Row: 1, Col: 1}) {
			t.Errorf("the press put the cursor on %+v, want row 1 field 1", got)
		}
	})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 4},
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 4})
	h.settle()
	h.onLoop(func() {})
	if activated != 3 {
		t.Errorf("a double press on row 3 activated %d", activated)
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 29, Y: 2})
	h.settle()
	if got := tableCursor(h, dt); got.Row != 3 {
		t.Errorf("a press past the fields moved the cursor to %+v", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 3},
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 3},
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelUp, X: 2, Y: 3})
	h.settle()
	h.onLoop(func() {
		if p.scrolled != 3 {
			t.Errorf("the wheel scrolled the layout %d rows, want 3 (down 3, down 3, up 3)", p.scrolled)
		}
	})
	if got := tableCursor(h, dt); got.Row != 3 {
		t.Errorf("the wheel moved the cursor to %+v", got)
	}
}
