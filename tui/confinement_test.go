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
