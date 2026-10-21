package widget_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func motion(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: x, Y: y}
}

func mouseUp(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y}
}

// tableSelected is the table's selection as its kind and its copy, read on the loop.
func tableSelected(h *harness, dt *widget.DataTable) (kind widget.TableSelectKind, text string, ok bool) {
	h.onLoop(func() {
		c := dt.Core()
		_, _, ok = c.Selection()
		kind, text = c.SelectionKind(), c.CopyText()
	})
	return kind, text, ok
}

// cellIndex is the cell sub starts at in line, a cell a rune (the rules are one cell, three bytes);
// -1 when it is not there.
func cellIndex(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return len([]rune(line[:i]))
}

// gesture presses at the first point, drags through the rest, and releases at the last.
func gesture(h *harness, pts ...[2]int) {
	h.t.Helper()
	evs := []tui.Event{click(pts[0][0], pts[0][1])}
	for _, p := range pts[1:] {
		evs = append(evs, motion(p[0], p[1]))
	}
	last := pts[len(pts)-1]
	evs = append(evs, mouseUp(last[0], last[1]))
	h.inject(evs...)
	h.settle()
}

// The cell layout with row numbers: the numbers are drawn in the header's style before the first
// field, ruled off from it, and the titles ruled off from the rows, the rules crossing. A drag over
// cells selects them in reading order, over the gutter (its rule too) whole rows, over the titles
// the fields under them, every row.
func TestDataTable_CellLayoutDragsOverCellsGutterAndHeaders(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(5), 30, 8, widget.TableRowNumbers(true))
	if at := cellIndex(h.row(0), "id"); at != 4 {
		t.Fatalf("the id title at %d, want 4: after one digit, its rule and the gap\n%s", at, h.grid())
	}
	if !strings.HasPrefix(h.row(0), "  │ id") || !strings.HasPrefix(h.row(1), "──┼─────") {
		t.Errorf("the rules, under the titles and after the numbers, crossing:\n%s", h.grid())
	}
	if !strings.HasPrefix(h.row(2), "1 │ 0") || !strings.HasPrefix(h.row(6), "5 │ 4") {
		t.Errorf("row numbers:\n%s", h.grid())
	}
	if a := cellAttrs(h, 0, 2); a.Mask&tui.AttrBold == 0 || a.Mask&tui.AttrUnderline != 0 {
		t.Errorf("the number 1 drawn %+v, want the header's bold, not underlined", a)
	}

	gesture(h, [2]int{10, 3}, [2]int{10, 3}, [2]int{4, 5}) // row 1 name → row 3 id
	if kind, text, _ := tableSelected(h, dt); kind != widget.TableSelectCells || text != "row1\t10\n2\trow2\t20\n3" {
		t.Errorf("a drag over cells: kind %v, %q", kind, text)
	}

	gesture(h, [2]int{2, 3}, [2]int{1, 4}) // the numbers of rows 1 and 2, from the rule
	if kind, text, _ := tableSelected(h, dt); kind != widget.TableSelectRows || text != "1\trow1\t10\n2\trow2\t20" {
		t.Errorf("a drag over the gutter: kind %v, %q", kind, text)
	}

	gesture(h, [2]int{10, 0}, [2]int{20, 0}) // the name title → the n title
	var from, to widget.TablePos
	h.onLoop(func() { from, to, _ = dt.Core().Selection() })
	if kind, _, _ := tableSelected(h, dt); kind != widget.TableSelectBlock || from != (widget.TablePos{Row: 0, Col: 1}) || to != (widget.TablePos{Row: 4, Col: 2}) {
		t.Errorf("a drag over the titles: kind %v, %+v to %+v; want a block of fields 1-2, every row", kind, from, to)
	}

	// a press is a command boundary: it ends the selection, and the release lets the pointer go
	gesture(h, [2]int{4, 2})
	if _, _, ok := tableSelected(h, dt); ok {
		t.Error("a press kept the selection")
	}
	before := len(sh.bubbledMice())
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 3, Y: 3})
	h.settle()
	if len(sh.bubbledMice()) == before {
		t.Error("motion after the release was swallowed")
	}
}

