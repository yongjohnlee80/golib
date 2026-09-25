package tui

import "testing"

// Children is a component's mounted children in document order, and nil for a
// component that is not mounted.
func TestContextChildrenAreTheMountedOnesInOrder(t *testing.T) {
	a := newFocusProbe("a", Size{W: 5, H: 1})
	b := newFocusProbe("b", Size{W: 5, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 20, 4)
	var got, stray []Component
	h.onLoop(func() {
		got = a.ctx.Children(root)
		stray = a.ctx.Children(newFocusProbe("stray", Size{}))
	})
	if len(got) != 2 || got[0] != Component(a) || got[1] != Component(b) {
		t.Errorf("Children(root) = %v, want a then b", got)
	}
	if stray != nil {
		t.Errorf("Children of a component not mounted = %v, want nil", stray)
	}
}
