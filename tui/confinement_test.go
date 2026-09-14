package tui

import (
	"sync/atomic"
	"testing"
)

// A trapping focus scope must confine every kind of input to its own subtree.
// The hard case is a PARTIAL-SIZE scope with no focusable descendant: focus
// repair legitimately leaves nothing focused while the trap survives, and a
// naive "fall back to root" then hands keys to the controls the trap covers.
// The pointer half is just as easy to miss — refusing to move focus outside
// the trap does not stop the press being delivered there.
//
// These tests are deliberately generic. They use a bare trapping scope rather
// than any widget, so they pin the runtime rule rather than one component's
// behaviour.

// trapScope traps focus but is not itself focusable, and the tests give it no
// focusable children — the state that produced the original leak.
type trapScope struct {
	MultiChild
	size   Size
	keys   atomic.Int64
	pastes atomic.Int64
	mouse  atomic.Int64
}

func (t *trapScope) Layout(c Constraints) Size {
	if t.ctx != nil {
		for _, ch := range t.All() {
			t.ctx.LayoutChild(ch, Tight(t.size))
			t.ctx.PlaceChild(ch, Rect{W: t.size.W, H: t.size.H})
		}
	}
	return c.Constrain(t.size)
}
func (t *trapScope) Render(Surface)     {}
func (t *trapScope) TrapsFocus() bool   { return true }
func (t *trapScope) AcceptsFocus() bool { return false }

func (t *trapScope) HandleEvent(ev Event) bool {
	switch ev.(type) {
	case KeyEvent:
		t.keys.Add(1)
	case PasteEvent:
		t.pastes.Add(1)
	case MouseEvent:
		t.mouse.Add(1)
	}
	return false // never consume: let anything unconfined reach an ancestor
}

func (t *trapScope) totals() (int64, int64, int64) {
	return t.keys.Load(), t.pastes.Load(), t.mouse.Load()
}

// counter records deliveries without consuming anything, so an event that
// reaches it keeps bubbling and every reachable node is counted.
// Counters are atomic because HandleEvent runs on the loop goroutine while
// the test goroutine reads them — the same reason the package's own probe
// uses atomics.
type counter struct {
	MultiChild
	size   Size
	keys   atomic.Int64
	pastes atomic.Int64
	mouse  atomic.Int64
}

func (c *counter) Layout(cs Constraints) Size {
	if c.ctx != nil {
		x := 0
		for _, ch := range c.All() {
			// LOOSE, not the incoming constraints: the App hands the root a
			// TIGHT terminal-sized box, and forwarding that clamps every child
			// up to full width. That silently placed the second child
			// off-screen and made the pointer assertions below vacuous — the
			// mutation matrix caught it.
			sz := c.ctx.LayoutChild(ch, Loose(Size{W: cs.MaxW, H: cs.MaxH}))
			c.ctx.PlaceChild(ch, Rect{X: x, Y: 0, W: sz.W, H: sz.H})
			x += sz.W
		}
	}
	return cs.Constrain(c.size)
}
func (c *counter) Render(Surface) {}
func (c *counter) HandleEvent(ev Event) bool {
	switch ev.(type) {
	case KeyEvent:
		c.keys.Add(1)
	case PasteEvent:
		c.pastes.Add(1)
	case MouseEvent:
		c.mouse.Add(1)
	}
	return false
}

func (c *counter) totals() (int64, int64, int64) {
	return c.keys.Load(), c.pastes.Load(), c.mouse.Load()
}