// The rules are drawn in the Header's border: its Left after the numbers, its Top under the titles,
// crossing in the border's weight. A hidden border hides both, and their crossing.
func TestDataTable_TheRulesAreTheHeadersBorder(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(3), 20, 6, widget.TableRowNumbers(true))
	for _, c := range []struct {
		name       string
		border     style.BorderStyle
		rule, cell string
	}{
		{"double", style.BorderDouble, "══╬═", "1 ║ 0"},
		{"thick", style.BorderThick, "━━╋━", "1 ┃ 0"},
		{"hidden", style.BorderHidden, "    ", "1   0"},
	} {
		h.onLoop(func() { dt.SetStyles(widget.DataTableStyles{Header: style.New().Bold(true).Border(c.border)}) })
		h.barrier(sh)
		if !strings.HasPrefix(h.row(1), c.rule) || !strings.HasPrefix(h.row(2), c.cell) {
			t.Errorf("a %s border's rules, want %q over %q:\n%s", c.name, c.rule, c.cell, h.grid())
		}
	}
}

// The gutter is as wide as the digits of the rows loaded: one digit for 9 rows, two once a tenth
// arrives, and the fields and the pointer move with it.
func TestDataTable_TheGutterGrowsFromNineRowsToTen(t *testing.T) {
	src := numbered(9)
	h, sh, dt := dataTable(t, src, 30, 14, widget.TableRowNumbers(true))
	if at := cellIndex(h.row(0), "id"); at != 4 || !strings.HasPrefix(h.row(10), "9 │ 8") {
		t.Fatalf("9 rows: id at %d\n%s", at, h.grid())
	}
	h.onLoop(func() {
		if d := dt.Core().RowNumberDigits(); d != 1 {
			t.Errorf("9 rows: %d digits", d)
		}
		src.rows = append(src.rows, []string{"9", "row9", "90"})
		dt.Core().Changed()
	})
	h.barrier(sh)
	if at := cellIndex(h.row(0), "id"); at != 5 || !strings.HasPrefix(h.row(2), " 1 │ 0") || !strings.HasPrefix(h.row(11), "10 │ 9") {
		t.Errorf("10 rows: id at %d\n%s", at, h.grid())
	}
	h.onLoop(func() {
		if d := dt.Core().RowNumberDigits(); d != 2 {
			t.Errorf("10 rows: %d digits", d)
		}
	})
	gesture(h, [2]int{4, 3}) // a cell of the id field with one digit; the gutter's gap with two
	if kind, text, _ := tableSelected(h, dt); kind != widget.TableSelectRows || text != "1\trow1\t10" {
		t.Errorf("a press at x=4 with two digits: kind %v, %q; want row 1, from the gutter", kind, text)
	}
	h.onLoop(func() { dt.Core().SetRowNumbers(false) })
	h.barrier(sh)
	if at := strings.Index(h.row(0), "id"); at != 0 {
		t.Errorf("numbers off: id at %d\n%s", at, h.grid())
	}
}

// hittingTable is a pixel layout that tells the gutter apart: 50 pixels of row numbers, then
// fields 100 wide, rows 20 high under a header as high.
type hittingTable struct{ placingTable }

func (p *hittingTable) HitAt(x, y float32) (widget.TablePos, widget.TableHit) {
	switch {
	case y < 20 && x >= 50:
		return widget.TablePos{Col: int((x - 50) / 100)}, widget.TableHitHeader
	case y < 20:
		return widget.TablePos{}, widget.TableHitNone
	case x < 50:
		return widget.TablePos{Row: int(y/20) - 1}, widget.TableHitGutter
	case x < 250:
		return widget.TablePos{Row: int(y/20) - 1, Col: int((x - 50) / 100)}, widget.TableHitCell
	}
	return widget.TablePos{}, widget.TableHitNone
}

func boundTableApp(t *testing.T, l widget.TableLayout) (*harness, *widget.DataTable) {
	t.Helper()
	dt := widget.NewDataTable(widget.NewTableCore(numbered(20)))
	dt.BindLayout(l)
	sh := newShell(dt)
	tb := tui.NewTestBackend(30, 6)
	h := startAppOpts(t, sh, 30, 6, tui.WithBackend(pixelBackend{tb}))
	h.tb = tb
	h.barrier(sh)
	return h, dt
}

