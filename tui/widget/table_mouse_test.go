package widget_test

// Pointer behaviour for Table (ADR-0010 §2.2, criteria 7-10).
//
// Body presses, double-click and body wheel are List's, and are asserted here
// because Table must not break them — under ADR-0004's target-first routing the
// row list is a placed child at local Y:1, so those events reach it directly.
// What Table owns is the HEADER row: a press there is inert, and the wheel is
// forwarded so scrolling over a column title still scrolls the rows.

import (
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

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

// Criterion 8 — THE NEGATIVE CONTROL for this change. Before the fix, Table
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

// Criterion 7 — body press selects the row the keyboard would call current,
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

// Criterion 9 — double-click in the body activates, using List's existing window.
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
	_ = time.Now
}

// Criterion 10 — wheel scrolls over BOTH regions and moves no selection.
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
