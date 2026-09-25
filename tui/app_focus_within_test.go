package tui

import "testing"

// App.FocusWithin answers as Context.FocusWithin does: whether the focused
// node is comp or under it, for a caller holding only the App.
func TestAppFocusWithinIsTheFocusedNodesAncestry(t *testing.T) {
	a := newFocusProbe("a", Size{W: 5, H: 1})
	b := newFocusProbe("b", Size{W: 5, H: 1})
	pane := NewFlex(Vertical)
	pane.Add(a)
	root := NewFlex(Vertical)
	root.Add(pane, b)
	h := startApp(t, root, 20, 4)
	var inPane, inB, inStray, viaCtx bool
	h.onLoop(func() {
		a.ctx.RequestFocus()
		inPane, inB = h.app.FocusWithin(pane), h.app.FocusWithin(b)
		inStray = h.app.FocusWithin(newFocusProbe("stray", Size{}))
		viaCtx = b.ctx.FocusWithin(pane)
	})
	if !inPane || inB || inStray || !viaCtx {
		t.Errorf("focus on a: within pane %v (want true), b %v, a stray %v (want false), via a Context %v (want true)",
			inPane, inB, inStray, viaCtx)
	}
}
