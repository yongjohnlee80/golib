package tui

import (
	"sync/atomic"
	"testing"
	"time"
)

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

// designProbe is a container whose focusability is a per-instance choice.
type designProbe struct {
	focusProbe
	designed bool
}

func (d *designProbe) FocusableByDesign() bool { return d.designed }

// A FocusDesigner answers for itself: built not to take focus, it is not
// focusable by design even though it implements Focusable; built to, it is.
func TestHoldsFocusableAsksAFocusDesigner(t *testing.T) {
	t.Parallel()
	off := &designProbe{focusProbe: *newFocusProbe("off", Size{W: 2, H: 1})}
	off.accepts.Store(false)
	on := &designProbe{focusProbe: *newFocusProbe("on", Size{W: 2, H: 1}), designed: true}
	on.accepts.Store(false) // designed to take focus, and not accepting it now
	root := NewFlex(Vertical)
	root.Add(off, on)
	h := startApp(t, root, 8, 8)
	var offHolds, onHolds bool
	h.onLoop(func() {
		offHolds, _ = h.app.HoldsFocusable(off)
		onHolds, _ = h.app.HoldsFocusable(on)
	})
	if offHolds {
		t.Error("a FocusDesigner built not to take focus was judged focusable by design")
	}
	if !onHolds {
		t.Error("a FocusDesigner built to take focus, refusing it NOW, was judged not focusable by design")
	}
}

// Two panes swapped in one turn — the focused one hidden, the other shown and focused into: after
// the layout the focus is in the shown pane. The hidden focus is not repaired to the first
// focusable in the window first, which would overtake the FocusInto waiting for that layout.
func TestFocusIntoAPaneShownAsTheFocusedOneIsHidden(t *testing.T) {
	t.Parallel()
	out := newFocusProbe("out", Size{W: 2, H: 1})
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	paneA, paneB := NewFlex(Vertical), NewFlex(Vertical)
	paneA.Add(a)
	paneB.Add(b)
	paneB.SetVisible(false)
	root := NewFlex(Vertical)
	root.Add(out, paneA, paneB)
	h := startApp(t, root, 8, 8)
	h.onLoop(func() { a.ctx.RequestFocus() })
	h.sync()
	if got := focusedID(h); got != a.nodeID() {
		t.Fatalf("fixture: focus on %d, want a %d", got, a.nodeID())
	}
	h.onLoop(func() {
		paneA.SetVisible(false)
		paneB.SetVisible(true)
		a.ctx.FocusInto(paneB)
	})
	h.sync()
	if got := focusedID(h); got != b.nodeID() {
		t.Errorf("after the swap the focus is on %d, want b %d (out is %d)", got, b.nodeID(), out.nodeID())
	}
}

// Keys typed right after the one that showed a field and focused into it reach the field, not the
// control the focus was leaving, though no frame runs between them: the deferred focus settles
// before the next key is routed. A frame cap of an hour keeps every frame out after the first.
func TestKeysAfterAFocusIntoReachTheShownField(t *testing.T) {
	t.Parallel()
	out := newFocusProbe("out", Size{W: 2, H: 1})
	field := newFocusProbe("field", Size{W: 2, H: 1})
	pane := NewFlex(Vertical)
	pane.Add(field)
	pane.SetVisible(false)
	out.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == '/' {
			pane.SetVisible(true)
			p.ctx.FocusInto(pane)
			return true
		}
		return false
	}
	root := NewFlex(Vertical)
	root.Add(out, pane)
	h := startApp(t, root, 8, 8, WithMinFrameInterval(time.Hour))
	h.onLoop(func() { h.app.repairFocus() })
	h.sync()
	if got := focusedID(h); got != out.nodeID() {
		t.Fatalf("fixture: focus on %d, want out %d", got, out.nodeID())
	}
	h.inject(keyEv('/'), keyEv('a'), keyEv('b'))
	keys := func(p *focusProbe) int {
		n := 0
		for _, ev := range p.recorded() {
			if _, ok := ev.(KeyEvent); ok {
				n++
			}
		}
		return n
	}
	for deadline := time.Now().Add(3 * time.Second); keys(out)+keys(field) < 3; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the three keys were not all routed: out %d, field %d", keys(out), keys(field))
		}
	}
	var typed []rune
	for _, ev := range field.recorded() {
		if k, ok := ev.(KeyEvent); ok {
			typed = append(typed, k.Code)
		}
	}
	if string(typed) != "ab" {
		t.Fatalf("the shown field got %q, want \"ab\"; focus on %d (field %d)", string(typed), focusedID(h), field.nodeID())
	}
}

// Keys typed right after the one that grew a control from no cells (a bar a preference had
// hidden, laid out at zero size) and focused into it reach that control, though no frame runs
// between them: while a layout is pending, a control the last layout left with no cells is
// waiting for it, and the deferred focus settles before the next key is routed.
func TestKeysAfterAFocusIntoReachTheControlGrownThisTurn(t *testing.T) {
	t.Parallel()
	var shown atomic.Bool
	out := newFocusProbe("out", Size{W: 2, H: 1})
	bar := newFocusProbe("bar", Size{})
	bar.onLayout = func(_ *probe, c Constraints) Size {
		if shown.Load() {
			return c.Constrain(Size{W: 4, H: 1})
		}
		return c.Constrain(Size{})
	}
	out.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == '/' {
			shown.Store(true)
			bar.ctx.RequestLayout()
			p.ctx.FocusInto(bar)
			return true
		}
		return false
	}
	root := NewFlex(Vertical)
	root.Add(out, bar)
	h := startApp(t, root, 8, 8, WithMinFrameInterval(time.Hour))
	h.onLoop(func() { h.app.repairFocus() })
	h.sync()
	if got := focusedID(h); got != out.nodeID() {
		t.Fatalf("fixture: focus on %d, want out %d", got, out.nodeID())
	}
	h.inject(keyEv('/'), keyEv('a'), keyEv('b'))
	keys := func(p *focusProbe) []rune {
		var typed []rune
		for _, ev := range p.recorded() {
			if k, ok := ev.(KeyEvent); ok {
				typed = append(typed, k.Code)
			}
		}
		return typed
	}
	for deadline := time.Now().Add(3 * time.Second); len(keys(out))+len(keys(bar)) < 3; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the three keys were not all routed: out %q, bar %q", string(keys(out)), string(keys(bar)))
		}
	}
	if got := string(keys(bar)); got != "ab" {
		t.Fatalf("the grown bar got %q, want \"ab\" (out got %q); focus on %d (bar %d)", got, string(keys(out)), focusedID(h), bar.nodeID())
	}
}