// A bound layout with HitAt places every pointer event itself, CellAt never asked: the gutter, the
// cells and the titles each select their way.
func TestDataTable_ATableLayoutHitterPlacesTheGutter(t *testing.T) {
	p := &hittingTable{}
	h, dt := boundTableApp(t, p)
	gesture(h, [2]int{2, 2}, [2]int{2, 4}) // the numbers of rows 1 to 3, at 20 pixels
	if kind, text, _ := tableSelected(h, dt); kind != widget.TableSelectRows || text != "1\trow1\t10\n2\trow2\t20\n3\trow3\t30" {
		t.Errorf("a gutter drag: kind %v, %q", kind, text)
	}
	gesture(h, [2]int{7, 2}, [2]int{17, 3}) // row 1 id → row 2 name
	if kind, text, _ := tableSelected(h, dt); kind != widget.TableSelectCells || text != "1\trow1\t10\n2\trow2" {
		t.Errorf("a cell drag: kind %v, %q", kind, text)
	}
	gesture(h, [2]int{7, 0}, [2]int{17, 0}) // the id title → the name title
	var from, to widget.TablePos
	h.onLoop(func() { from, to, _ = dt.Core().Selection() })
	if kind, _, _ := tableSelected(h, dt); kind != widget.TableSelectBlock || from != (widget.TablePos{}) || to != (widget.TablePos{Row: 19, Col: 1}) {
		t.Errorf("a header drag: kind %v, %+v to %+v", kind, from, to)
	}
	h.onLoop(func() {
		if len(p.at) != 0 {
			t.Errorf("CellAt was asked %d times under a layout with HitAt", len(p.at))
		}
	})
}

// A plain TableLayoutPointer is placed by CellAt: its cells select; its header, whose field CellAt
// does not name, selects nothing and leaves a selection as it is; and nothing is a gutter, so no
// press or drag selects rows.
func TestDataTable_APlainTableLayoutPointerHasNoGutter(t *testing.T) {
	p := &placingTable{}
	h, dt := boundTableApp(t, p)
	gesture(h, [2]int{1, 0})
	var from, to widget.TablePos
	h.onLoop(func() { from, to, _ = dt.Core().Selection() })
	if _, _, ok := tableSelected(h, dt); ok {
		t.Errorf("a header press without HitAt selected %+v to %+v; CellAt names no field, so it must select nothing", from, to)
	}
	gesture(h, [2]int{1, 2}, [2]int{12, 3}) // cells selected first, then the header pressed
	_, before, selected := tableSelected(h, dt)
	if !selected {
		t.Fatal("the drag over cells selected nothing: the header press below would prove nothing")
	}
	gesture(h, [2]int{1, 0})
	if _, after, _ := tableSelected(h, dt); after != before {
		t.Errorf("a header press without HitAt changed the selection: %q, then %q", before, after)
	}
	gesture(h, [2]int{1, 2}) // where a gutter would be: a cell here
	if _, _, ok := tableSelected(h, dt); ok {
		t.Error("a press at the left edge selected: read as a gutter")
	}
	gesture(h, [2]int{1, 2}, [2]int{12, 3}) // row 1 id → row 2 name
	if kind, text, _ := tableSelected(h, dt); kind != widget.TableSelectCells || text != "1\trow1\t10\n2\trow2" {
		t.Errorf("a drag from the left edge: kind %v, %q; want cells", kind, text)
	}
	h.onLoop(func() {
		if len(p.at) == 0 {
			t.Error("CellAt was never asked")
		}
	})
}

// The cursor is Cursor while the table has the keys and CursorBlurred while it has not, faint
// Cursor by default; the focus coming back brings Cursor back.
func TestDataTable_TheCursorBlursWithoutTheKeys(t *testing.T) {
	dt := widget.NewDataTable(widget.NewTableCore(numbered(3)))
	other := widget.NewTextInput()
	root := tui.NewFlex(tui.Vertical)
	root.AddSized(dt, 5)
	root.Add(other)
	h := startApp(t, root, 30, 8)
	cursor := func() tui.CellAttrs { return cellAttrs(h, 0, 2) } // row 0's id
	focus := func(c interface{ Context() *tui.Context }) {
		h.onLoop(func() { c.Context().RequestFocus() })
		h.settle()
	}
	focus(dt)
	if a := cursor(); a.Mask&tui.AttrReverse == 0 || a.Mask&tui.AttrFaint != 0 {
		t.Fatalf("with the keys: %+v, want reversed, not faint", a)
	}
	focus(other)
	if a := cursor(); a.Mask&tui.AttrReverse == 0 || a.Mask&tui.AttrFaint == 0 {
		t.Errorf("without the keys: %+v, want reversed and faint", a)
	}
	focus(dt)
	if a := cursor(); a.Mask&tui.AttrFaint != 0 {
		t.Errorf("with the keys again: %+v, want not faint", a)
	}
	h.onLoop(func() {
		dt.SetStyles(widget.DataTableStyles{Cursor: style.New().Bold(true), CursorBlurred: style.New().Underline(true)})
	})
	focus(other)
	if a := cursor(); a.Mask&tui.AttrUnderline == 0 || a.Mask&tui.AttrBold != 0 {
		t.Errorf("a declared CursorBlurred: %+v, want underlined, not bold", a)
	}
	focus(dt)
	if a := cursor(); a.Mask&tui.AttrBold == 0 || a.Mask&tui.AttrUnderline != 0 {
		t.Errorf("a declared Cursor: %+v, want bold, not underlined", a)
	}
}

