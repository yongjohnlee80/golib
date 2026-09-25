package widget_test

// Table contract: header row + column-aligned rows over a List core.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

type crewRow struct{ name, role, note string }

func crewColumns() []widget.TableColumn[crewRow] {
	return []widget.TableColumn[crewRow]{
		{Title: "Name", Width: 10, Cell: func(r crewRow) string { return r.name }},
		{Title: "Role", Width: 8, Cell: func(r crewRow) string { return r.role }},
		{Title: "Note", Width: 0, Cell: func(r crewRow) string { return r.note }}, // flex
	}
}

func TestTableHeaderAndRows(t *testing.T) {
	tab := widget.NewTable(crewColumns())
	sh := newShell(tab)
	h := startApp(t, sh, 48, 6)
	h.onLoop(func() {
		tab.SetItems([]crewRow{
			{"ada", "eng", "first"},
			{"grace", "capt", "cobol"},
		})
	})
	h.barrier(sh)

	h.wantContains("Name")
	h.wantContains("Role")
	h.wantContains("Note")
	h.wantContains("ada")
	h.wantContains("cobol")

	// Header and cells align: each row's "Role" column starts where the
	// header says it does (fixed 10 + 2-gap layout).
	scr := h.grid()
	lines := strings.Split(scr, "\n")
	hdr, row := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Name") {
			hdr = i
		}
		if strings.Contains(l, "ada") {
			row = i
		}
	}
	if hdr < 0 || row < 0 {
		t.Fatalf("header/row not found:\n%s", scr)
	}
	if strings.Index(lines[hdr], "Role") != strings.Index(lines[row], "eng") {
		t.Errorf("Role column misaligned:\n%q\n%q", lines[hdr], lines[row])
	}
}

func TestTableEmptyText(t *testing.T) {
	tab := widget.NewTable(crewColumns(), widget.WithEmptyText[crewRow]("no crew yet"))
	sh := newShell(tab)
	h := startApp(t, sh, 48, 6)
	h.barrier(sh)
	h.wantContains("Name") // header always renders
	h.wantContains("no crew yet")
}

func TestTableCursorNavigation(t *testing.T) {
	tab := widget.NewTable(crewColumns())
	sh := newShell(tab)
	h := startApp(t, sh, 48, 6)
	h.onLoop(func() {
		tab.SetItems([]crewRow{{"ada", "eng", ""}, {"grace", "capt", ""}})
	})
	h.barrier(sh)

	var idx int
	var ok bool
	h.onLoop(func() { idx, ok = tab.Selected() })
	if !ok || idx != 0 {
		t.Fatalf("initial Selected() = %d,%v; want 0,true", idx, ok)
	}
	// Down moves the cursor even when the event is delivered to the Table
	// (controller-forwarding path).
	h.onLoop(func() { tab.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}) })
	h.onLoop(func() { idx, ok = tab.Selected() })
	if !ok || idx != 1 {
		t.Errorf("after Down Selected() = %d,%v; want 1,true", idx, ok)
	}
}

// Flex columns share the remaining width EVENLY: a table of all-flex
// columns renders equal columns instead of the first one swallowing the
// row (autodb M6: a uuid id column took the whole results pane).
func TestTableFlexColumnsShareWidth(t *testing.T) {
	type row struct{ a, b, c string }
	cols := []widget.TableColumn[row]{
		{Title: "A", Cell: func(r row) string { return r.a }},
		{Title: "B", Cell: func(r row) string { return r.b }},
		{Title: "C", Cell: func(r row) string { return r.c }},
	}
	tbl := widget.NewTable(cols, widget.WithItems([]row{{
		a: strings.Repeat("x", 40), b: "short", c: "tiny",
	}}, func(r row) string { return r.a }))
	sh := newShell(tbl)
	h := startApp(t, sh, 62, 6)
	h.inject(tab())
	h.barrier(sh)

	// The header row shows each title at its column origin; with even
	// sharing the three origins are evenly spaced.
	line := h.row(0)
	ia := strings.Index(line, "A")
	ib := strings.Index(line, "B")
	ic := strings.Index(line, "C")
	if ia < 0 || ib < 0 || ic < 0 {
		t.Fatalf("headers missing: %q", line)
	}
	// Even sharing, up to the one-column division remainder that the
	// leftmost flex columns absorb.
	gap1, gap2 := ib-ia, ic-ib
	if d := gap1 - gap2; d < 0 || d > 1 {
		t.Fatalf("columns not evenly shared: origins %d/%d/%d (%q)", ia, ib, ic, line)
	}
	// A fixed column keeps its width; the rest still share.
	if gap1 < 8 {
		t.Fatalf("flex share below the minimum: %d", gap1)
	}
}

