package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// focus_into_later_test.go: FocusInto on a pane shown in the same turn takes
// effect once the pane is laid out — as Qt's forceActiveFocus takes an item
// being shown — while a hidden pane takes none and a later focus change wins.

// twoPanes is a field on top, and a pane holding another field below it,
// hidden.
func twoPanes(t *testing.T) (*harness, *widget.TextInput, *widget.TextInput, *widget.Box) {
	t.Helper()
	top, inner := widget.NewTextInput(), widget.NewTextInput()
	pane := widget.NewBox(inner)
	pane.SetVisible(false)
	root := tui.NewFlex(tui.Vertical)
	root.Add(top, pane)
	h := startApp(t, root, 30, 8)
	h.onLoop(func() { top.Context().RequestFocus() })
	h.settle()
	return h, top, inner, pane
}

func focused(h *harness, w *widget.TextInput) bool {
	var ok bool
	h.onLoop(func() { ok = w.Context() != nil && w.Context().Focused() })
	return ok
}

func TestFocusIntoAPaneShownInTheSameTurnTakesEffectAfterLayout(t *testing.T) {
	h, top, inner, pane := twoPanes(t)
	defer h.stop()
	h.onLoop(func() {
		pane.SetVisible(true)
		top.Context().FocusInto(pane) // not laid out yet: deferred
	})
	h.settle()
	if !focused(h, inner) {
		t.Error("focus did not reach the pane once it was laid out")
	}
}

func TestFocusIntoAHiddenPaneIsNotDeferred(t *testing.T) {
	h, top, inner, pane := twoPanes(t)
	defer h.stop()
	h.onLoop(func() { top.Context().FocusInto(pane) }) // still hidden
	h.settle()
	h.onLoop(func() { pane.SetVisible(true) })
	h.settle()
	if focused(h, inner) || !focused(h, top) {
		t.Error("a FocusInto on a hidden pane took effect once the pane was shown later")
	}
}

func TestAFocusChangeOvertakesADeferredFocusInto(t *testing.T) {
	top, other, inner := widget.NewTextInput(), widget.NewTextInput(), widget.NewTextInput()
	pane := widget.NewBox(inner)
	pane.SetVisible(false)
	root := tui.NewFlex(tui.Vertical)
	root.Add(top, other, pane)
	h := startApp(t, root, 30, 10)
	defer h.stop()
	h.onLoop(func() { top.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() {
		pane.SetVisible(true)
		top.Context().FocusInto(pane)  // deferred until the pane is laid out
		other.Context().RequestFocus() // the user moved on first
	})
	h.settle()
	if !focused(h, other) || focused(h, inner) {
		t.Error("a deferred FocusInto took the keyboard back after focus had moved on")
	}
}
