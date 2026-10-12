package widget_test

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// panelFloat is a non-modal floating panel at r: a titled box around one button.
func panelFloat(title string, r tui.Rect) (*widget.Float, *widget.Button) {
	btn := widget.NewButton(title + "-btn")
	return widget.NewFloat(widget.NewBox(btn, widget.WithTitle(title)), widget.WithAnchor(widget.AtRect(r))), btn
}

// clickUp is a left click at (x, y): the press and its release.
func clickUp(x, y int) []tui.Event {
	return []tui.Event{click(x, y), tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y}}
}

// layerOrder is where each of cs is in the host's layers, bottom to top; -1 for one not there.
func layerOrder(h *harness, host *widget.OverlayHost, cs ...tui.Component) []int {
	out := make([]int, len(cs))
	h.onLoop(func() {
		layers := slices.Collect(func(yield func(tui.Component) bool) {
			for _, l := range host.Stack.All() {
				if !yield(l) {
					return
				}
			}
		})
		for i, c := range cs {
			out[i] = slices.Index(layers, c)
		}
	})
	return out
}

// TestFocusRaisesANonModalFloat: two overlapping panels, the second attached on top. The keyboard
// entering the lower one (Tab) raises it above the other, and a click on the other's uncovered
// part raises that one back; focus stays on what took it. A modal dialog stays above both,
// whichever is raised, and a topmost notification stays above everything.
func TestFocusRaisesANonModalFloat(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	a, aBtn := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	b, bBtn := panelFloat("B", tui.Rect{X: 12, Y: 4, W: 20, H: 6})
	host.Attach(a)
	host.Attach(b)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		a.Show()
		b.Show()
		base.Context().RequestFocus()
	})
	h.settle()
	if o := layerOrder(h, host, a, b); o[0] > o[1] {
		t.Fatalf("attach order %v; want A beneath B before any focus", o)
	}

	h.inject(tab()) // base -> A's button: the keyboard enters the lower panel
	h.settle()
	if !focusedOn(t, h, aBtn) {
		t.Fatal("Tab did not focus A's button")
	}
	if o := layerOrder(h, host, a, b); o[0] < o[1] {
		t.Fatalf("layers %v; focusing A left it beneath B", o)
	}
	if !focusedOn(t, h, aBtn) {
		t.Fatal("raising A moved the focus")
	}

	// B's bottom-right corner is outside A: a click there focuses B's content and raises B.
	h.inject(clickUp(30, 8)...)
	h.settle()
	if !focusedOn(t, h, bBtn) {
		t.Fatal("the click did not focus B's button")
	}
	if o := layerOrder(h, host, a, b); o[1] < o[0] {
		t.Fatalf("layers %v; clicking into B left it beneath A", o)
	}

	// A topmost notification, then a dialog over both panels.
	toasts := widget.NewToasts(widget.WithToastWidth(12))
	h.onLoop(func() {
		host.AttachTopmost(toasts.Float())
		toasts.Post(widget.Toast{Text: "note", Ongoing: true})
	})
	h.settle()
	h.inject(clickUp(4, 2)...) // into A's uncovered part: A comes up, still beneath the notification
	h.settle()
	if !focusedOn(t, h, aBtn) {
		t.Fatalf("the click did not focus A's button:\n%s", h.grid())
	}
	if o := layerOrder(h, host, a, b, toasts.Float()); o[0] < o[1] || o[2] < o[0] {
		t.Fatalf("layers %v (A, B, toasts); want A above B and the notification above both", o)
	}
	h.onLoop(func() { host.Raise(b) })
	m := widget.NewModal(widget.NewText("dialog"), widget.WithModalTitle("D"), widget.WithScrim(true))
	openOn(t, h, m, host)
	h.onLoop(func() { host.Raise(a) }) // the keyboard is in the dialog: raise A as a program would
	h.settle()
	o := layerOrder(h, host, a, b, m, toasts.Float())
	if o[0] < o[1] {
		t.Fatalf("layers %v; Raise(A) left it beneath B", o)
	}
	if o[2] < o[0] || o[2] < o[1] {
		t.Fatalf("layers %v (A, B, dialog, toasts); a panel was raised above the open dialog", o)
	}
	if o[3] < o[2] {
		t.Fatalf("layers %v (A, B, dialog, toasts); the topmost notification went beneath the dialog", o)
	}
	if top := host.TopModal(); top != m {
		t.Fatal("the dialog is no longer the top modal")
	}
	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
}

// TestShowingAModalFloatPutsItAboveRaisedPanels: a modal Float attached before the panels is an
// open dialog once shown, above a panel raised since, and beneath a topmost layer.
func TestShowingAModalFloatPutsItAboveRaisedPanels(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	dlgBtn := widget.NewButton("ok")
	dlg := widget.NewFloat(widget.NewBox(dlgBtn, widget.WithTitle("Dialog")), widget.WithModal(true))
	a, aBtn := panelFloat("A", tui.Rect{X: 2, Y: 1, W: 20, H: 6})
	host.Attach(dlg)
	host.Attach(a)
	toasts := widget.NewToasts(widget.WithToastWidth(12))
	host.AttachTopmost(toasts.Float())
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		a.Show()
		aBtn.Context().RequestFocus()
	})
	h.settle()
	h.onLoop(dlg.Show)
	h.settle()
	o := layerOrder(h, host, a, dlg, toasts.Float())
	if o[1] < o[0] {
		t.Fatalf("layers %v (A, dialog, toasts); the shown dialog is beneath the panel", o)
	}
	if o[2] < o[1] {
		t.Fatalf("layers %v (A, dialog, toasts); the dialog went above the topmost layer", o)
	}
	if !focusedOn(t, h, dlgBtn) {
		t.Fatal("the dialog did not take the keyboard")
	}
}