// A Table renders through its COLUMNS even when the caller supplies rows
// with WithItems (whose render func is for plain lists).
func TestTableIgnoresCallerRowRenderer(t *testing.T) {
	type row struct{ a, b string }
	cols := []widget.TableColumn[row]{
		{Title: "A", Width: 10, Cell: func(r row) string { return r.a }},
		{Title: "B", Cell: func(r row) string { return r.b }},
	}
	tbl := widget.NewTable(cols,
		widget.WithItems([]row{{a: "alpha", b: "beta"}},
			func(r row) string { return "RAW-" + r.a })) // must NOT win
	sh := newShell(tbl)
	h := startApp(t, sh, 40, 6)
	h.settle()

	grid := h.grid()
	if strings.Contains(grid, "RAW-") {
		t.Fatalf("caller render func overrode the column renderer:\n%s", grid)
	}
	if !strings.Contains(grid, "alpha") || !strings.Contains(grid, "beta") {
		t.Fatalf("column cells missing:\n%s", grid)
	}
}

func tableWithRows(t *testing.T, n, w, h int) (*harness, *widget.Table[string]) {
	t.Helper()
	rows := make([]string, n)
	for i := range rows {
		rows[i] = "row-" + string(rune('a'+i%26))
	}
	tbl := widget.NewTable[string]([]widget.TableColumn[string]{
		{Title: "NAME", Width: 10, Cell: func(s string) string { return s }},
	})
	tbl.SetItems(rows)
	hh := startApp(t, tbl, w, h)
	hh.settle()
	return hh, tbl
}

func selected(t *testing.T, tbl *widget.Table[string]) int {
	t.Helper()
	i, ok := tbl.List().Selected()
	if !ok {
		return -1
	}
	return i
}

// press at an ABSOLUTE grid position; row 0 is the header.
func press(y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: y}
}

// THE NEGATIVE CONTROL for this change. Before the fix, Table
// forwarded the header press with Table-local Y=0 and the list selected row 0.
func TestTableHeaderPressIsInert(t *testing.T) {
	h, tbl := tableWithRows(t, 8, 20, 6)

	h.onLoop(func() { tbl.List().SetCursor(2) })
	h.settle()
	if got := selected(t, tbl); got != 2 {
		t.Fatalf("precondition: selection = %d, want 2", got)
	}

	h.inject(press(0)) // the header row
	h.settle()

	if got := selected(t, tbl); got != 2 {
		t.Errorf("header press moved the selection to %d; a column title must not "+
			"select a row (ADR-0010 §2.2)", got)
	}
}

// Body press selects the row the keyboard would call current,
// at a NON-ZERO scroll offset, which is where a header-offset error hides.
func TestTableBodyPressAtScrollOffset(t *testing.T) {
	h, tbl := tableWithRows(t, 30, 20, 6) // header + 5 body rows

	// Scroll down 3 rows with the wheel over the body, then click the first
	// visible body row: it must be row 3, not row 0.
	for i := 0; i < 3; i++ {
		h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 3})
	}
	h.settle()

	h.inject(press(1)) // first body row (absolute Y=1, list-local Y=0)
	h.settle()

	if got := selected(t, tbl); got != 3 {
		t.Errorf("body press at scroll offset 3 selected %d, want 3", got)
	}
}

// Double-click in the body activates, using List's existing window.
func TestTableBodyDoubleClickActivates(t *testing.T) {
	h, tbl := tableWithRows(t, 8, 20, 6)
	acts := record[widget.ActivateEvent](h)

	h.inject(press(2), press(2)) // same row twice, well inside doubleClickWindow
	h.settle()

	ev, ok := acts.last()
	if !ok {
		t.Fatalf("double-click emitted no ActivateEvent")
	}
	if ev.Index != 1 { // absolute Y=2 -> list-local Y=1 -> row 1
		t.Errorf("ActivateEvent index = %d, want 1", ev.Index)
	}
	if got := selected(t, tbl); got != 1 {
		t.Errorf("selection = %d, want 1", got)
	}
}

