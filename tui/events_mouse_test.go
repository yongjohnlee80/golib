package tui

// Mouse & pointer tests: pointer focus resolution, focus trap interaction,
// unmount/redirect skips, and multi-click (double-click) ordinal synthesis.

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pressAt(x, y int) MouseEvent {
	return MouseEvent{Kind: MousePress, Button: MouseLeft, X: x, Y: y}
}

// nodeGone reports whether a NodeID is no longer in the tree, read on the loop
// goroutine so it cannot race the dispatch that removed it.
func nodeGone(h *harness, id NodeID) bool {
	var gone bool
	h.onLoop(func() { gone = h.app.nodes[id] == nil })
	return gone
}

// pressCounts extracts the Count of every MousePress the probe received.
func pressCounts(p *probe) []int {
	var out []int
	for _, ev := range p.recorded() {
		if m, ok := ev.(MouseEvent); ok && m.Kind == MousePress {
			out = append(out, m.Count)
		}
	}
	return out
}

// waitPresses polls until n presses have been DELIVERED. h.sync() is not enough:
// it runs an Update callback on the loop, while injected events travel through
// the backend channel and the intake goroutine, so Update can win the race and
// the probe still holds nothing. Every assertion here would then read an empty
// slice — which is how these tests first "failed", and how the non-press one
// first passed vacuously.
func waitPresses(t *testing.T, p *probe, n int) []int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := pressCounts(p); len(got) >= n {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d presses; got %v", n, pressCounts(p))
	return nil
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Pointer Focus Tests
// ---------------------------------------------------------------------------

// A press focuses the widget under it, and that widget also
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

// Wheel, motion and release never move focus. This is what lets a
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

// A press with no focusable ancestor leaves focus alone. Dead
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

// A press OUTSIDE an active trap does not move focus.
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

// If focus handling unmounts and REPLACES the pointer target, the
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

// A press must be able to ENTER a trap. focusRing(scope) deliberately
// excludes nested trapping scopes, so using it as the containment check refuses
// a click into a modal while the active scope is root ("unrestricted").
func TestPointerCanEnterNestedTrap(t *testing.T) {
	t.Parallel()
	outer := newFocusProbe("outer", Size{W: 8, H: 1})
	inner := newFocusProbe("inner", Size{W: 8, H: 1})
	scope := newScopeProbe(true) // a trap, NOT yet entered
	scope.Add(inner)
	root := NewFlex(Vertical)
	root.Add(outer, scope)
	h := startApp(t, root, 8, 4)

	// Focus something outside: the active scope is now root == unrestricted.
	h.inject(pressAt(1, 0))
	waitFor(t, "outer focused", func() bool { return focusedID(h) == outer.nodeID() })

	// Click INTO the trap. This must focus the child — that is how a user enters
	// a modal with the mouse.
	h.inject(pressAt(1, 1))
	waitFor(t, "click entered the trap", func() bool { return focusedID(h) == inner.nodeID() })
}

// A gained-focus handler may REDIRECT focus while leaving the target
// mounted. The target then receives the press unfocused, which the
// press-focuses-the-target rule
// forbids. Checking target.mounted alone does not catch it.
func TestPointerPressSkippedWhenFocusRedirected(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 1})
	b := newFocusProbe("b", Size{W: 8, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 4)

	// b hands focus straight to a when it gains it, and stays mounted.
	b.onEvent = func(p *probe, ev Event) bool {
		if fe, ok := ev.(FocusEvent); ok && fe.Gained {
			h.app.requestFocusByID(a.nodeID())
		}
		return false
	}

	h.inject(pressAt(1, 1)) // press b
	waitFor(t, "focus redirected to a", func() bool { return focusedID(h) == a.nodeID() })

	for _, ev := range b.recorded() {
		if _, ok := ev.(MouseEvent); ok {
			t.Error("b received the press while UNFOCUSED: its focus was redirected, so " +
				"delivery must be skipped (ADR-0010 §2.1 criterion 1/6)")
		}
	}
}

// ---------------------------------------------------------------------------
// Multi-Click (Double-Click) Timing & Count Tests
// ---------------------------------------------------------------------------

// Two presses on the same cell inside the window count 1 then 2.
func TestDoubleClick_SameCellInsideWindowCounts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(pressAt(2, 1), pressAt(2, 1))

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 2}) {
		t.Errorf("press counts = %v, want [1 2]", got)
	}
}

// ONE CELL APART is not a double-click. A terminal row is one
// cell tall, so a drift is a different row, and activating the row the user did
// not click is worse than requiring a steady hand.
func TestDoubleClick_DifferentCellRestarts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 3})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 3, WithDoubleClickWindow(10*time.Second))

	h.inject(pressAt(2, 1), pressAt(2, 2)) // one row apart

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 1}) {
		t.Errorf("press counts = %v, want [1 1] — a different cell must restart the run", got)
	}
}

// Outside the window is not a double-click. A 1ns window makes
// this deterministic without giving App an injectable clock.
func TestDoubleClick_OutsideWindowRestarts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(time.Nanosecond))

	h.inject(pressAt(2, 1), pressAt(2, 1))

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 1}) {
		t.Errorf("press counts = %v, want [1 1] — a 1ns window can never continue a run", got)
	}
}

// A different button restarts the run.
func TestDoubleClick_DifferentButtonRestarts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(
		MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1},
		MouseEvent{Kind: MousePress, Button: MouseRight, X: 2, Y: 1},
	)

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 1}) {
		t.Errorf("press counts = %v, want [1 1]", got)
	}
}

// A release between the presses is NORMAL and must not interrupt
// the run; that is what a real double-click looks like on the wire.
func TestDoubleClick_InterveningReleaseDoesNotReset(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(
		pressAt(2, 1),
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 2, Y: 1},
		pressAt(2, 1),
	)

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 2}) {
		t.Errorf("press counts = %v, want [1 2] — a release is part of a double-click", got)
	}
}