// A right press inside the selection opens the menu at the pointer and leaves the selection as it
// was; MenuAt is where it was pressed, and a row chosen runs, on a menu with no editor core.
func TestDataTable_ARightPressOpensTheMenuOverTheSelection(t *testing.T) {
	dt := widget.NewDataTable(widget.NewTableCore(numbered(5)))
	sh := newShell(widget.NewOverlayHost(dt))
	h := startApp(t, sh, 40, 12)
	h.barrier(sh)
	ran := make(chan widget.TablePos, 1)
	var at widget.TablePos
	var hit widget.TableHit
	menu := widget.NewEditorMenu(dt, nil)
	menu.SetRows(func() []widget.MenuItemModel {
		at, hit = dt.MenuAt()
		return []widget.MenuItemModel{widget.NewCommand("table.copy", "Copy rows", widget.CoreMenuAction{
			ID: "table.copy", Run: func(*widget.EditorCore) { ran <- dt.Core().Cursor() }})}
	})
	h.onLoop(func() {
		dt.SetMenu(menu)
		menu.SetEnabled(true)
		dt.Context().RequestFocus()
		dt.HandleEvent(key('V'))
		dt.HandleEvent(key('j'))
	})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 7, Y: 3})
	h.waitFor("the menu opened", func() bool {
		var open bool
		h.onLoop(func() { open = menu.IsOpen() })
		return open
	})
	h.settle()
	h.wantContains("Copy rows")
	if kind, text, ok := tableSelected(h, dt); !ok || kind != widget.TableSelectRows || text != "0\trow0\t0\n1\trow1\t10" {
		t.Errorf("after the right press: kind %v (%v), %q; want rows 0-1 still selected", kind, ok, text)
	}
	h.onLoop(func() {
		if at != (widget.TablePos{Row: 1, Col: 1}) || hit != widget.TableHitCell {
			t.Errorf("MenuAt %+v %v, want row 1 field 1, a cell", at, hit)
		}
	})
	h.inject(key(tui.KeyEnter))
	h.waitFor("the row ran", func() bool { return len(ran) == 1 })
	if got := <-ran; got.Row != 1 {
		t.Errorf("the row ran with the cursor on %+v; the right press moved it", got)
	}
	if _, _, ok := tableSelected(h, dt); !ok {
		t.Error("choosing a row ended the selection")
	}
}

// A right press outside the selection on a cell moves the cursor there and ends the selection
// before the menu opens: the menu acts on what was pressed. No drag starts.
func TestDataTable_ARightPressOutsideTheSelectionTakesTheCell(t *testing.T) {
	dt := widget.NewDataTable(widget.NewTableCore(numbered(5), widget.TableMenuSelects(true)))
	sh := newShell(widget.NewOverlayHost(dt))
	h := startApp(t, sh, 40, 12)
	h.barrier(sh)
	menu := widget.NewEditorMenu(dt, nil)
	menu.SetRows(func() []widget.MenuItemModel {
		return []widget.MenuItemModel{widget.NewCommand("table.copy", "Copy rows", widget.CoreMenuAction{ID: "table.copy"})}
	})
	h.onLoop(func() {
		dt.SetMenu(menu)
		menu.SetEnabled(true)
		dt.Context().RequestFocus()
		dt.HandleEvent(key('V'))
		dt.HandleEvent(key('j'))
	})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 7, Y: 5})
	h.waitFor("the menu opened", func() bool {
		var open bool
		h.onLoop(func() { open = menu.IsOpen() })
		return open
	})
	h.onLoop(func() {
		c := dt.Core()
		if _, _, ok := c.Selection(); ok || c.Cursor() != (widget.TablePos{Row: 3, Col: 1}) || c.Dragging() {
			t.Errorf("after a right press on row 3: selection %v, cursor %+v, dragging %v; want none, row 3 field 1, no drag",
				ok, c.Cursor(), c.Dragging())
		}
	})
}

