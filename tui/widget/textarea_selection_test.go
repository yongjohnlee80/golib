package widget_test

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A TextArea reports its selection at the cells it draws it on, one rect per row: selected across a
// line break, the cells SelectionCells names are exactly the cells drawn reversed. Unfocused, or
// with no selection, it reports none; its placeholder's cells are reported while it shows.
func TestATextAreaReportsItsSelectionWhereItDrawsIt(t *testing.T) {
	area := widget.NewTextArea(widget.WithTextAreaPlaceholder("say it"))
	sh := newShell(area)
	h := startApp(t, sh, 10, 3)
	defer h.stop()
	h.inject(tab())
	h.barrier(sh)
	var ph []tui.Rect
	var ok bool
	h.onLoop(func() { ph, ok = area.PlaceholderCells() })
	if !ok || !slices.Equal(ph, []tui.Rect{{W: 6, H: 1}}) {
		t.Fatalf("the placeholder's cells %v %v; want the six it is drawn on\n%s", ph, ok, h.grid())
	}
	h.inject(typeString("abc")...)
	h.inject(key(tui.KeyEnter))
	h.inject(typeString("de")...)
	h.barrier(sh)
	h.onLoop(func() { _, ok = area.SelectionCells() })
	if ok {
		t.Fatal("an area with no selection reported cells")
	}
	shiftLeft := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft, Mods: tui.ModShift}
	h.inject(shiftLeft, shiftLeft, shiftLeft, shiftLeft) // back over "de", the break and "c"
	h.barrier(sh)
	var spans []tui.Rect
	h.onLoop(func() { spans, ok = area.SelectionCells(); _, phOK := area.PlaceholderCells(); ok = ok && !phOK })
	var drawn []tui.Rect
	for y, row := range h.tb.Snapshot() {
		for x, c := range row {
			if c.Attrs.Mask&tui.AttrReverse != 0 {
				drawn = append(drawn, tui.Rect{X: x, Y: y, W: 1, H: 1})
			}
		}
	}
	want := []tui.Rect{{X: 2, Y: 0, W: 1, H: 1}, {X: 0, Y: 1, W: 2, H: 1}}
	if !ok || !slices.Equal(spans, want) {
		t.Fatalf("SelectionCells %v %v; want %v (a placeholder reported while text shows?)\n%s", spans, ok, want, h.grid())
	}
	if len(drawn) != 3 || drawn[0] != (tui.Rect{X: 2, Y: 0, W: 1, H: 1}) || drawn[1].Y != 1 || drawn[2].Y != 1 {
		t.Fatalf("the cells drawn reversed %v; want the three SelectionCells names\n%s", drawn, h.grid())
	}
}
