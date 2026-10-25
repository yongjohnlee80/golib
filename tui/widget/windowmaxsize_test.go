package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A window's resize stops at its maximum (WithWindowMaxSize), on each axis, by the pointer and by
// a keyboard step; SetMaxSize changes it live, and a zero or negative dimension has none.
func TestAWindowResizeStopsAtItsMaximum(t *testing.T) {
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowResize(), widget.WithWindowMaxSize(tui.Size{W: 16, H: 7}))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 12, H: 5})))
	host := widget.NewOverlayHost(widget.NewText("base"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 5, Y: 4})
	h.barrier(nil)
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 20, Y: 10})
	h.barrier(nil)
	if got := windowBounds(h, w); got != (tui.Rect{X: 2, Y: 2, W: 16, H: 7}) {
		t.Fatalf("a drag 15 by 6 cells out: %+v, want 16 by 7, the maximum", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 20, Y: 10})
	h.barrier(nil)

	h.onLoop(func() { w.ResizeBy(5, 5) })
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 2, Y: 2, W: 16, H: 7}) {
		t.Fatalf("a keyboard step past the maximum: %+v, want it held at 16 by 7", got)
	}

	h.onLoop(func() { w.Core().SetMaxSize(tui.Size{W: 20}) })
	h.onLoop(func() { w.ResizeBy(10, 3) })
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 2, Y: 2, W: 20, H: 10}) {
		t.Fatalf("after SetMaxSize(20 by none): %+v, want 20 wide and the height free", got)
	}

	h.onLoop(func() { w.Core().SetMaxSize(tui.Size{W: -3, H: -1}) })
	var got tui.Size
	h.onLoop(func() { got = w.Core().MaxSize() })
	if got != (tui.Size{}) {
		t.Fatalf("SetMaxSize(-3, -1): MaxSize %+v, want none", got)
	}
}

// A negative maximum at construction is refused, as a negative minimum is.
func TestANegativeWindowMaximumIsRefused(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewWindowCore took a negative maximum")
		}
	}()
	widget.NewWindowCore(widget.WithWindowResize(), widget.WithWindowMaxSize(tui.Size{W: -1}))
}