// TestTrapConfinesEveryEventKind builds
//
//	root (counter, 20x4)
//	├── trap    (traps focus, LEFT half, no focusable descendant)
//	└── outside (counter, RIGHT half)
//
// so a pointer event on the right half is outside the trap, and root is an
// ancestor of both — the two places a leak shows up.
func TestTrapConfinesEveryEventKind(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	outside := &counter{size: Size{W: 10, H: 4}}
	root.Add(trap, outside)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Enter the trap the way a dialog does, then let repair empty focus: the
	// scope has no focusable descendant, so focused becomes 0 while the scope
	// stays on the stack. That is the exact state under test.
	h.onLoop(func() {
		a := h.app
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: a.byComp[trap].id, restore: 0})
		a.focused = 0
	})
	h.onLoop(func() {
		if h.app.confinement() == nil {
			t.Fatal("precondition failed: no active confinement, so this proves nothing")
		}
	})

	// Everything here targets OUTSIDE the trap: an unfocused key and paste
	// (which would fall back to root), and every mouse kind aimed at the right
	// half of the screen.
	h.inject(keyEv('\r'))
	h.inject(PasteEvent{Text: "x"})
	for _, k := range []MouseKind{MousePress, MouseRelease, MouseMotion, MouseWheel} {
		h.inject(MouseEvent{Kind: k, Button: MouseLeft, X: 15, Y: 1})
	}

	// A SENTINEL, aimed INSIDE the trap. dispatch is single-threaded and
	// ordered, so once the sentinel has been handled every event above it has
	// been handled too. Waiting on a positive signal is the only sound way to
	// assert that the others delivered nowhere — without it, "all counters are
	// zero" is equally consistent with nothing having been processed at all.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "sentinel press inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()

	if k, p, m := root.totals(); k != 0 || p != 0 || m != 0 {
		t.Errorf("root received keys=%d pastes=%d mouse=%d; a trap must confine all of them", k, p, m)
	}
	if k, p, m := outside.totals(); k != 0 || p != 0 || m != 0 {
		t.Errorf("outside leaf received keys=%d pastes=%d mouse=%d; all must be dropped", k, p, m)
	}
	// The key and paste were not lost — they were delivered to the scope, which
	// is the ceiling, and went no further.
	if k, p, _ := trap.totals(); k != 1 || p != 1 {
		t.Errorf("scope received keys=%d pastes=%d, want 1 and 1: a confined event "+
			"is delivered to the scope, not discarded before it", k, p)
	}
}

// TestNoTrapStillReachesRoot is the negative control. Without it the test above
// could pass because events reach nobody at all — a different bug wearing the
// same green tick. It caught exactly that during development.
func TestNoTrapStillReachesRoot(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	inner := &counter{size: Size{W: 10, H: 4}}
	root.Add(inner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	h.inject(keyEv('\r'))
	waitFor(t, "unfocused key reaching root", func() bool {
		k, _, _ := root.totals()
		return k > 0
	})

	h.inject(PasteEvent{Text: "x"})
	waitFor(t, "paste reaching root", func() bool {
		_, p, _ := root.totals()
		return p > 0
	})

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "press being delivered", func() bool {
		_, _, m := root.totals()
		_, _, im := inner.totals()
		return m > 0 || im > 0
	})
}

// TestTabStillTraversesInsideATrap guards the other half of the rule. Bounding
// the bubble must not disable Tab: globalKey is the only implementation of
// traversal, so a generic scope whose children ignore Tab would otherwise stop
// traversing entirely. focusStep is already scope-confined, so reaching it is
// safe.
func TestTabStillTraversesInsideATrap(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	trap := &trapScope{size: Size{W: 20, H: 4}}
	a1 := newFocusProbe("a1", Size{W: 4, H: 1})
	a2 := newFocusProbe("a2", Size{W: 4, H: 1})
	trap.Add(a1, a2)
	root.Add(trap)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var first NodeID
	h.onLoop(func() {
		a := h.app
		a.requestFocus(a.byComp[a1])
		first = a.focused
	})
	if first == 0 {
		t.Fatal("precondition failed: nothing focused inside the trap")
	}

	var wantID NodeID
	h.onLoop(func() { wantID = h.app.byComp[a2].id })

	h.inject(KeyEvent{Code: KeyTab})
	waitFor(t, "Tab to move focus inside the trap", func() bool {
		var f NodeID
		h.onLoop(func() { f = h.app.focused })
		return f != first
	})

	var second NodeID
	h.onLoop(func() { second = h.app.focused })

	if second == first {
		t.Fatal("Tab did not traverse inside the trap; bounding the bubble " +
			"must not disable globalKey")
	}
	if second != wantID {
		t.Errorf("Tab landed on node %d, want %d (the sibling inside the scope)", second, wantID)
	}
}

// TestConfinementFixtureIsWellFormed guards the fixture the confinement test
// depends on, and is not itself a contract.
//
// It exists because the first version of that fixture forwarded the root's
// TIGHT terminal-sized constraints to its children, which clamped both to full
// width and placed the second one off-screen. The "outside" press then landed
// INSIDE the trap, so the pointer assertions passed while proving nothing —
// the mutation matrix caught it, this test stops it coming back.
func TestConfinementFixtureIsWellFormed(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	left := &counter{size: Size{W: 10, H: 4}}
	right := &counter{size: Size{W: 10, H: 4}}
	root.Add(left, right)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	h.onLoop(func() {
		a := h.app
		if got := a.byComp[left].absRect; got.X != 0 || got.W != 10 {
			t.Errorf("left half = %+v, want X=0 W=10", got)
		}
		if got := a.byComp[right].absRect; got.X != 10 || got.W != 10 {
			t.Errorf("right half = %+v, want X=10 W=10", got)
		}
	})

	// The coordinate the confinement test calls "outside" must really reach
	// the right-hand leaf when nothing traps.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 15, Y: 1})
	waitFor(t, "press at x=15 reaching the right-half leaf", func() bool {
		_, _, m := right.totals()
		return m > 0
	})
}

