package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A TextInput reports its selection at the cells it draws it on: after the scroll, clipped where the
// field clips, a wide cluster cut by an edge left out. Selected from the start to the end, the field
// scrolled to the end, the selection runs out of view on the left; selected from the end back to the
// start, the field scrolled to the start, it runs out of view on the right. In both, the cells
// SelectionCells names are exactly the cells drawn reversed.
func TestATextInputReportsItsSelectionWhereItDrawsIt(t *testing.T) {
	value := "abcdefghij界klmnopqrstuvwxyz0123界456"
	for name, keys := range map[string][]tui.Event{
		"scrolled to the end":   {key(tui.KeyHome), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnd, Mods: tui.ModShift}},
		"scrolled to the start": {key(tui.KeyEnd), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyHome, Mods: tui.ModShift}},
	} {
		t.Run(name, func(t *testing.T) {
			h, in, sh := focusedInput(t, widget.WithInitialValue(value))
			defer h.stop()
			h.inject(keys...)
			h.barrier(sh)
			var spans []tui.Rect
			var ok bool
			h.onLoop(func() { spans, ok = in.SelectionCells() })
			row := h.tb.Snapshot()[0]
			var drawn []int
			for x, c := range row {
				if c.Attrs.Mask&tui.AttrReverse != 0 {
					drawn = append(drawn, x)
				}
			}
			if !ok || len(spans) != 1 || len(drawn) == 0 {
				t.Fatalf("SelectionCells %v %v; %d cells drawn reversed\n%s", spans, ok, len(drawn), h.grid())
			}
			// A wide cluster is one cell holding it and a continuation cell: both lie in the span.
			s := spans[0]
			if s.Y != 0 || s.H != 1 || s.X != drawn[0] || s.X+s.W-1 < drawn[len(drawn)-1] {
				t.Fatalf("SelectionCells %+v; the reversed cells run from %d to %d\n%s", s, drawn[0], drawn[len(drawn)-1], h.grid())
			}
			for x := s.X; x < s.X+s.W; x++ {
				if c := row[x]; c.Attrs.Mask&tui.AttrReverse == 0 && c.Content != "" {
					t.Fatalf("cell %d is in the span but not drawn selected\n%s", x, h.grid())
				}
			}
		})
	}
}

// With no selection, or the field unfocused, there are no cells: ok is false.
func TestATextInputWithoutASelectionReportsNoCells(t *testing.T) {
	in, other := widget.NewTextInput(widget.WithInitialValue("abc")), widget.NewTextInput()
	root := tui.NewFlex(tui.Vertical)
	root.Add(in, other)
	h := startApp(t, root, 20, 2)
	defer h.stop()
	cells := func() (ok bool) {
		h.settle()
		h.onLoop(func() { _, ok = in.SelectionCells() })
		return ok
	}
	h.onLoop(func() { in.Context().RequestFocus() })
	if cells() {
		t.Fatal("a field with no selection reported cells")
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft, Mods: tui.ModShift})
	if !cells() {
		t.Fatal("a selected cluster reported no cells")
	}
	h.onLoop(func() { other.Context().RequestFocus() }) // the selection stays, undrawn
	if cells() {
		t.Fatal("an unfocused field reported cells it does not draw")
	}
}
