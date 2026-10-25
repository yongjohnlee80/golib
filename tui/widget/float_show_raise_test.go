package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// float_show_raise_test.go: a window that opens comes forward. Showing a Float raises it, a
// non-modal one without taking the keyboard.

// A panel shown while another panel holds the keyboard opens above it, and the keyboard stays where
// it was: a card that opens on its own is seen, and takes no keys it was not given.
func TestShowingANonModalFloatRaisesItWithoutTheKeyboard(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	a, _ := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	b, bBtn := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	host.Attach(a) // attached first: beneath B by attach order
	host.Attach(b)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		b.Show()
		bBtn.Context().RequestFocus()
	})
	h.settle()
	if !focusedOn(t, h, bBtn) {
		t.Fatal("B's button did not take the keyboard")
	}
	h.onLoop(func() { a.Show() })
	h.settle()
	if o := layerOrder(h, host, a, b); o[0] < o[1] {
		t.Fatalf("layers %v (A, B); A, shown after B took the keyboard, opened beneath it", o)
	}
	if !focusedOn(t, h, bBtn) {
		t.Fatal("showing A took the keyboard from B")
	}
}

// A panel hidden and shown again comes forward again: shown is opened, wherever it was before.
func TestShowingAHiddenFloatAgainRaisesIt(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	a, _ := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	b, _ := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	host.Attach(a)
	host.Attach(b)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		a.Show()
		b.Show()
	})
	h.settle()
	if o := layerOrder(h, host, a, b); o[0] > o[1] {
		t.Fatalf("layers %v (A, B); want B, shown last, on top", o)
	}
	h.onLoop(func() {
		a.Hide()
		a.Show()
	})
	h.settle()
	if o := layerOrder(h, host, a, b); o[0] < o[1] {
		t.Fatalf("layers %v (A, B); A shown again stayed beneath B", o)
	}
}