// TestRequestFocusRefusedOutsideTrap covers the first escape path: an outside
// component calling RequestFocus while a modal is still mounted. Without the
// refusal, focus moves out, currentScope falls back to the root, and the whole
// confinement dissolves from the outside while the scope entry is still live.
func TestRequestFocusRefusedOutsideTrap(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	inside := newFocusProbe("inside", Size{W: 4, H: 1})
	outside := newFocusProbe("outside", Size{W: 4, H: 1})
	trap.Add(inside)
	root.Add(trap, outside)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var insideID, outsideID NodeID
	h.onLoop(func() {
		a := h.app
		insideID, outsideID = a.byComp[inside].id, a.byComp[outside].id
		a.requestFocus(a.byComp[inside]) // enter the trap: legal
	})
	h.onLoop(func() {
		if h.app.focused != insideID {
			t.Fatalf("precondition failed: focus is %d, want the in-trap node %d",
				h.app.focused, insideID)
		}
	})

	// The outside sibling asks for focus. It must be refused.
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[outside]) })

	var got NodeID
	h.onLoop(func() { got = h.app.focused })
	if got == outsideID {
		t.Fatal("RequestFocus from outside an active trap was granted; " +
			"confinement can be dissolved from the outside")
	}
	if got != insideID {
		t.Errorf("focus = %d, want it to stay on the in-trap node %d", got, insideID)
	}
}

// TestStaleOutsideFocusStillConfined is the backstop regression. It forces the
// state the refusal above is meant to prevent — focus parked outside while the
// stack entry is live — and proves dispatch still confines.
//
// This is what kills removal of the retarget in confinedTarget: bubbleWithin
// only stops when it MEETS the ceiling, so a walk that starts outside the
// subtree never meets it and would run to the root.
func TestStaleOutsideFocusStillConfined(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	outside := &counter{size: Size{W: 10, H: 4}}
	root.Add(trap, outside)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Live stack entry, and focus deliberately forced OUTSIDE it.
	h.onLoop(func() {
		a := h.app
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: a.byComp[trap].id, restore: 0})
		a.focused = a.byComp[outside].id
	})
	h.onLoop(func() {
		if h.app.confinement() == nil {
			t.Fatal("precondition failed: the live stack entry is not governing")
		}
	})

	h.inject(keyEv('\r'))
	h.inject(PasteEvent{Text: "x"})
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1}) // sentinel, inside
	waitFor(t, "sentinel press inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()

	if k, p, _ := trap.totals(); k != 1 || p != 1 {
		t.Errorf("scope received keys=%d pastes=%d, want 1 and 1: a stale outside "+
			"focus must be retargeted to the confinement, not honoured", k, p)
	}
	if k, p, m := outside.totals(); k != 0 || p != 0 || m != 0 {
		t.Errorf("outside node received keys=%d pastes=%d mouse=%d despite a live trap", k, p, m)
	}
	if k, p, m := root.totals(); k != 0 || p != 0 || m != 0 {
		t.Errorf("root received keys=%d pastes=%d mouse=%d despite a live trap", k, p, m)
	}
}

// TestTrapGovernsViaAncestryWithoutStackEntry covers the fallback path.
//
// The stack is the first authority, but it is not the only one: focus can land
// inside a trapping subtree without that trap ever being entered through
// requestFocus, so no stack entry exists. The scope must still govern, by
// ancestry.
func TestTrapGovernsViaAncestryWithoutStackEntry(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	inside := newFocusProbe("inside", Size{W: 4, H: 1})
	trap.Add(inside)
	root.Add(trap)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// setFocus, not requestFocus: it moves focus without pushing a scope entry,
	// which is exactly the state this fallback exists for.
	h.onLoop(func() {
		a := h.app
		a.setFocus(a.byComp[inside].id)
		if len(a.scopeStack) != 0 {
			t.Fatalf("precondition failed: stack has %d entries, want none", len(a.scopeStack))
		}
	})

	var governing bool
	h.onLoop(func() {
		s := h.app.confinement()
		governing = s != nil && s == h.app.byComp[trap]
	})
	if !governing {
		t.Fatal("a trapping ancestor of the focused node must govern even with an empty scope stack")
	}

	// And it really confines: an unconsumed key must not reach the root.
	h.inject(keyEv('\r'))
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "sentinel press inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()
	if k, _, m := root.totals(); k != 0 || m != 0 {
		t.Errorf("root received keys=%d mouse=%d through an ancestry-only trap", k, m)
	}
}

