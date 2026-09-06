package tui

// Boundaries found by an implementation review, verified here before
// being fixed (challenge-review-verdicts).

import "testing"

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
