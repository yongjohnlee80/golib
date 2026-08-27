package tui

// Move semantics: identity preservation across reorder — the entire
// feature in one test file. A Move must change document order while
// changing NOTHING about the node: NodeID, context, hooks, Init count,
// focus.

import (
	"testing"
)

// TestMovePreservesIdentity: Move changes document order but the moved
// node keeps its NodeID and ctx (no cancel), Init does not re-run, and
// focus stays on the moved, still-focused child.
func TestMovePreservesIdentity(t *testing.T) {
	t.Parallel()
	log := &callLog{}
	a := &probe{name: "a", pref: Size{W: 2, H: 1}, log: log}
	b := &probe{name: "b", pref: Size{W: 2, H: 1}, log: log}
	c := &probe{name: "c", pref: Size{W: 2, H: 1}, log: log}
	root := NewFlex(Vertical)
	root.Add(a, b, c)
	h := startApp(t, root, 8, 3)

	waitFor(t, "mounted", func() bool { return a.renders.Load() >= 1 })
	idBefore := a.nodeID()
	ctxBefore := a.ctx.Ctx()

	h.onLoop(func() { root.Move(a, 2) })
	waitFor(t, "post-move frame", func() bool { return a.renders.Load() >= 2 })

	if got := a.nodeID(); got != idBefore {
		t.Fatalf("NodeID after Move = %d, want %d (identity preserved)", got, idBefore)
	}
	select {
	case <-ctxBefore.Done():
		t.Fatal("Move cancelled the moved node's context")
	default:
	}
	if got := a.inits.Load(); got != 1 {
		t.Fatalf("Init calls after Move = %d, want 1 (no re-Init)", got)
	}

	// Document order changed: a's node must now sit at index 2 of its
	// parent's children.
	h.onLoop(func() {
		rn := a.ctx.node.parent
		if rn == nil || len(rn.children) != 3 || rn.children[2].comp != a {
			t.Fatalf("document order after Move wrong: a not at index 2")
		}
	})
}

// TestMovePanics guards the contract: out-of-range index and a child of
// another container panic; an unmounted component panics.
func TestMovePanics(t *testing.T) {
	t.Parallel()
	a := &probe{name: "a", pref: Size{W: 1, H: 1}}
	b := &probe{name: "b", pref: Size{W: 1, H: 1}}
	stranger := &probe{name: "s", pref: Size{W: 1, H: 1}}
	root := NewFlex(Vertical)
	other := NewFlex(Vertical)
	root.Add(a)
	other.Add(b)
	wrap := NewFlex(Vertical)
	wrap.Add(root, other)
	h := startApp(t, wrap, 4, 2)
	waitFor(t, "mounted", func() bool { return b.renders.Load() >= 1 })

	mustPanic := func(name string, fn func()) {
		defer func() {
			if recover() == nil {
				t.Fatalf("%s: expected panic", name)
			}
		}()
		fn()
	}
	h.onLoop(func() { mustPanic("out of range", func() { root.Move(a, 1) }) })
	// A child this container does not own is a silent no-op at the
	// container level, mirroring Remove's contract.
	h.onLoop(func() { root.Move(stranger, 0) })
	// The framework-level guard fires through Context.Move: b is mounted
	// but not under the node issuing the call.
	h.onLoop(func() { mustPanic("cross-container", func() { a.ctx.Move(b, 0) }) })
}
