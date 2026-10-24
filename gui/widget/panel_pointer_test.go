package widget_test

import (
	"testing"

	gw "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
)

// A floating, resizable panel's right column, bottom row and corner are its resize arrows, as
// its presses resize from there; a panel that doesn't resize has none.
func TestAFloatingPanelsEdgesAreResizeArrows(t *testing.T) {
	at := tui.Rect{X: 2, Y: 2, W: 24, H: 6}
	h := startPanel(t, at, gw.WithResizable())
	var got []tui.PointerRegion
	h.onLoop(func() { got = h.panel.PointerShapes(nil) })
	want := []tui.PointerRegion{
		{Rect: tui.Rect{X: at.W - 1, Y: 1, W: 1, H: at.H - 2}, Shape: tui.PointerResizeEW},
		{Rect: tui.Rect{Y: at.H - 1, W: at.W - 1, H: 1}, Shape: tui.PointerResizeNS},
		{Rect: tui.Rect{X: at.W - 1, Y: at.H - 1, W: 1, H: 1}, Shape: tui.PointerResizeNWSE},
	}
	if len(got) != len(want) {
		t.Fatalf("regions %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("region %d: %+v, want %+v", i, got[i], want[i])
		}
	}

	fixed := startPanel(t, at)
	fixed.onLoop(func() { got = fixed.panel.PointerShapes(nil) })
	if len(got) != 0 {
		t.Errorf("a panel that doesn't resize: %+v", got)
	}
}