// A menu with no core and no rows has nothing to show: the right press goes on, and nothing breaks.
func TestDataTable_AMenuWithNoRowsLetsTheRightPressGoOn(t *testing.T) {
	dt := widget.NewDataTable(widget.NewTableCore(numbered(3)))
	sh := newShell(widget.NewOverlayHost(dt))
	h := startApp(t, sh, 30, 8)
	h.barrier(sh)
	menu := widget.NewEditorMenu(dt, nil)
	h.onLoop(func() {
		dt.SetMenu(menu)
		menu.SetEnabled(true)
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("opening a menu with no core and no rows panicked: %v", r)
			}
		}()
		if menu.OpenAt(tui.Point{X: 2, Y: 2}) {
			t.Error("a menu with no core and no rows opened")
		}
	})
	if t.Failed() {
		return // the press would fail the same way, on the loop, where it takes the process down
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 2, Y: 2})
	h.settle()
	bubbled := false
	for _, m := range sh.bubbledMice() {
		bubbled = bubbled || (m.Kind == tui.MousePress && m.Button == tui.MouseRight)
	}
	var open bool
	h.onLoop(func() { open = menu.IsOpen() })
	if !bubbled || open {
		t.Errorf("bubbled %v, open %v: want the press passed on and no menu", bubbled, open)
	}
}

// y copies the cursor's cell, and a second y the cursor's row.
func TestDataTable_YCopiesTheCellAndYYTheRow(t *testing.T) {
	h, _, dt := dataTable(t, numbered(3), 30, 6)
	h.onLoop(func() {
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('l'))
		dt.HandleEvent(key('y'))
	})
	h.waitFor("the cell on the clipboard", func() bool { return string(h.tb.Clipboard()) == "row1" })
	h.onLoop(func() { dt.HandleEvent(key('y')) })
	h.waitFor("the row on the clipboard", func() bool { return string(h.tb.Clipboard()) == "1\trow1\t10" })
	h.onLoop(func() {
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('y'))
	})
	h.waitFor("a y after a move copies the cell again", func() bool { return string(h.tb.Clipboard()) == "row2" })
}

// A copy longer than TableCopyLimit copies nothing, by y, yy or Copy, and TableOnCopyRefused hears
// the limit; within it, and with the limit lifted, the copies go on.
func TestDataTable_ACopyPastTheLimitCopiesNothing(t *testing.T) {
	var refused []int
	h, sh, dt := dataTable(t, numbered(3), 30, 6,
		widget.TableCopyLimit(8), widget.TableOnCopyRefused(func(limit int) { refused = append(refused, limit) }))
	h.onLoop(func() {
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('l'))
		dt.HandleEvent(key('y')) // row1: 4 bytes
	})
	h.waitFor("the cell on the clipboard", func() bool { return string(h.tb.Clipboard()) == "row1" })
	var copied bool
	h.onLoop(func() {
		dt.HandleEvent(key('y')) // yy: "1\trow1\t10", 9 bytes
		dt.HandleEvent(key('V'))
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('y')) // two rows
		copied = dt.Core().Copy()
	})
	h.barrier(sh)
	var got []int
	h.onLoop(func() { got = append(got, refused...) })
	if !slices.Equal(got, []int{8, 8, 8}) || copied {
		t.Errorf("refused %v, Copy %v; want yy, y and Copy each refused at 8", got, copied)
	}
	if s := string(h.tb.Clipboard()); s != "row1" {
		t.Errorf("the clipboard holds %q after refused copies, want row1 still", s)
	}
	h.onLoop(func() {
		dt.Core().SetCopyLimit(0, nil)
		dt.HandleEvent(key('y'))
	})
	h.waitFor("the rows on the clipboard", func() bool { return string(h.tb.Clipboard()) == "1\trow1\t10\n2\trow2\t20" })
}