// TestRepairLeavesFocusEmptyInsideSurvivingTrap exercises the real path into
// the state every other test here reaches by poking scopeStack directly.
//
// A trap whose only focusable child is removed has an empty focus ring, so
// repair leaves focused == 0 while the scope itself survives. That is the
// precondition the whole confinement rule exists for, and reaching it through
// ordinary Remove — rather than by hand — proves the runtime really produces
// it, not just that the tests can simulate it.
func TestRepairLeavesFocusEmptyInsideSurvivingTrap(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	only := newFocusProbe("only", Size{W: 4, H: 1})
	outside := &counter{size: Size{W: 10, H: 4}}
	trap.Add(only)
	root.Add(trap, outside)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Enter the trap for real: requestFocus pushes the scope entry.
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[only]) })
	h.onLoop(func() {
		if h.app.focused == 0 {
			t.Fatal("precondition failed: focus never entered the trap")
		}
		if len(h.app.scopeStack) == 0 {
			t.Fatal("precondition failed: entering the trap pushed no scope entry")
		}
	})

	// Remove the only focusable. Repair finds an empty ring inside a scope that
	// is still mounted.
	h.onLoop(func() { trap.Remove(only) })

	h.onLoop(func() {
		if h.app.focused != 0 {
			t.Errorf("focused = %d after removing the only focusable, want 0", h.app.focused)
		}
		if h.app.confinement() == nil {
			t.Fatal("the surviving trap stopped governing once focus emptied")
		}
	})

	// And input is still confined in that state.
	h.inject(keyEv('\r'))
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 15, Y: 1}) // outside
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})  // sentinel, inside
	waitFor(t, "sentinel press inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()

	if k, _, m := root.totals(); k != 0 || m != 0 {
		t.Errorf("root received keys=%d mouse=%d with focus empty inside a live trap", k, m)
	}
	if _, _, m := outside.totals(); m != 0 {
		t.Errorf("outside node received %d mouse events", m)
	}
}

// focusEventCounter counts FocusEvent deliveries and nothing else. It is
// deliberately separate from counter: counter.totals measures the confined
// input families (key, paste, mouse), and mixing focus traffic into it would
// blur what those assertions mean.
type focusEventCounter struct {
	MultiChild
	size   Size
	focus  atomic.Int64
	accept atomic.Bool
}

func (f *focusEventCounter) Layout(cs Constraints) Size {
	if f.ctx != nil {
		for _, ch := range f.All() {
			sz := f.ctx.LayoutChild(ch, Loose(Size{W: cs.MaxW, H: cs.MaxH}))
			f.ctx.PlaceChild(ch, Rect{W: sz.W, H: sz.H})
		}
	}
	return cs.Constrain(f.size)
}
func (f *focusEventCounter) Render(Surface)     {}
func (f *focusEventCounter) AcceptsFocus() bool { return f.accept.Load() }
func (f *focusEventCounter) HandleEvent(ev Event) bool {
	if _, ok := ev.(FocusEvent); ok {
		f.focus.Add(1)
	}
	return false
}

// TestFocusingTheFocusedNodeIsANoOp pins focus idempotence.
//
// A redundant setFocus must emit no FocusEvents: ancestors style themselves
// from those, so a spurious loss/gain pair would flicker focus-within chrome.
//
// The counter here must observe FocusEvent specifically. An earlier version of
// this test asserted against counter.totals, which only tracks key, paste and
// mouse — so it could not have failed however many focus events were emitted.
func TestFocusingTheFocusedNodeIsANoOp(t *testing.T) {
	root := &focusEventCounter{size: Size{W: 20, H: 4}}
	only := &focusEventCounter{size: Size{W: 4, H: 1}}
	only.accept.Store(true)
	root.Add(only)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var id NodeID
	h.onLoop(func() {
		a := h.app
		a.requestFocus(a.byComp[only])
		id = a.focused
	})
	if id == 0 {
		t.Fatal("precondition failed: nothing focused")
	}
	// Focusing it in the first place MUST have emitted events, or the counter
	// is not observing and the assertion below is worthless.
	if got := only.focus.Load(); got == 0 {
		t.Fatal("focus counter observed nothing on the initial focus; " +
			"it cannot detect a spurious event either")
	}

	before := only.focus.Load()
	beforeRoot := root.focus.Load()
	h.onLoop(func() { h.app.setFocus(id) }) // the same node again

	var after NodeID
	h.onLoop(func() { after = h.app.focused })
	if after != id {
		t.Errorf("focus moved to %d on a redundant setFocus, want %d", after, id)
	}
	if got := only.focus.Load(); got != before {
		t.Errorf("redundant setFocus emitted %d FocusEvent(s) to the node", got-before)
	}
	if got := root.focus.Load(); got != beforeRoot {
		t.Errorf("redundant setFocus bubbled %d FocusEvent(s) to the ancestor", got-beforeRoot)
	}
}

