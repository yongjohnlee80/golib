package tui

import "testing"

// focus_into_test.go: Context.FocusInto — Qt's forceActiveFocus(): focus moved
// INTO a component, whatever its focusable part is.

// intoFixture is `out` beside a pane holding a non-focusable probe and two
// focusable ones; `out` has focus to begin with.
func intoFixture(t *testing.T, pane interface {
	Component
	Add(...Component)
}) (*harness, *focusProbe, *probe, *focusProbe, *focusProbe) {
	t.Helper()
	out := newFocusProbe("out", Size{W: 2, H: 1})
	label := &probe{name: "label", pref: Size{W: 2, H: 1}}
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	pane.Add(label, a, b)
	root := NewFlex(Vertical)
	root.Add(out, pane)
	h := startApp(t, root, 8, 8)
	h.onLoop(func() { h.app.repairFocus() })
	h.sync()
	if got := focusedID(h); got != out.nodeID() {
		t.Fatalf("fixture: focus on %d, want out %d", got, out.nodeID())
	}
	return h, out, label, a, b
}

func focusInto(h *harness, from *focusProbe, comp Component) bool {
	var ok bool
	h.onLoop(func() { ok = from.ctx.FocusInto(comp) })
	h.sync()
	return ok
}

// A container that takes no focus hands it to its first focusable descendant.
func TestFocusIntoAContainerFocusesItsFirstFocusable(t *testing.T) {
	t.Parallel()
	pane := NewFlex(Vertical)
	h, out, _, a, _ := intoFixture(t, pane)
	if !focusInto(h, out, pane) || focusedID(h) != a.nodeID() {
		t.Errorf("focus on %d, want the pane's first focusable, a %d", focusedID(h), a.nodeID())
	}
}

// A container that nominates its focus child hands focus to that one.
func TestFocusIntoPrefersTheNominee(t *testing.T) {
	t.Parallel()
	pane := newNominatingScope()
	h, out, _, _, b := intoFixture(t, pane)
	h.onLoop(func() { pane.nominee, pane.nominate = b, true })
	if !focusInto(h, out, pane) || focusedID(h) != b.nodeID() {
		t.Errorf("focus on %d, want the nominee b %d", focusedID(h), b.nodeID())
	}
}

// A component that takes focus takes it itself.
func TestFocusIntoAFocusableTakesItItself(t *testing.T) {
	t.Parallel()
	pane := NewFlex(Vertical)
	h, out, _, _, b := intoFixture(t, pane)
	if !focusInto(h, out, b) || focusedID(h) != b.nodeID() {
		t.Errorf("focus on %d, want b itself %d", focusedID(h), b.nodeID())
	}
}

// A hidden pane takes none; nor does one with nothing focusable; focus stays.
func TestFocusIntoRefusesWhatCannotTakeIt(t *testing.T) {
	t.Parallel()
	pane := NewFlex(Vertical)
	h, out, label, a, b := intoFixture(t, pane)
	h.onLoop(func() { pane.SetVisible(false) })
	h.sync()
	if focusInto(h, out, pane) || focusedID(h) != out.nodeID() {
		t.Errorf("a hidden pane took focus (now on %d)", focusedID(h))
	}
	h.onLoop(func() { pane.SetVisible(true); a.accepts.Store(false); b.accepts.Store(false) })
	h.sync()
	if focusInto(h, out, pane) || focusedID(h) != out.nodeID() {
		t.Errorf("a pane with nothing focusable took focus (now on %d)", focusedID(h))
	}
	if focusInto(h, out, label) {
		t.Error("a probe that takes no focus took it")
	}
}

// Hidden and focused in the SAME loop turn, before any layout: the pane's
// children still carry their last placement, and must not take focus for it.
func TestFocusIntoAPaneHiddenThisTurnRefuses(t *testing.T) {
	t.Parallel()
	pane := NewFlex(Vertical)
	h, out, _, _, _ := intoFixture(t, pane)
	var ok bool
	h.onLoop(func() {
		pane.SetVisible(false)
		ok = out.ctx.FocusInto(pane)
	})
	h.sync()
	if ok || focusedID(h) != out.nodeID() {
		t.Errorf("a pane hidden this turn took focus (now on %d)", focusedID(h))
	}
}

// A component inside a pane hidden this turn is hidden too: the pane's
// hiding reaches everything under it, before any layout says so.
func TestFocusIntoInsideAPaneHiddenThisTurnRefuses(t *testing.T) {
	t.Parallel()
	pane := NewFlex(Vertical)
	h, out, _, _, b := intoFixture(t, pane)
	var ok bool
	h.onLoop(func() {
		pane.SetVisible(false)
		ok = out.ctx.FocusInto(b)
	})
	h.sync()
	if ok || focusedID(h) != out.nodeID() {
		t.Errorf("a component inside a pane hidden this turn took focus (now on %d)", focusedID(h))
	}
}

// A focus trap elsewhere keeps FocusInto out, as it keeps RequestFocus out.
func TestFocusIntoIsKeptOutByATrap(t *testing.T) {
	t.Parallel()
	trapped := newFocusProbe("trapped", Size{W: 2, H: 1})
	trap := newScopeProbe(true)
	trap.Add(trapped)
	a := newFocusProbe("a", Size{W: 2, H: 1})
	pane := NewFlex(Vertical)
	pane.Add(a)
	root := NewFlex(Vertical)
	root.Add(pane, trap)
	h := startApp(t, root, 8, 8)
	h.onLoop(func() { trapped.ctx.RequestFocus() }) // into the trap, which then confines
	h.sync()
	if got := focusedID(h); got != trapped.nodeID() {
		t.Fatalf("fixture: focus on %d, want the trapped %d", got, trapped.nodeID())
	}
	if focusInto(h, trapped, pane) || focusedID(h) != trapped.nodeID() {
		t.Errorf("FocusInto crossed a focus trap (now on %d)", focusedID(h))
	}
}

// Focusable BY DESIGN is a property of what a subtree holds, not of what it
// accepts now: a pane of disabled buttons holds focusables; a label does not.
func TestHoldsFocusableIsByDesign(t *testing.T) {
	t.Parallel()
	pane := NewFlex(Vertical)
	h, out, label, a, b := intoFixture(t, pane)
	var paneHolds, labelHolds bool
	h.onLoop(func() {
		a.accepts.Store(false)
		b.accepts.Store(false)
		paneHolds, labelHolds = out.ctx.HoldsFocusable(pane), out.ctx.HoldsFocusable(label)
	})
	if !paneHolds {
		t.Error("a pane of buttons that accept no focus NOW was judged not focusable by design")
	}
	if labelHolds {
		t.Error("a label was judged focusable")
	}
}
