package tui

// Pointer focus (ADR-0010 §2.1, criteria 1-6).
//
// A primary press focuses the first focusable node at or above the hit-test
// target, provided it lies inside the ACTIVE focus scope, and only then is the
// event delivered. The cases that matter are the refusals: a press must not
// escape a focus trap, must not clear focus, and must not be delivered to a node
// that focus handling just unmounted.

import (
	"testing"
)

func pressAt(x, y int) MouseEvent {
	return MouseEvent{Kind: MousePress, Button: MouseLeft, X: x, Y: y}
}

// Criterion 1 — a press focuses the widget under it, and that widget also
// receives the press: one gesture, both effects.
func TestPointerPressFocusesAndDelivers(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 1})
	b := newFocusProbe("b", Size{W: 8, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 4)

	h.inject(pressAt(1, 1)) // b's row
	waitFor(t, "b focused by the press", func() bool { return focusedID(h) == b.nodeID() })

	if got := b.eventCount(); got == 0 {
		t.Error("b was focused but never received the press (criterion 1 wants both)")
	}
}

// Criterion 2 — wheel, motion and release never move focus. This is what lets a
// reader scroll a result grid without losing the keyboard.
func TestPointerNonPressDoesNotMoveFocus(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 1})
	b := newFocusProbe("b", Size{W: 8, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 4)

	h.inject(pressAt(1, 0)) // focus a
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })

	h.inject(
		MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 1, Y: 1},
		MouseEvent{Kind: MouseMotion, Button: MouseNone, X: 1, Y: 1},
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 1, Y: 1},
	)
	h.sync()
	if got := focusedID(h); got != a.nodeID() {
		t.Errorf("focus moved to %d on a non-press event; want it to stay on a (%d)", got, a.nodeID())
	}
}

// Criterion 3 — a press with no focusable ancestor leaves focus alone. Dead
// space must not CLEAR focus, which would make a stray click lose the keyboard.
func TestPointerPressOnDeadSpaceKeepsFocus(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 1})
	plain := &probe{name: "plain", pref: Size{W: 8, H: 1}} // not Focusable
	root := NewFlex(Vertical)
	root.Add(a, plain)
	h := startApp(t, root, 8, 4)

	h.inject(pressAt(1, 0))
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })

	h.inject(pressAt(1, 1)) // the non-focusable row
	h.sync()
	if got := focusedID(h); got != a.nodeID() {
		t.Errorf("focus = %d after clicking a non-focusable row; want a (%d) retained",
			got, a.nodeID())
	}
}

// Criterion 4 — a press OUTSIDE an active trap does not move focus.
//
// Deliberately built on a custom PARTIAL-SIZE FocusScope rather than a Float:
// a full-area modal backdrop covers the outside widget, so a broken guard would
// pass by accident simply because nothing outside is clickable.
func TestPointerPressCannotEscapeFocusTrap(t *testing.T) {
	t.Parallel()
	outside := newFocusProbe("outside", Size{W: 8, H: 1})
	inside := newFocusProbe("inside", Size{W: 8, H: 1})
	scope := newScopeProbe(true) // traps focus
	scope.Add(inside)
	root := NewFlex(Vertical)
	root.Add(outside, scope) // outside on row 0, the trap on row 1
	h := startApp(t, root, 8, 4)

	// Enter the trap.
	h.onLoop(func() { h.app.requestFocusByID(inside.nodeID()) })
	waitFor(t, "inside focused", func() bool { return focusedID(h) == inside.nodeID() })

	// Click the widget outside the trap.
	h.inject(pressAt(1, 0))
	h.sync()

	if got := focusedID(h); got != inside.nodeID() {
		t.Errorf("a press outside the trap moved focus to %d; it must stay confined to "+
			"inside (%d) — ADR-0010 §2.1 step 3", got, inside.nodeID())
	}
}

