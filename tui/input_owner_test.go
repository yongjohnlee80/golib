package tui

import (
	"sync/atomic"
	"testing"
)

// A layer bound to an owner inside the active trap takes the pointer as the trap's own content
// does; one bound to nothing, or to an owner outside the trap, stays outside it. Generic nodes
// rather than widgets, so these pin the runtime rule rather than a menu.

// ownedLayer is a counter that takes the pointer capture on a press and records whether it got
// it, and records hover.
type ownedLayer struct {
	counter
	captured atomic.Int32 // 0 not asked, 1 granted, 2 refused
	hovered  atomic.Bool
}

func (l *ownedLayer) HandleEvent(ev Event) bool {
	if m, ok := ev.(MouseEvent); ok && m.Kind == MousePress {
		if l.ctx.CapturePointer() {
			l.captured.Store(1)
			l.ctx.ReleasePointer()
		} else {
			l.captured.Store(2)
		}
	}
	return l.counter.HandleEvent(ev)
}

func (l *ownedLayer) HoverChanged(h bool) { l.hovered.Store(h) }

// ownerFixture builds, left to right across a 20x4 screen,
//
//	root (counter)
//	├── trap  [0,5)    the active scope; its one child, owner, fills it
//	├── bound [5,10)   bound to owner
//	├── loose [10,15)  bound to nothing
//	└── stray [15,20)  bound to loose, which is outside the trap
//
// with the trap entered as a dialog enters it.
func ownerFixture(t *testing.T) (h *harness, root, owner, loose *counter, trap *trapScope, bound, stray *ownedLayer) {
	t.Helper()
	root = &counter{size: Size{W: 20, H: 4}}
	trap = &trapScope{size: Size{W: 5, H: 4}}
	owner = &counter{size: Size{W: 5, H: 4}}
	trap.Add(owner)
	bound = &ownedLayer{counter: counter{size: Size{W: 5, H: 4}}}
	loose = &counter{size: Size{W: 5, H: 4}}
	stray = &ownedLayer{counter: counter{size: Size{W: 5, H: 4}}}
	root.Add(trap, bound, loose, stray)

	h = startApp(t, root, 20, 4)
	h.sync()
	h.onLoop(func() {
		if !root.ctx.SetChildInputOwner(bound, owner.ctx.NodeAnchor()) {
			t.Error("binding bound to owner was refused")
		}
		if !root.ctx.SetChildInputOwner(stray, loose.ctx.NodeAnchor()) {
			t.Error("binding stray to loose was refused")
		}
		a := h.app
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: a.byComp[trap].id, restore: 0})
		a.focused = 0
	})
	h.onLoop(func() {
		if h.app.confinement() != h.app.byComp[trap] {
			t.Fatal("precondition failed: the trap is not the active scope")
		}
	})
	return h, root, owner, loose, trap, bound, stray
}

// TestALayerBoundIntoTheTrapTakesThePointer: a press on the bound layer reaches it, it may take
// the capture, and the press stops at the layer rather than bubbling to the root behind the trap.
// The unbound layer and the layer bound outside the trap get nothing.
func TestALayerBoundIntoTheTrapTakesThePointer(t *testing.T) {
	h, root, owner, loose, trap, bound, stray := ownerFixture(t)
	defer h.wait()

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 7, Y: 1})  // bound
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 12, Y: 1}) // loose
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 17, Y: 1}) // stray
	// The sentinel, inside the trap: once it has arrived, every press above it has been handled.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "the sentinel press inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()

	if _, _, m := bound.totals(); m != 1 {
		t.Errorf("the bound layer received %d presses, want 1", m)
	}
	if got := bound.captured.Load(); got != 1 {
		t.Errorf("the bound layer's capture: %d, want 1 (granted)", got)
	}
	if _, _, m := root.totals(); m != 0 {
		t.Errorf("the root behind the trap received %d presses: the bound layer is the ceiling", m)
	}
	if _, _, m := loose.totals(); m != 0 {
		t.Errorf("the unbound layer received %d presses", m)
	}
	if _, _, m := stray.totals(); m != 0 {
		t.Errorf("the layer bound outside the trap received %d presses", m)
	}
	if got := stray.captured.Load(); got != 0 {
		t.Errorf("the layer bound outside the trap was asked to capture (%d)", got)
	}
	if _, _, m := owner.totals(); m != 1 {
		t.Errorf("the owner inside the trap received %d presses, want the sentinel's 1", m)
	}
}

// TestALayerBoundIntoTheTrapIsHovered: hover follows the same target routing does.
func TestALayerBoundIntoTheTrapIsHovered(t *testing.T) {
	h, _, _, _, _, bound, stray := ownerFixture(t)
	defer h.wait()

	h.inject(MouseEvent{Kind: MouseMotion, X: 17, Y: 1})
	h.sync()
	if stray.hovered.Load() {
		t.Error("the layer bound outside the trap is hovered")
	}
	h.inject(MouseEvent{Kind: MouseMotion, X: 7, Y: 1})
	waitFor(t, "the bound layer hovered", func() bool { return bound.hovered.Load() })
}

// TestABindingNeedsAChildAndALiveOwner: only a container's own mounted child can be bound, to a
// mounted owner; the zero ref clears a binding, and the cleared layer is outside the trap again.
func TestABindingNeedsAChildAndALiveOwner(t *testing.T) {
	h, root, owner, _, trap, bound, _ := ownerFixture(t)
	defer h.wait()

	h.onLoop(func() {
		if trap.ctx.SetChildInputOwner(bound, owner.ctx.NodeAnchor()) {
			t.Error("a node that is not bound's parent bound it")
		}
		if root.ctx.SetChildInputOwner(owner, owner.ctx.NodeAnchor()) {
			t.Error("root bound a node that is not its child")
		}
		if root.ctx.SetChildInputOwner(bound, AnchorRef{owner: 1 << 40}) {
			t.Error("a binding to a node that does not exist was accepted")
		}
		if !root.ctx.SetChildInputOwner(bound, AnchorRef{}) {
			t.Error("clearing the binding was refused")
		}
	})
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 7, Y: 1})
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "the sentinel press inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()
	if _, _, m := bound.totals(); m != 0 {
		t.Errorf("the unbound layer received %d presses", m)
	}
}

// TestOwnersInARingEndTheWalk: two layers bound to each other, neither inside the trap, are
// refused rather than walked forever.
func TestOwnersInARingEndTheWalk(t *testing.T) {
	h, root, _, loose, trap, _, stray := ownerFixture(t)
	defer h.wait()

	h.onLoop(func() {
		if !root.ctx.SetChildInputOwner(loose, stray.ctx.NodeAnchor()) {
			t.Error("binding loose to stray was refused")
		}
		if h.app.inInputScope(h.app.byComp[stray], h.app.byComp[trap]) {
			t.Error("a ring of owners outside the trap was admitted")
		}
	})
}
