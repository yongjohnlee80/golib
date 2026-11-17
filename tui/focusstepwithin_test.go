package tui

import (
	"testing"
)

// paneProbe is a pane that keeps Tab within itself, as a decl Pane does: a vertical Flex whose
// handler asks KeyCommand for FocusNext and FocusPrev, under whatever keys the map binds, and steps
// focus within its own subtree, consuming the key.
type paneProbe struct {
	Flex
}

func newPaneProbe(children ...Component) *paneProbe {
	p := &paneProbe{Flex: *NewFlex(Vertical)}
	p.Add(children...)
	return p
}

func (p *paneProbe) HandleEvent(ev Event) bool {
	e, ok := ev.(KeyEvent)
	if !ok || p.ctx == nil {
		return false
	}
	cmd, ok := p.ctx.KeyCommand(e, FocusNext, FocusPrev)
	if !ok {
		return false
	}
	dir := 1
	if cmd.ID == FocusPrev.ID {
		dir = -1
	}
	p.ctx.FocusStepWithin(p, dir)
	return true
}

// stepWithin runs App.FocusStepWithin on the loop and returns its answer.
func stepWithin(h *harness, comp Component, dir int) bool {
	var ok bool
	h.onLoop(func() { ok = h.app.FocusStepWithin(comp, dir) })
	return ok
}

// focusOn focuses n's node on the loop and waits until it holds focus.
func focusOn(t *testing.T, h *harness, p *focusProbe) {
	t.Helper()
	h.onLoop(func() { h.app.requestFocusByID(p.nodeID()) })
	waitFor(t, "focus on "+p.name, func() bool { return focusedID(h) == p.nodeID() })
}

func stops(names ...string) []*focusProbe {
	out := make([]*focusProbe, len(names))
	for i, n := range names {
		out[i] = newFocusProbe(n, Size{W: 2, H: 1})
	}
	return out
}

// TestFocusStepWithinWrapsInsideItsSubtree: a step moves through comp's stops in Tab's order and
// wraps at its ends, never to a stop beside it, which the whole scope's Tab would reach. From
// outside comp, a step lands on comp's first stop, or its last.
func TestFocusStepWithinWrapsInsideItsSubtree(t *testing.T) {
	t.Parallel()
	s := stops("before", "a", "b", "c", "after")
	pane := newPaneProbe(s[1], s[2], s[3])
	root := NewFlex(Vertical)
	root.Add(s[0], pane, s[4])
	h := startApp(t, root, 8, 10)
	defer h.wait()
	h.sync()

	focusOn(t, h, s[1])
	for _, want := range []*focusProbe{s[2], s[3], s[1]} {
		if !stepWithin(h, pane, 1) {
			t.Fatalf("a step forward reported no move")
		}
		if got := focusedID(h); got != want.nodeID() {
			t.Fatalf("a step forward focused %d, want %s (%d)", got, want.name, want.nodeID())
		}
	}
	for _, want := range []*focusProbe{s[3], s[2], s[1], s[3]} {
		stepWithin(h, pane, -1)
		if got := focusedID(h); got != want.nodeID() {
			t.Fatalf("a step back focused %d, want %s (%d)", got, want.name, want.nodeID())
		}
	}

	focusOn(t, h, s[0])
	stepWithin(h, pane, 1)
	if got := focusedID(h); got != s[1].nodeID() {
		t.Errorf("a step forward from outside focused %d, want the first stop a (%d)", got, s[1].nodeID())
	}
	focusOn(t, h, s[4])
	stepWithin(h, pane, -7) // the size of dir is no matter: one step
	if got := focusedID(h); got != s[3].nodeID() {
		t.Errorf("a step back from outside focused %d, want the last stop c (%d)", got, s[3].nodeID())
	}
}

// TestFocusStepWithinFollowsAMovedFocusNext: a pane that steps on FocusNext and FocusPrev follows
// the key map: with them moved to Ctrl+N and Ctrl+P, Ctrl+N steps and wraps within the pane, Ctrl+P
// steps back, and Tab, bound to nothing now, moves nothing.
func TestFocusStepWithinFollowsAMovedFocusNext(t *testing.T) {
	t.Parallel()
	s := stops("before", "a", "b", "after")
	pane := newPaneProbe(s[1], s[2])
	root := NewFlex(Vertical)
	root.Add(s[0], pane, s[3])
	m := mustMap(t, Commands(), mustScheme(t, `<mapping>
<shortcut id="tui.app.focusNext"><key value="Ctrl+N"/></shortcut>
<shortcut id="tui.app.focusPrev"><key value="Ctrl+P"/></shortcut>
</mapping>`))
	h := startApp(t, root, 8, 10, WithKeymap(m))
	defer h.wait()
	h.sync()

	focusOn(t, h, s[1])
	h.inject(KeyEvent{Code: 'n', Mods: ModCtrl})
	waitFor(t, "Ctrl+N steps to b", func() bool { return focusedID(h) == s[2].nodeID() })
	h.inject(KeyEvent{Code: 'n', Mods: ModCtrl})
	waitFor(t, "Ctrl+N wraps to a, never to the stop after the pane", func() bool { return focusedID(h) == s[1].nodeID() })
	h.inject(KeyEvent{Code: 'p', Mods: ModCtrl})
	waitFor(t, "Ctrl+P steps back to b", func() bool { return focusedID(h) == s[2].nodeID() })
	h.inject(tabEv(false))
	h.sync()
	if got := focusedID(h); got != s[2].nodeID() {
		t.Errorf("Tab, unbound, moved focus to %d", got)
	}
}