// Count is 0 on every non-press kind, so no consumer can read a
// count that was never computed.
func TestDoubleClick_NonPressKindsCarryNoCount(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(
		pressAt(2, 1),
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 2, Y: 1},
		MouseEvent{Kind: MouseMotion, Button: MouseNone, X: 2, Y: 1},
		MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 2, Y: 1},
	)
	waitPresses(t, &a.probe, 1)
	waitFor(t, "all four mouse events delivered", func() bool {
		n := 0
		for _, ev := range a.probe.recorded() {
			if _, ok := ev.(MouseEvent); ok {
				n++
			}
		}
		return n >= 4
	})

	seen := 0
	for _, ev := range a.probe.recorded() {
		m, ok := ev.(MouseEvent)
		if !ok || m.Kind == MousePress {
			continue
		}
		seen++
		if m.Count != 0 {
			t.Errorf("%v carries Count %d, want 0", m.Kind, m.Count)
		}
	}
	// Without this the test passes when NO non-press event arrived at all, which
	// is how it first "passed" while the probe was receiving nothing.
	if seen != 3 {
		t.Fatalf("saw %d non-press mouse events, want 3 — the assertion above was vacuous", seen)
	}
}

// The ordinal is stamped BEFORE the focus step, so a
// double-click that also moves focus still delivers Count 2.
func TestDoubleClick_SurvivesTheFocusStep(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 1})
	b := newFocusProbe("b", Size{W: 8, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(pressAt(1, 0)) // focus a
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })

	// Both presses land on b: the first moves focus, the second doubles.
	h.inject(pressAt(1, 1), pressAt(1, 1))
	waitFor(t, "b focused", func() bool { return focusedID(h) == b.nodeID() })

	if got := waitPresses(t, &b.probe, 2); !equalInts(got, []int{1, 2}) {
		t.Errorf("press counts on the newly focused widget = %v, want [1 2]", got)
	}
}

// A press delivered to NOBODY must not advance the run.
//
// The ordinal used to be committed on arrival, before hit-testing and before the
// focus step could decide to skip. So the victim's press counted, and the
// widget that REPLACED it saw Count == 2 as its first ever delivered press.
// Count drives activation, so that is a spurious activation on a first click.
func TestDoubleClick_SkippedPressDoesNotAdvanceTheRun(t *testing.T) {
	t.Parallel()
	victim := newFocusProbe("victim", Size{W: 8, H: 1})
	replacement := newFocusProbe("replacement", Size{W: 8, H: 1})
	filler := &probe{name: "filler", pref: Size{W: 8, H: 1}}
	root := NewFlex(Vertical)
	root.Add(victim, filler)
	h := startApp(t, root, 8, 4, WithDoubleClickWindow(10*time.Second))

	// The replacement must end up in the VICTIM'S CELL, not merely in the tree:
	// at a different cell the run could not continue anyway and the assertion
	// below would pass without testing anything. Flex only appends, so the filler
	// is re-added after the replacement to restore the row order.
	victim.onEvent = func(p *probe, ev Event) bool {
		if fe, ok := ev.(FocusEvent); ok && fe.Gained {
			root.Remove(victim)
			root.Remove(filler)
			root.Add(replacement)
			root.Add(filler)
		}
		return false
	}

	h.inject(pressAt(1, 0)) // skipped: focus handling unmounts the target
	waitFor(t, "focus handler unmounted the pointer target", func() bool {
		return nodeGone(h, victim.nodeID())
	})
	if got := pressCounts(&victim.probe); len(got) != 0 {
		t.Fatalf("precondition: the victim received %v, want nothing — the press must be skipped", got)
	}

	// The replacement is mounted but has measured=false/placed=false until a
	// layout pass, so it is not hit-testable yet. Waiting for its first Layout is
	// what makes the click below actually land — without it the test times out
	// with no presses and says nothing about counting.
	waitFor(t, "replacement laid out and hit-testable", func() bool {
		return replacement.layouts.Load() > 0
	})

	// The replacement now occupies that cell — a DIFFERENT target at the same
	// coordinates. Its FIRST delivered press is a first click and must say so.
	h.inject(pressAt(1, 0))
	got := waitPresses(t, &replacement.probe, 1)
	if got[0] != 1 {
		t.Errorf("the replacement's first delivered press has Count %d, want 1 — "+
			"a press nobody received advanced the run", got[0])
	}
}

// Finding 4 — non-press Count is CANONICALISED, not merely left alone. dispatch
// used to rewrite presses only, so a producer (or a test) could hand a wheel a
// count and it would be delivered verbatim, contradicting the ADR.
func TestDoubleClick_NonPressCountIsCanonicalisedNotIgnored(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	// SEEDED nonzero: the previous version of this assertion passed because
	// nothing ever set a non-press count, not because anything cleared it.
	h.inject(
		MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 2, Y: 1, Count: 99},
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 2, Y: 1, Count: 42},
		MouseEvent{Kind: MouseMotion, Button: MouseNone, X: 2, Y: 1, Count: 7},
	)
	waitFor(t, "all three non-press events delivered", func() bool {
		n := 0
		for _, ev := range a.probe.recorded() {
			if m, ok := ev.(MouseEvent); ok && m.Kind != MousePress {
				n++
			}
		}
		return n >= 3
	})
	for _, ev := range a.probe.recorded() {
		m, ok := ev.(MouseEvent)
		if !ok || m.Kind == MousePress {
			continue
		}
		if m.Count != 0 {
			t.Errorf("%v was delivered with Count %d, want 0 — a seeded count survived", m.Kind, m.Count)
		}
	}
}
