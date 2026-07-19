package tui

// Focus tests: Tab traversal with wrap and skips, trapping scopes, restore
// on scope unmount, repair on focused-node unmount (ADR-0004 §5.4).

import (
	"testing"
)

// focusedID reads the loop-owned focus state safely.
func focusedID(h *harness) NodeID {
	var id NodeID
	h.onLoop(func() { id = h.app.focused })
	return id
}

// TestTabTraversalWrapsAndSkips: ADR-0004 §5.4 — Tab from the last
// focusable wraps to the first; AcceptsFocus()==false and zero-Rect nodes
// are skipped.
func TestTabTraversalWrapsAndSkips(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	b.accepts.Store(false) // temporarily unfocusable: skipped
	c := newFocusProbe("c", Size{W: 2, H: 1})
	d := newFocusProbe("d", Size{}) // zero-Rect: not a tab stop
	root := NewFlex(Vertical)
	root.Add(a, b, c, d)
	h := startApp(t, root, 8, 8)

	h.inject(tabEv(false)) // nothing focused → first stop
	waitFor(t, "focus a", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(tabEv(false)) // b skipped (AcceptsFocus false)
	waitFor(t, "focus c", func() bool { return focusedID(h) == c.nodeID() })
	h.inject(tabEv(false)) // d skipped (zero rect) → wraps to a
	waitFor(t, "wrap to a", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(tabEv(true)) // Shift-Tab wraps backward to c
	waitFor(t, "shift-tab to c", func() bool { return focusedID(h) == c.nodeID() })
}

// TestFocusComponent: cross-node programmatic focus — a controller hands focus
// to another mounted component, not the calling node (RequestFocus is self).
func TestFocusComponent(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 8)

	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	h.onLoop(func() { a.ctx.FocusComponent(b) }) // focus a different node
	waitFor(t, "b focused via FocusComponent", func() bool { return focusedID(h) == b.nodeID() })
}

// TestFocusEventsAndBubble: every move delivers Gained=false to the loser
// then Gained=true to the gainer, and FocusEvents bubble so ancestors can
// restyle (ADR-0004 §2.6.1, §2.5.3).
func TestFocusEventsAndBubble(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	seen := &callLog{}
	record := func(name string) func(*probe, Event) bool {
		return func(_ *probe, ev Event) bool {
			if f, ok := ev.(FocusEvent); ok {
				if f.Gained {
					seen.add(name + ".gained")
				} else {
					seen.add(name + ".lost")
				}
			}
			return false
		}
	}
	a.onEvent = record("a")
	b.onEvent = record("b")
	root := NewFlex(Vertical)
	root.Add(a, b)

	h := startApp(t, root, 8, 4)
	h.inject(tabEv(false))
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	h.inject(tabEv(false))
	waitFor(t, "b focused", func() bool { return focusedID(h) == b.nodeID() })

	got := seen.get()
	want := []string{"a.gained", "a.lost", "b.gained"}
	if len(got) != len(want) {
		t.Fatalf("focus events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("focus events = %v, want %v (loser first, then gainer)", got, want)
		}
	}
}

// TestFocusScopeTrapsAndRestores: ADR-0004 §5.4 — a trapping FocusScope
// confines Tab within its subtree; closing the scope restores focus to the
// pre-modal node.
func TestFocusScopeTrapsAndRestores(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	m1 := newFocusProbe("m1", Size{W: 2, H: 1})
	m2 := newFocusProbe("m2", Size{W: 2, H: 1})
	modal := newScopeProbe(true)
	modal.Add(m1, m2)
	root := NewFlex(Vertical)
	root.Add(a, modal)
	h := startApp(t, root, 8, 8)

	// Focus a, then enter the modal via RequestFocus (pushes the scope).
	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	h.onLoop(func() { m1.ctx.RequestFocus() })
	waitFor(t, "m1 focused", func() bool { return focusedID(h) == m1.nodeID() })

	// Tab cycles inside the trap and cannot escape to a.
	h.inject(tabEv(false))
	waitFor(t, "m2 focused", func() bool { return focusedID(h) == m2.nodeID() })
	h.inject(tabEv(false))
	waitFor(t, "trapped wrap to m1", func() bool { return focusedID(h) == m1.nodeID() })

	// Closing the scope restores focus to the pre-modal node.
	h.onLoop(func() { root.Remove(modal) })
	waitFor(t, "focus restored to a", func() bool { return focusedID(h) == a.nodeID() })
}

// TestTrapExcludedFromOuterRing: an open (unfocused) trap's stops are not
// part of the enclosing scope's Tab ring.
func TestTrapExcludedFromOuterRing(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	m1 := newFocusProbe("m1", Size{W: 2, H: 1})
	modal := newScopeProbe(true)
	modal.Add(m1)
	root := NewFlex(Vertical)
	root.Add(a, modal, b)
	h := startApp(t, root, 8, 8)

	h.inject(tabEv(false), tabEv(false)) // a → b (m1 is inside a trap)
	waitFor(t, "b focused, m1 skipped", func() bool { return focusedID(h) == b.nodeID() })
}

// TestUnmountFocusedRepairsFocus: ADR-0004 §5.4 — unmounting the focused
// node repairs focus within the same frame (no dangling ID).
func TestUnmountFocusedRepairsFocus(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 4)

	h.onLoop(func() { b.ctx.RequestFocus() })
	waitFor(t, "b focused", func() bool { return focusedID(h) == b.nodeID() })
	var repaired NodeID
	h.onLoop(func() {
		root.Remove(b)
		repaired = h.app.focused // observed INSIDE the same drain — same frame
	})
	if repaired != a.nodeID() {
		t.Fatalf("focus after unmount = %d, want repaired to a (%d) within the cascade", repaired, a.nodeID())
	}
}

// TestRequestFocusIgnoredForNonFocusable: RequestFocus on a non-Focusable
// component is a no-op.
func TestRequestFocusIgnoredForNonFocusable(t *testing.T) {
	t.Parallel()
	plain := &probe{name: "plain", pref: Size{W: 2, H: 1}}
	root := NewFlex(Vertical)
	root.Add(plain)
	h := startApp(t, root, 8, 4)
	h.onLoop(func() { plain.ctx.RequestFocus() })
	if got := focusedID(h); got != 0 {
		t.Fatalf("focused = %d after RequestFocus on non-Focusable, want 0", got)
	}
}
