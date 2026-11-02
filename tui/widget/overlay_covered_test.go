package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// covered is f.Covered() and host.Covered(f), read on the loop; the two must agree.
func covered(t *testing.T, h *harness, host *widget.OverlayHost, f *widget.Float) bool {
	t.Helper()
	var viaFloat, viaHost bool
	h.onLoop(func() { viaFloat, viaHost = f.Covered(), host.Covered(f) })
	if viaFloat != viaHost {
		t.Fatalf("Float.Covered %v, OverlayHost.Covered %v", viaFloat, viaHost)
	}
	return viaFloat
}

// TestAPanelBeneathAnOverlappingOneIsCovered: of two overlapping panels the lower is covered and
// the upper isn't; raising the lower swaps them. A panel beside them, overlapping neither, is
// covered by neither.
func TestAPanelBeneathAnOverlappingOneIsCovered(t *testing.T) {
	host := widget.NewOverlayHost(widget.NewButton("base"))
	a, _ := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	b, _ := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	c, _ := panelFloat("C", tui.Rect{X: 2, Y: 12, W: 10, H: 3}) // below both, touching neither
	host.Attach(c)
	host.Attach(a)
	host.Attach(b)
	h := startApp(t, host, 40, 16)
	defer h.stop()
	h.onLoop(func() { c.Show(); a.Show(); b.Show() })
	h.settle()

	if !covered(t, h, host, a) {
		t.Fatal("A, beneath B where they overlap, is not covered")
	}
	if covered(t, h, host, b) {
		t.Fatal("B, on top, is covered")
	}
	if covered(t, h, host, c) {
		t.Fatal("C, beneath A and B but overlapping neither, is covered")
	}

	h.onLoop(func() { host.Raise(a) })
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A, raised above B, is still covered")
	}
	if !covered(t, h, host, b) {
		t.Fatal("B, now beneath A where they overlap, is not covered")
	}
}

// TestOnlyAShownWindowAboveCovers: a hidden panel above covers nothing; a hidden panel is never
// covered; a Float not attached to the host is not covered by it.
func TestOnlyAShownWindowAboveCovers(t *testing.T) {
	host := widget.NewOverlayHost(widget.NewButton("base"))
	a, _ := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	b, _ := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	host.Attach(a)
	host.Attach(b)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() { a.Show(); b.Show() })
	h.settle()
	if !covered(t, h, host, a) {
		t.Fatal("A is not covered by B to begin with")
	}

	h.onLoop(func() { b.Hide() })
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A is covered by B, hidden")
	}

	h.onLoop(func() { b.Show() })
	h.settle()
	if !covered(t, h, host, a) {
		t.Fatal("A is not covered by B, shown again")
	}
	h.onLoop(func() { b.SetVisible(false) }) // shown, but not drawn: its last rectangle is stale
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A is covered by B, shown but not visible")
	}

	h.onLoop(func() { b.SetVisible(true); host.Raise(b); a.Hide() })
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A, hidden, is covered")
	}
	h.onLoop(func() { a.Show() })
	h.settle()
	if !covered(t, h, host, a) {
		t.Fatal("A, shown again beneath B, is not covered")
	}
	h.onLoop(func() { a.SetVisible(false) }) // shown, but set invisible: its last rectangle is stale
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A, shown but set invisible, is covered")
	}
	h.onLoop(func() { a.SetVisible(true) })

	stray, _ := panelFloat("stray", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	var got bool
	h.onLoop(func() { got = host.Covered(stray) })
	if got {
		t.Fatal("a Float not attached to the host is covered by it")
	}
	if host.Covered(nil) {
		t.Fatal("nil is covered")
	}
}

// TestADialogOrATopmostFloatDoesNotCover: a shown modal Float and a topmost one over a panel
// leave it uncovered: the dialog takes the keyboard anyway, and a notification is not a window.
func TestADialogOrATopmostFloatDoesNotCover(t *testing.T) {
	host := widget.NewOverlayHost(widget.NewButton("base"))
	a, _ := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	note, _ := panelFloat("note", tui.Rect{X: 4, Y: 2, W: 10, H: 3})
	dialog := widget.NewFloat(widget.NewButton("ok"), widget.WithModal(true), widget.WithAnchor(widget.AtRect(tui.Rect{X: 6, Y: 2, W: 10, H: 3})))
	host.Attach(a)
	host.AttachTopmost(note)
	host.Attach(dialog)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() { a.Show(); note.Show() })
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A is covered by a topmost float")
	}
	h.onLoop(func() { dialog.Show() })
	h.settle()
	if covered(t, h, host, a) {
		t.Fatal("A is covered by a shown modal dialog")
	}
}