// r puts the cursor on its whole row: every field of the row is drawn in the cursor's style, y
// copies the row (as yy would), and r again brings the cell cursor back, where y copies the cell.
func TestDataTable_RTogglesARowCursorThatYCopiesWhole(t *testing.T) {
	h, _, dt := dataTable(t, numbered(3), 30, 6)
	// at is where text is drawn on the screen: the first line holding it, and its column there
	at := func(text string) (x, y int) {
		for y, line := range strings.Split(h.grid(), "\n") {
			if i := strings.Index(line, text); i >= 0 {
				return len([]rune(line[:i])), y
			}
		}
		t.Fatalf("%q is not on the screen:\n%s", text, h.grid())
		return 0, 0
	}
	h.onLoop(func() {
		dt.SetStyles(widget.DataTableStyles{Cursor: style.New().Bold(true), CursorBlurred: style.New().Underline(true)})
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('l'))
	})
	h.settle()
	nx, y := at("row1") // row 1's name: the cursor's cell
	line := []rune(strings.Split(h.grid(), "\n")[y])
	ix := slices.Index(line, '1') // row 1's id, the field before it: the line's first 1
	cell := cellAttrs(h, nx, y)
	if cell == cellAttrs(h, ix, y) {
		t.Fatalf("precondition: the cursor's cell %+v is drawn as row 1's id", cell)
	}
	h.onLoop(func() { dt.HandleEvent(key('r')) })
	h.settle()
	if got := cellAttrs(h, ix, y); got != cell {
		t.Errorf("the row cursor leaves row 1's id drawn %+v, want the cursor's %+v", got, cell)
	}
	if _, y2 := at("row2"); cellAttrs(h, ix, y2) == cell {
		t.Error("the row cursor covers row 2 too")
	}
	h.onLoop(func() { dt.HandleEvent(key('y')) })
	h.waitFor("the row on the clipboard", func() bool { return string(h.tb.Clipboard()) == "1\trow1\t10" })
	h.onLoop(func() {
		dt.HandleEvent(key('r'))
		dt.HandleEvent(key('y'))
	})
	h.settle()
	if got := cellAttrs(h, ix, y); got == cell {
		t.Error("back on the cell, row 1's id is still drawn as the cursor")
	}
	h.waitFor("the cell on the clipboard", func() bool { return string(h.tb.Clipboard()) == "row1" })
}

// A table that copies CSV writes its cells comma-separated and quoted as RFC 4180 has it: a value
// with a comma or a quote is quoted, its quotes doubled; whole rows are CSV lines.
func TestDataTable_ACSVTableCopiesCSV(t *testing.T) {
	src := numbered(2)
	src.rows[1][1] = `say "hi", then`
	h, _, dt := dataTable(t, src, 40, 6, widget.TableCopyAs(widget.TableCopyCSV))
	h.onLoop(func() {
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('l'))
		dt.HandleEvent(key('y'))
	})
	h.waitFor("the quoted cell", func() bool { return string(h.tb.Clipboard()) == `"say ""hi"", then"` })
	h.onLoop(func() { dt.HandleEvent(key('y')) })
	h.waitFor("the row as CSV", func() bool { return string(h.tb.Clipboard()) == `1,"say ""hi"", then",10` })
	h.onLoop(func() {
		dt.HandleEvent(key('g'))
		dt.HandleEvent(key('g'))
		dt.HandleEvent(key('V'))
		dt.HandleEvent(key('j'))
		dt.HandleEvent(key('y'))
	})
	h.waitFor("two rows as CSV", func() bool {
		return string(h.tb.Clipboard()) == "0,row0,0\n"+`1,"say ""hi"", then",10`
	})
}

// A right press whose cursor callback (TableOnCursor) replaces the source opens no menu: the row it
// was for is gone. The control is TestDataTable_ARightPressOutsideTheSelectionTakesTheCell.
func TestDataTable_ARightPressOpensNoMenuForRowsItsCallbackReplaced(t *testing.T) {
	var core *widget.TableCore
	replaced := false
	dt := widget.NewDataTable(widget.NewTableCore(numbered(5), widget.TableMenuSelects(true),
		widget.TableOnCursor(func(int) {
			if !replaced && core != nil {
				replaced = true
				core.SetSource(numbered(5))
			}
		})))
	sh := newShell(widget.NewOverlayHost(dt))
	h := startApp(t, sh, 40, 12)
	h.barrier(sh)
	menu := widget.NewEditorMenu(dt, nil)
	menu.SetRows(func() []widget.MenuItemModel {
		return []widget.MenuItemModel{widget.NewCommand("table.copy", "Copy rows", widget.CoreMenuAction{ID: "table.copy"})}
	})
	h.onLoop(func() {
		core = dt.Core()
		dt.SetMenu(menu)
		menu.SetEnabled(true)
	})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 7, Y: 5})
	h.barrier(sh)
	h.onLoop(func() {
		if !replaced || menu.IsOpen() {
			t.Errorf("replaced %v; the menu opened %v for a row that is gone", replaced, menu.IsOpen())
		}
	})
}
