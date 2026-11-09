package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// startLongEditor is an editor of 60 lines of 200 columns in a 40x10 screen: it scrolls both ways.
func startLongEditor(t *testing.T) *edHarness {
	t.Helper()
	h := startEditor(t, 40, 10)
	line := strings.Repeat("abcdefghij", 20)
	h.onLoop(func() { h.e.SetValue(strings.TrimSuffix(strings.Repeat(line+"\n", 60), "\n")) })
	h.barrier()
	h.paint()
	return h
}

// The wheel scrolls the native body: three lines a notch down and up, four monospace cells a notch
// sideways with a horizontal wheel, and sideways with Shift and the vertical wheel.
func TestTheWheelScrollsTheNativeBodyBothWays(t *testing.T) {
	h := startLongEditor(t)
	var line, cell float32
	h.onLoop(func() { l := h.e.layout; line, cell = l.lineHeight(l.monoFont()), l.monoCell() })
	if line <= 0 || cell <= 0 {
		t.Fatalf("line %v, cell %v: the layout was not painted", line, cell)
	}
	at := func() (scroll, left float32) {
		h.onLoop(func() { scroll, left = h.e.layout.scroll, h.e.layout.left })
		return
	}
	wheel := func(b tui.MouseButton, mods tui.Mods) {
		t.Helper()
		if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: b, Mods: mods, X: 5, Y: 3}); err != nil {
			t.Fatal(err)
		}
		h.barrier()
	}
	for _, step := range []struct {
		name         string
		b            tui.MouseButton
		mods         tui.Mods
		scroll, left float32
	}{
		{"down", tui.WheelDown, 0, 3 * line, 0},
		{"down again", tui.WheelDown, 0, 6 * line, 0},
		{"up", tui.WheelUp, 0, 3 * line, 0},
		{"right", tui.WheelRight, 0, 3 * line, 4 * cell},
		{"shift-down: right", tui.WheelDown, tui.ModShift, 3 * line, 8 * cell},
		{"shift-up: left", tui.WheelUp, tui.ModShift, 3 * line, 4 * cell},
		{"left", tui.WheelLeft, 0, 3 * line, 0},
		{"left at the edge", tui.WheelLeft, 0, 3 * line, 0},
	} {
		wheel(step.b, step.mods)
		if s, l := at(); s != step.scroll || l != step.left {
			t.Fatalf("after %s: scroll %v, left %v; want %v, %v", step.name, s, l, step.scroll, step.left)
		}
	}
}

// PointerEdge says which edges of the text a drag is past, in pixels; ScrollStep scrolls a line
// down or up and a monospace cell sideways.
func TestPointerEdgeAndScrollStepWorkInPixels(t *testing.T) {
	h := startLongEditor(t)
	h.onLoop(func() {
		p := bodyPointer{h.e.body}
		w, ht := h.e.body.w, h.e.body.h
		for _, c := range []struct {
			name   string
			x, y   int
			dx, dy int
		}{
			{"inside", w / 2, ht / 2, 0, 0},
			{"above", w / 2, -1, 0, -1},
			{"below", w / 2, ht + 1, 0, 1},
			{"left of the text", -1, ht / 2, -1, 0},
			{"right of the text", w + 1, ht / 2, 1, 0},
			{"above and right", w + 1, -1, 1, -1},
		} {
			dx, dy := p.PointerEdge(tui.MouseEvent{X: c.x, Y: c.y})
			if dx != c.dx || dy != c.dy {
				t.Errorf("%s (%d, %d): edge %d, %d; want %d, %d", c.name, c.x, c.y, dx, dy, c.dx, c.dy)
			}
		}
		l := h.e.layout
		line, cell := l.lineHeight(l.monoFont()), l.monoCell()
		p.ScrollStep(1, 1)
		if l.scroll != line || l.left != cell {
			t.Errorf("ScrollStep(1, 1): scroll %v, left %v; want a line (%v) and a cell (%v)", l.scroll, l.left, line, cell)
		}
		p.ScrollStep(-1, -1)
		if l.scroll != 0 || l.left != 0 {
			t.Errorf("ScrollStep(-1, -1): scroll %v, left %v; want back at 0, 0", l.scroll, l.left)
		}
	})
}