// TestFocusStepWithinRefusesOutsideTheActiveTrap: while a trapping scope holds focus, a subtree
// outside it steps nothing, as it may not take the pointer: false, and focus stays in the trap.
func TestFocusStepWithinRefusesOutsideTheActiveTrap(t *testing.T) {
	t.Parallel()
	s := stops("inside", "a", "b", "inside2")
	trap := newScopeProbe(true)
	trap.Add(s[0], s[3])
	pane := newPaneProbe(s[1], s[2])
	root := NewFlex(Vertical)
	root.Add(trap, pane)
	h := startApp(t, root, 8, 10)
	defer h.wait()
	h.sync()

	focusOn(t, h, s[0])
	if stepWithin(h, pane, 1) {
		t.Error("a step outside the active trap reported a move")
	}
	h.sync()
	if got := focusedID(h); got != s[0].nodeID() {
		t.Errorf("a step outside the active trap moved focus to %d, want it left inside (%d)", got, s[0].nodeID())
	}
	// inside the trap, the same step runs
	if !stepWithin(h, trap, 1) {
		t.Error("a step inside the active trap reported no move")
	}
	if got := focusedID(h); got != s[3].nodeID() {
		t.Errorf("a step inside the active trap focused %d, want its second stop (%d)", got, s[3].nodeID())
	}
}

// TestFocusStepWithinOnItsOnlyStopReportsNoMove: comp whose only stop already has the focus has
// nowhere to step: false, either way, and the focus stays. From outside, the same step moves to it
// and reports so.
func TestFocusStepWithinOnItsOnlyStopReportsNoMove(t *testing.T) {
	t.Parallel()
	s := stops("outside", "only")
	pane := newPaneProbe(s[1])
	root := NewFlex(Vertical)
	root.Add(s[0], pane)
	h := startApp(t, root, 8, 10)
	defer h.wait()
	h.sync()

	focusOn(t, h, s[0])
	if !stepWithin(h, pane, 1) {
		t.Error("a step from outside onto the only stop reported no move")
	}
	if got := focusedID(h); got != s[1].nodeID() {
		t.Fatalf("a step from outside focused %d, want the only stop (%d)", got, s[1].nodeID())
	}
	for _, dir := range []int{1, -1} {
		if stepWithin(h, pane, dir) {
			t.Errorf("a step %+d on the only stop, focused already, reported a move", dir)
		}
	}
	if got := focusedID(h); got != s[1].nodeID() {
		t.Errorf("focus moved to %d, want it left on the only stop (%d)", got, s[1].nodeID())
	}
}

// TestFocusStepWithinLeavesANestedTrapOut: a trapping scope inside comp is not stepped into, as Tab
// does not step into one: the stops either side of it follow each other.
func TestFocusStepWithinLeavesANestedTrapOut(t *testing.T) {
	t.Parallel()
	s := stops("a", "trapped", "b")
	nested := newScopeProbe(true)
	nested.Add(s[1])
	pane := newPaneProbe(s[0], nested, s[2])
	root := NewFlex(Vertical)
	root.Add(pane)
	h := startApp(t, root, 8, 10)
	defer h.wait()
	h.sync()

	focusOn(t, h, s[0])
	for _, want := range []*focusProbe{s[2], s[0], s[2]} {
		stepWithin(h, pane, 1)
		if got := focusedID(h); got != want.nodeID() {
			t.Fatalf("a step focused %d, want %s (%d): the nested trap's stop is not this ring's", got, want.name, want.nodeID())
		}
	}
}

// TestFocusStepWithinWithNothingToStepMovesNothing: comp holding no stop now, comp not mounted, and
// a dir of 0 each answer false and leave focus where it is.
func TestFocusStepWithinWithNothingToStepMovesNothing(t *testing.T) {
	t.Parallel()
	s := stops("outside", "off")
	s[1].accepts.Store(false)
	pane := newPaneProbe(s[1])
	root := NewFlex(Vertical)
	root.Add(s[0], pane)
	h := startApp(t, root, 8, 10)
	defer h.wait()
	h.sync()

	focusOn(t, h, s[0])
	for name, ok := range map[string]bool{
		"a pane with no stop":     stepWithin(h, pane, 1),
		"a dir of 0":              stepWithin(h, root, 0),
		"a component not mounted": stepWithin(h, newPaneProbe(newFocusProbe("x", Size{W: 2, H: 1})), 1),
		"nil":                     stepWithin(h, nil, 1),
	} {
		if ok {
			t.Errorf("%s: reported a move", name)
		}
	}
	h.sync()
	if got := focusedID(h); got != s[0].nodeID() {
		t.Errorf("focus moved to %d, want it left on outside (%d)", got, s[0].nodeID())
	}
}