// Wheel scrolls over BOTH regions and moves no selection.
func TestTableWheelOverHeaderAndBodyScrolls(t *testing.T) {
	h, tbl := tableWithRows(t, 30, 20, 6)

	h.onLoop(func() { tbl.List().SetCursor(0) })
	h.settle()

	// Baseline: what is the first visible body row?
	firstVisible := func() int {
		h.inject(press(1))
		h.settle()
		return selected(t, tbl)
	}
	if got := firstVisible(); got != 0 {
		t.Fatalf("precondition: first visible row = %d, want 0", got)
	}

	// Wheel over the HEADER (absolute Y=0) must scroll the body.
	h.onLoop(func() { tbl.List().SetCursor(0) })
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 0})
	h.settle()
	selBefore := selected(t, tbl)
	if got := firstVisible(); got != 1 {
		t.Errorf("wheel over the header did not scroll: first visible row = %d, want 1", got)
	}
	if selBefore != 0 {
		t.Errorf("wheel over the header moved the selection to %d", selBefore)
	}

	// Wheel over the BODY scrolls too, and still moves no selection.
	h.onLoop(func() { tbl.List().SetCursor(0) })
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 3})
	h.settle()
	if got := selected(t, tbl); got != 0 {
		t.Errorf("wheel over the body moved the selection to %d", got)
	}
}

// Table must not be a pointer SINK, which an implementation review caught:
// consuming every non-wheel MouseEvent stopped body motion and release at the
// Table, so an ancestor never saw them. A Split whose divider drag continues over
// a Table body then froze, and because the release was swallowed too it stayed in
// `dragging`, letting a later unpressed motion resize it.
//
// This is the ancestor interaction none of the focused Table tests exercised.
func TestTableDoesNotSwallowAncestorDrag(t *testing.T) {
	rows := make([]string, 20)
	for i := range rows {
		rows[i] = "r"
	}
	tbl := widget.NewTable[string]([]widget.TableColumn[string]{
		{Title: "NAME", Width: 6, Cell: func(s string) string { return s }},
	})
	tbl.SetItems(rows)
	other := widget.NewText("other")

	// Horizontal split: the divider sits at a column, the Table is pane B.
	sp := widget.NewSplit(widget.Horizontal, other, tbl, widget.WithRatio(0.5))
	h := startApp(t, sp, 20, 8)
	h.settle()

	start := sp.Ratio()

	// Press the divider, then drag and release INSIDE the Table body.
	divider := 10 // 20 cells * 0.5
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: divider, Y: 3})
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseNone, X: divider + 4, Y: 3})
	h.settle()

	moved := sp.Ratio()
	if moved == start {
		t.Errorf("drag over the Table body did not move the divider: ratio still %.2f — "+
			"Table swallowed the motion an ancestor needed", start)
	}

	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: divider + 4, Y: 3})
	h.settle()

	// After release the drag must be OVER: an unpressed motion must not resize.
	afterRelease := sp.Ratio()
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseNone, X: divider - 6, Y: 3})
	h.settle()
	if got := sp.Ratio(); got != afterRelease {
		t.Errorf("an unpressed motion changed the ratio %.2f -> %.2f: the release never "+
			"reached the Split, so it is still dragging", afterRelease, got)
	}
}

// TestSetColumnsRedrawsTheHeaderAndRows: the same table, new columns — the
// header and the rows follow, with the rows' source kept.
func TestSetColumnsRedrawsTheHeaderAndRows(t *testing.T) {
	type row struct{ a, b string }
	tbl := widget.NewTable([]widget.TableColumn[row]{
		{Title: "FIRST", Width: 8, Cell: func(r row) string { return r.a }},
	}, widget.WithItems([]row{{"one", "uno"}}, func(r row) string { return r.a }))
	h := startApp(t, tbl, 30, 4)
	defer h.stop()
	h.settle()
	h.wantContains("FIRST")
	h.wantContains("one")
	h.onLoop(func() {
		tbl.SetColumns([]widget.TableColumn[row]{
			{Title: "SECOND", Width: 8, Cell: func(r row) string { return r.b }},
			{Title: "THIRD", Width: 0, Cell: func(r row) string { return r.a + "!" }},
		})
	})
	h.settle()
	h.wantContains("SECOND")
	h.wantContains("THIRD")
	h.wantContains("uno")
	h.wantContains("one!")
	h.wantNotContains("FIRST")
}
