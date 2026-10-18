package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// csvDataTable is a DataTable editing a CSV document, with the edges, and the document's core.
func csvDataTable(t *testing.T, text string, w, h int) (*harness, *shell, *widget.DataTable, *widget.EditorCore) {
	t.Helper()
	core := widget.NewEditorCore(widget.CoreInitialText(text))
	hn, sh, dt := dataTable(t, widget.NewTableCSV(core, ','), w, h, widget.TableAppendEdges())
	return hn, sh, dt, core
}

func dtPress(h *harness, sh *shell, dt *widget.DataTable, evs ...tui.Event) {
	h.onLoop(func() {
		for _, ev := range evs {
			dt.HandleEvent(ev)
		}
	})
	h.barrier(sh)
}

func keyText(s string) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: []rune(s)[0], Text: s}
}

var (
	dtEnter = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	dtEsc   = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}
)

// TestDataTableDrawsAnEditableTable (ADR 1792303119 §4.3): a value's newline and tab show as ⏎ and
// →; the edge field is titled +, and the edge row is drawn after the last. Enter opens the field
// over the cell, its lines growing downward, the terminal's caret in it; a kept value is drawn in
// its cell.
func TestDataTableDrawsAnEditableTable(t *testing.T) {
	h, sh, dt, core := csvDataTable(t, "name,note\nann,\"one\ntwo\"\nbob,\"a\tb\"\n", 40, 8)
	if !strings.Contains(h.row(1), "one⏎two") || !strings.Contains(h.row(2), "a→b") {
		t.Fatalf("the one-line cells:\n%q\n%q", h.row(1), h.row(2))
	}
	if !strings.Contains(h.row(0), " + ") {
		t.Fatalf("no edge field in the header: %q", h.row(0))
	}
	var drawn int
	h.onLoop(func() { drawn = dt.Core().Len() })
	if drawn != 2 || strings.TrimSpace(h.row(4)) != "" {
		t.Fatalf("rows %d; below the edge row: %q", drawn, h.row(4))
	}

	dtPress(h, sh, dt, keyText("l"), dtEnter)
	if !strings.Contains(h.row(1), "one") || strings.Contains(h.row(1), "one⏎two") || !strings.Contains(h.row(2), "two") {
		t.Fatalf("the open field, over its cell and the row below:\n%q\n%q", h.row(1), h.row(2))
	}
	var x, y int
	var ok bool
	h.onLoop(func() { x, y, ok = dt.Cursor() })
	if !ok || y != 2 || x != strings.Index(h.row(2), "two")+3 {
		t.Fatalf("the caret at %d,%d (%v), want after two on row 2:\n%q", x, y, ok, h.row(2))
	}
	dtPress(h, sh, dt, keyText("!"), dtEnter)
	if v := core.Value(); v != "name,note\nann,\"one\ntwo!\"\nbob,\"a\tb\"\n" {
		t.Fatalf("kept: %q", v)
	}
	if !strings.Contains(h.row(1), "one⏎two!") {
		t.Fatalf("the kept value: %q", h.row(1))
	}
	h.onLoop(func() { _, _, ok = dt.Cursor() })
	if ok {
		t.Fatal("a caret with no field open")
	}
}

// TestDataTableDrawsOsRowAndTakesPaste: o draws an empty row below the cursor, the rows under it
// moved down; a paste goes into the field; Esc takes the row away again, nothing written.
func TestDataTableDrawsOsRowAndTakesPaste(t *testing.T) {
	const text = "k,v\na,1\nb,2\n"
	h, sh, dt, core := csvDataTable(t, text, 40, 8)
	dtPress(h, sh, dt, keyText("o"))
	if !strings.Contains(h.row(1), "a") || strings.Contains(h.row(2), "b") || !strings.Contains(h.row(3), "b") {
		t.Fatalf("o's row:\n%q\n%q\n%q", h.row(1), h.row(2), h.row(3))
	}
	dtPress(h, sh, dt, tui.PasteEvent{Text: "pasted"})
	if !strings.Contains(h.row(2), "pasted") {
		t.Fatalf("the paste: %q", h.row(2))
	}
	dtPress(h, sh, dt, dtEsc)
	if core.Value() != text || !strings.Contains(h.row(2), "b") {
		t.Fatalf("Esc: %q; row 2 %q", core.Value(), h.row(2))
	}
}

// TestADoubleClickEditsAnEditableCell: on an editable table a double click opens the cell's field
// rather than activating its row; a click on the edge field puts the cursor there.
func TestADoubleClickEditsAnEditableCell(t *testing.T) {
	h, sh, dt, _ := csvDataTable(t, "k,v\na,1\n", 40, 6)
	at := strings.Index(h.row(1), "1")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: at, Y: 1},
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: at, Y: 1, Count: 2})
	h.barrier(sh)
	var editing bool
	h.onLoop(func() { editing = dt.Core().Editing() != nil })
	if !editing {
		t.Fatal("a double click opened no field")
	}
	dtPress(h, sh, dt, dtEsc)
	plus := strings.Index(h.row(0), "+")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: plus, Y: 1})
	h.barrier(sh)
	if got := tableCursor(h, dt); got != (widget.TablePos{Row: 0, Col: 2}) {
		t.Fatalf("a click on the edge field: %+v", got)
	}
}

// TestTheWheelScrollsTheFieldsSideways (§4.4): Shift+wheel and a horizontal wheel move the first
// field shown, a field a step, clamped; the cursor stays; the plain wheel still scrolls rows.
func TestTheWheelScrollsTheFieldsSideways(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(20), 12, 6)
	if !strings.HasPrefix(h.row(0), "id") {
		t.Fatalf("header %q", h.row(0))
	}
	wheel := func(b tui.MouseButton, mods tui.Mods) {
		h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: b, Mods: mods, X: 2, Y: 2})
		h.barrier(sh)
	}
	wheel(tui.WheelDown, tui.ModShift)
	if strings.HasPrefix(h.row(0), "id") || !strings.HasPrefix(h.row(0), "name") {
		t.Fatalf("Shift+wheel down: %q, want name first", h.row(0))
	}
	wheel(tui.WheelRight, 0)
	wheel(tui.WheelRight, 0)
	wheel(tui.WheelRight, 0)
	if !strings.HasPrefix(h.row(0), "n") || strings.HasPrefix(h.row(0), "name") {
		t.Fatalf("clamped at the last field: %q", h.row(0))
	}
	wheel(tui.WheelLeft, 0)
	wheel(tui.WheelUp, tui.ModShift)
	if !strings.HasPrefix(h.row(0), "id") {
		t.Fatalf("back to the first field: %q", h.row(0))
	}
	if got := tableCursor(h, dt); got != (widget.TablePos{}) {
		t.Fatalf("the wheel moved the cursor to %+v", got)
	}
}
