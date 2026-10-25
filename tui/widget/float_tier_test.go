package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// float_tier_test.go: a tiled Float (SetTiled) stacks beneath every floating layer, raised only to
// the top of the tiled ones.

// A tiled panel stays beneath a floating one however it is raised: the keyboard entering it (Tab),
// a click into it, and Raise itself each keep it under. A panel opened after it tiled is above it,
// and the base stays beneath them all. A dialog opened while the keyboard is in the tiled panel is
// above everything. Released, the panel goes to the top of the floating ones, as a raise would.
func TestATiledFloatStaysBeneathTheFloatingOnes(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	later, _ := panelFloat("C", tui.Rect{X: 24, Y: 1, W: 14, H: 5}) // attached first, opened last
	a, aBtn := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	b, _ := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	host.Attach(later)
	host.Attach(a)
	host.Attach(b)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		a.Show()
		b.Show()
		a.SetTiled(true)
		base.Context().RequestFocus()
	})
	h.settle()
	beneath := func(when string) {
		t.Helper()
		if o := layerOrder(h, host, base, a, b, later); o[0] != 0 || o[1] > o[2] || o[1] > o[3] {
			t.Fatalf("%s: layers %v (base, A tiled, B, C); want the base first and A beneath B and C", when, o)
		}
	}
	beneath("tiled")
	h.inject(tab()) // base -> A's button: the keyboard enters the tiled panel
	h.settle()
	if !focusedOn(t, h, aBtn) {
		t.Fatal("Tab did not focus A's button")
	}
	beneath("the keyboard in A")
	h.inject(clickUp(4, 2)...) // into A's uncovered part
	h.settle()
	beneath("a click into A")
	h.onLoop(func() { host.Raise(a) })
	h.settle()
	beneath("Raise(A)")
	h.onLoop(func() { later.Show() })
	h.settle()
	beneath("C opened after A tiled")

	m := widget.NewModal(widget.NewText("dialog"), widget.WithModalTitle("D"), widget.WithScrim(true))
	openOn(t, h, m, host) // the keyboard was in A
	if o := layerOrder(h, host, a, b, later, m); o[3] < o[0] || o[3] < o[1] || o[3] < o[2] {
		t.Fatalf("layers %v (A tiled, B, C, dialog); want the dialog above every panel", o)
	}
	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	h.onLoop(func() { a.SetTiled(false) })
	h.settle()
	if o := layerOrder(h, host, a, b, later); o[0] < o[1] || o[0] < o[2] {
		t.Fatalf("released: layers %v (A, B, C); want A at the top of the floating panels", o)
	}
}

// A Float tiled before it is attached lands beneath the floating layers attached before it.
func TestAFloatTiledBeforeItIsAttachedLandsBeneathTheFloatingOnes(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	b, _ := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	a, _ := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	a.SetTiled(true)
	host.Attach(b)
	host.Attach(a)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	if o := layerOrder(h, host, base, a, b); o[0] != 0 || o[1] > o[2] {
		t.Fatalf("layers %v (base, A tiled, B); want the base first and A beneath B", o)
	}
}