// Criterion 4b — the REPAIR state, and the reason the boundary is currentScope()
// rather than trapScopeOf(focused).
//
// When the trap's last focusable stops accepting focus, repairFocus leaves
// focused = 0 while the scope survives on scopeStack. An implementation deriving
// the boundary from the focused node then finds NO trap and lets a click escape.
// This is the one state such an implementation passes every other test while
// failing.
func TestPointerRespectsSurvivingTrapWithNoFocusables(t *testing.T) {
	t.Parallel()
	outside := newFocusProbe("outside", Size{W: 8, H: 1})
	inside := newFocusProbe("inside", Size{W: 8, H: 1})
	scope := newScopeProbe(true)
	scope.Add(inside)
	root := NewFlex(Vertical)
	root.Add(outside, scope)
	h := startApp(t, root, 8, 4)

	h.onLoop(func() { h.app.requestFocusByID(inside.nodeID()) })
	waitFor(t, "inside focused", func() bool { return focusedID(h) == inside.nodeID() })

	// Remove the trap's only focusable WITHOUT unmounting the scope, then repair:
	// focus becomes empty while the trap is still standing.
	inside.accepts.Store(false)
	h.onLoop(func() { h.app.repairFocus() })
	waitFor(t, "focus empty after repair", func() bool { return focusedID(h) == 0 })

	// Click OUTSIDE: focus must stay empty — confined by the surviving trap.
	h.inject(pressAt(1, 0))
	h.sync()
	if got := focusedID(h); got != 0 {
		t.Errorf("focus escaped a surviving trap to %d while focus was empty; "+
			"the boundary must come from currentScope(), not trapScopeOf(focused) "+
			"— ADR-0010 §2.1 step 1 / criterion 4b", got)
	}

	// Click INSIDE, and this half is what makes the test discriminating.
	//
	// A trapScopeOf(a.focused) implementation reaches focusRing(nil), which is
	// EMPTY, so it refuses every candidate while focus is empty — over-restrictive
	// rather than escaping, which the assertion above passes vacuously. Requiring
	// that a click inside the surviving trap still WORKS pins the behaviour from
	// both sides: the boundary must be the surviving scope, not "no scope".
	inside.accepts.Store(true)
	h.inject(pressAt(1, 1))
	waitFor(t, "click inside the surviving trap focuses it", func() bool {
		return focusedID(h) == inside.nodeID()
	})
}

// Criterion 6 — if focus handling unmounts and REPLACES the pointer target, the
// press is not delivered. A replacement mounted during dispatch has
// measured=false/placed=false and is not hit-testable until layout runs, so
// there is nothing correct to re-target.
func TestPointerPressSkippedWhenFocusHandlerRebuildsTarget(t *testing.T) {
	t.Parallel()
	victim := newFocusProbe("victim", Size{W: 8, H: 1})
	replacement := newFocusProbe("replacement", Size{W: 8, H: 1})
	filler := &probe{name: "filler", pref: Size{W: 8, H: 1}}
	root := NewFlex(Vertical)
	root.Add(victim, filler)
	h := startApp(t, root, 8, 4)

	// On GAINING focus, victim removes itself and mounts a replacement — a real
	// unmount+mount, so stale ancestor geometry cannot make this pass by accident.
	victim.onEvent = func(p *probe, ev Event) bool {
		if fe, ok := ev.(FocusEvent); ok && fe.Gained {
			root.Remove(victim)
			root.Add(replacement)
		}
		return false
	}

	h.inject(pressAt(1, 0))

	// Wait for the unmount rather than sampling: this both settles the dispatch
	// and PROVES the test exercised the skip path instead of passing because
	// nothing happened.
	waitFor(t, "focus handler unmounted the pointer target", func() bool {
		return nodeGone(h, victim.nodeID())
	})

	// The press must reach NEITHER node, and the victim is the one that matters:
	// delivery walks from the original target, so an implementation that skips the
	// unmount check hands the event to a component whose lifecycle has ended.
	// Asserting only on the replacement would pass either way — the replacement is
	// never a delivery target.
	for _, ev := range victim.recorded() {
		if _, ok := ev.(MouseEvent); ok {
			t.Errorf("the UNMOUNTED target received a MouseEvent; the press must be " +
				"skipped once focus handling unmounted it (ADR-0010 §2.1 step 5)")
		}
	}
	for _, ev := range replacement.recorded() {
		if _, ok := ev.(MouseEvent); ok {
			t.Errorf("the replacement received a MouseEvent; a press must not be " +
				"delivered to a subtree mounted during this dispatch")
		}
	}
}

// nodeGone reports whether a NodeID is no longer in the tree, read on the loop
// goroutine so it cannot race the dispatch that removed it.
func nodeGone(h *harness, id NodeID) bool {
	var gone bool
	h.onLoop(func() { gone = h.app.nodes[id] == nil })
	return gone
}