// focusableCounter is a counter that can take focus, so it can stand in for a
// focusable ancestor ABOVE a trapping scope.
type focusableCounter struct{ counter }

func (f *focusableCounter) AcceptsFocus() bool { return true }

// TestPointerCannotFocusAnAncestorOutsideTheTrap covers the escape path that
// scope-rooted hit-testing does NOT close.
//
// Rooting the hit-test at the scope bounds the TARGET, but focusFromPointer
// then walks UP from that target looking for something focusable — and that
// walk can climb straight out of the trap to a focusable ancestor above it.
// The guard inside focusFromPointer is what stops it, and this is the case
// that proves the guard is still load-bearing rather than dead.
func TestPointerCannotFocusAnAncestorOutsideTheTrap(t *testing.T) {
	root := &focusableCounter{counter{size: Size{W: 20, H: 4}}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	inert := &counter{size: Size{W: 10, H: 4}} // inside the trap, NOT focusable
	trap.Add(inert)
	root.Add(trap)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Enter the trap so it governs, with nothing focusable inside it.
	h.onLoop(func() {
		a := h.app
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: a.byComp[trap].id, restore: 0})
		a.focused = 0
	})

	// Press INSIDE the trap. The target is in scope, but the only focusable
	// candidate on the way up is the root, which is outside it.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "press delivered inside the trap", func() bool {
		_, _, m := trap.totals()
		return m > 0
	})
	h.sync()

	var focused, rootID NodeID
	h.onLoop(func() {
		focused = h.app.focused
		rootID = h.app.byComp[root].id
	})
	if focused == rootID {
		t.Fatal("a press inside the trap focused an ancestor OUTSIDE it; " +
			"the ancestor walk escaped the scope")
	}
	if focused != 0 {
		t.Errorf("focused = %d, want 0: no candidate inside the trap accepts focus", focused)
	}
}

// TestInvalidateFocusabilityRepairsFocusStrandedOutsideTheScope.
//
// The early return has to check BOTH that the focused node still accepts focus
// and that it belongs to the scope being revalidated. Those come apart in a
// state this runtime supports on purpose: a trap can be live while focus sits
// outside it, which L1a made reachable and which the confinement rules then
// have to correct. A focusability check alone returns early for that node —
// it is perfectly focusable — and leaves the scope unrepaired while reporting
// that it was checked.
func TestInvalidateFocusabilityRepairsFocusStrandedOutsideTheScope(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	outside := &focusableCounter{counter{size: Size{W: 10, H: 4}}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	inTrap := &focusableCounter{counter{size: Size{W: 4, H: 1}}}
	trap.Add(inTrap)
	root.Add(outside, trap)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Open the trap, then strand focus on the still-eligible node outside it —
	// the exact state a plain focusability check cannot distinguish.
	h.onLoop(func() {
		a := h.app
		a.scopeStack = append(a.scopeStack, scopeEntry{scope: a.byComp[trap].id, restore: 0})
		a.focused = a.byComp[outside].id
	})
	h.onLoop(func() {
		a := h.app
		if a.confinement() == nil {
			t.Fatal("precondition failed: no active trap, so there is no scope to be outside of")
		}
		if !a.acceptsFocus(a.nodes[a.focused]) {
			t.Fatal("precondition failed: the stranded node is not focusable, so a " +
				"focusability check alone would already repair it and this proves nothing")
		}
	})

	h.onLoop(func() { h.app.byComp[outside].ctx.InvalidateFocusability() })
	h.sync()

	var focused, want NodeID
	h.onLoop(func() { focused, want = h.app.focused, h.app.byComp[inTrap].id })
	if focused != want {
		t.Errorf("focused = %d after revalidation, want %d (the focusable inside the "+
			"active trap): an eligible node OUTSIDE the scope must not satisfy the "+
			"early return", focused, want)
	}
}
