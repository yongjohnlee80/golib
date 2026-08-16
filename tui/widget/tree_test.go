package widget_test

// Tree contract (ADR-0008 §2.2): generation-tokened lazy loading where every
// outcome settles the spinner and stale results are inert; owner-stamped
// attachment; vim navigation over the flattened rows.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func focusedTree(t *testing.T, w, h int, opts ...widget.TreeOption) (*harness, *widget.Tree, *shell) {
	t.Helper()
	tr := widget.NewTree(opts...)
	sh := newShell(tr)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, tr, sh
}

func selectedID(h *harness, tr *widget.Tree) string {
	var id string
	h.onLoop(func() {
		if n, ok := tr.Selected(); ok {
			id = n.ID()
		}
	})
	return id
}

func TestTreeExpandLifecycle(t *testing.T) {
	root := widget.NewTreeNode("conns", "connections")
	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)

	// First expand fires exactly one request; a second expand while loading
	// fires nothing.
	h.inject(key('l'))
	h.inject(key('l'))
	h.barrier(sh)
	if reqs.count() != 1 {
		t.Fatalf("expand requests = %d, want 1", reqs.count())
	}
	gen := reqs.events()[0].Gen
	if gen == 0 {
		t.Fatal("request generation is 0")
	}

	// Settling with the CURRENT generation loads the children.
	h.onLoop(func() {
		root.SetChildren(gen, []*widget.TreeNode{
			widget.NewTreeNode("a", "alpha", widget.WithLeaf()),
			widget.NewTreeNode("b", "beta", widget.WithLeaf()),
		})
	})
	h.barrier(sh)
	h.wantContains("alpha")
	h.wantContains("beta")

	// j moves onto the first child.
	h.inject(key('j'))
	h.barrier(sh)
	if id := selectedID(h, tr); id != "a" {
		t.Fatalf("selected = %q, want a", id)
	}

	// h from a child jumps to the parent; h again collapses.
	h.inject(key('h'))
	h.barrier(sh)
	if id := selectedID(h, tr); id != "conns" {
		t.Fatalf("selected = %q, want conns", id)
	}
	collapses := record[widget.CollapseEvent](h)
	h.inject(key('h'))
	h.barrier(sh)
	if collapses.count() != 1 {
		t.Fatalf("collapse events = %d, want 1", collapses.count())
	}

	// Re-expanding a LOADED node fires no new request.
	h.inject(key('l'))
	h.barrier(sh)
	if reqs.count() != 1 {
		t.Fatalf("expand requests after re-expand = %d, want 1 (already loaded)", reqs.count())
	}
}

func TestTreeStaleGenerationsAreInert(t *testing.T) {
	root := widget.NewTreeNode("r", "root")
	h, _, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)

	h.inject(key('l')) // expand → gen 1 in flight
	h.barrier(sh)
	gen1 := reqs.events()[0].Gen

	// Collapse invalidates gen1; re-expand issues gen2.
	h.inject(key('h'))
	h.inject(key('l'))
	h.barrier(sh)
	if reqs.count() != 2 {
		t.Fatalf("requests = %d, want 2", reqs.count())
	}
	gen2 := reqs.events()[1].Gen

	// The stale gen1 result must be ignored entirely.
	h.onLoop(func() {
		root.SetChildren(gen1, []*widget.TreeNode{widget.NewTreeNode("stale", "STALE", widget.WithLeaf())})
	})
	h.barrier(sh)
	h.wantNotContains("STALE")

	// The current gen2 result lands.
	h.onLoop(func() {
		root.SetChildren(gen2, []*widget.TreeNode{widget.NewTreeNode("fresh", "FRESH", widget.WithLeaf())})
	})
	h.barrier(sh)
	h.wantContains("FRESH")
}

func TestTreeLoadErrorAndRetry(t *testing.T) {
	root := widget.NewTreeNode("r", "root")
	h, _, sh := focusedTree(t, 44, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)

	h.inject(key('l'))
	h.barrier(sh)
	gen1 := reqs.events()[0].Gen

	h.onLoop(func() { root.SetLoadError(gen1, "conn refused") })
	h.barrier(sh)
	h.wantContains("conn refused")

	// Retry is user-driven and fires a NEW generation.
	h.inject(key('l'))
	h.barrier(sh)
	if reqs.count() != 2 {
		t.Fatalf("requests = %d, want 2 after retry", reqs.count())
	}
	if reqs.events()[1].Gen == gen1 {
		t.Fatal("retry reused the failed generation")
	}
}

func TestTreeOwnershipPanics(t *testing.T) {
	// Attaching an owned node to a second tree panics.
	shared := widget.NewTreeNode("x", "x")
	widget.NewTree(widget.WithRoots(shared))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("cross-tree reuse did not panic")
			}
		}()
		widget.NewTree(widget.WithRoots(shared))
	}()

	// Duplicate sibling IDs panic.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("duplicate sibling ids did not panic")
			}
		}()
		widget.NewTree(widget.WithRoots(
			widget.NewTreeNode("dup", "a"), widget.NewTreeNode("dup", "b")))
	}()

	// Static pre-assembly (gen 0 on an unowned node) is allowed; attaching
	// the assembled subtree stamps every node, so re-attaching a descendant
	// panics.
	parent := widget.NewTreeNode("p", "parent")
	child := widget.NewTreeNode("c", "child", widget.WithLeaf())
	parent.SetChildren(0, []*widget.TreeNode{child})
	widget.NewTree(widget.WithRoots(parent))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("descendant re-attachment did not panic")
			}
		}()
		widget.NewTree(widget.WithRoots(child))
	}()
}

func TestTreeSetRootsReleasesAndLateResultsAreInert(t *testing.T) {
	rootA := widget.NewTreeNode("a", "rootA")
	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(rootA))
	reqs := record[widget.ExpandRequestEvent](h)

	h.inject(key('l')) // gen in flight on rootA
	h.barrier(sh)
	gen := reqs.events()[0].Gen

	// Replace the roots: rootA is released; its late result must be inert.
	rootB := widget.NewTreeNode("b", "rootB", widget.WithLeaf())
	h.onLoop(func() { tr.SetRoots(rootB) })
	h.barrier(sh)
	h.onLoop(func() {
		rootA.SetChildren(gen, []*widget.TreeNode{widget.NewTreeNode("ghost", "GHOST", widget.WithLeaf())})
	})
	h.barrier(sh)
	h.wantNotContains("GHOST")

	// A released node is re-attachable.
	h.onLoop(func() { tr.SetRoots(rootA) })
	h.barrier(sh)
	h.wantContains("rootA")
}

func TestTreeExpandPathAndActivate(t *testing.T) {
	ws := widget.NewTreeNode("ws", "workspace")
	conns := widget.NewTreeNode("conns", "connections")
	db := widget.NewTreeNode("db1", "maindb", widget.WithLeaf())
	conns.SetChildren(0, []*widget.TreeNode{db})
	ws.SetChildren(0, []*widget.TreeNode{conns})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(ws))
	acts := record[widget.ActivateEvent](h)

	h.onLoop(func() { tr.ExpandPath("ws", "conns", "db1") })
	h.barrier(sh)
	if id := selectedID(h, tr); id != "db1" {
		t.Fatalf("selected = %q, want db1", id)
	}
	h.inject(key(tui.KeyEnter)) // leaf activation
	h.barrier(sh)
	if acts.count() != 1 {
		t.Fatalf("activations = %d, want 1", acts.count())
	}
}

// --- implementation-review r1 regressions (2026-08-16) ---

// MF1: attachment validates the WHOLE incoming forest before any mutation —
// a bad forest leaves the existing tree fully intact.
func TestTreePreflightBeforeMutation(t *testing.T) {
	good := widget.NewTreeNode("good", "good", widget.WithLeaf())
	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(good))

	shared := widget.NewTreeNode("shared", "shared", widget.WithLeaf())
	a := widget.NewTreeNode("a", "a")
	b := widget.NewTreeNode("b", "b")
	a.SetChildren(0, []*widget.TreeNode{shared})

	h.onLoop(func() {
		defer func() {
			if recover() == nil {
				t.Error("shared descendant did not panic")
			}
		}()
		// b would share a's descendant: rejected BEFORE the old roots are
		// released.
		b2 := widget.NewTreeNode("b2", "b2")
		_ = b2
		tr.SetRoots(a, func() *widget.TreeNode {
			b.SetChildren(0, []*widget.TreeNode{shared}) // duplicate pointer
			return b
		}())
	})
	h.barrier(sh)
	// The original tree survived untouched.
	h.wantContains("good")
	if id := selectedID(h, tr); id != "good" {
		t.Fatalf("selected = %q, want good (tree intact)", id)
	}
}

// MF2: a released multi-level subtree keeps its internal ancestry and
// navigates correctly after re-attachment.
func TestTreeReattachedSubtreeKeepsAncestry(t *testing.T) {
	parent := widget.NewTreeNode("p", "parent")
	child := widget.NewTreeNode("c", "child")
	leaf := widget.NewTreeNode("l", "leaf", widget.WithLeaf())
	child.SetChildren(0, []*widget.TreeNode{leaf})
	parent.SetChildren(0, []*widget.TreeNode{child})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(parent))

	// Detach the whole assembly, then re-attach it.
	other := widget.NewTreeNode("o", "other", widget.WithLeaf())
	h.onLoop(func() { tr.SetRoots(other) })
	h.barrier(sh)
	h.onLoop(func() { tr.SetRoots(parent) })
	h.barrier(sh)

	// Expand down to the leaf and walk back up with h (parent links).
	h.onLoop(func() { tr.ExpandPath("p", "c", "l") })
	h.barrier(sh)
	if id := selectedID(h, tr); id != "l" {
		t.Fatalf("selected = %q, want l", id)
	}
	h.inject(key('h'))
	h.barrier(sh)
	if id := selectedID(h, tr); id != "c" {
		t.Fatalf("h from leaf = %q, want c (internal ancestry intact)", id)
	}
	h.inject(typeString("hh")...) // collapse c? no: first h collapses... c expanded → collapse; second h → parent
	h.barrier(sh)
	if id := selectedID(h, tr); id != "p" {
		t.Fatalf("walk up = %q, want p", id)
	}
}

// MF3: shrinking mutations reconcile the cursor synchronously — the next
// key operates on a real row, never a stale modulo-wrapped index.
func TestTreeCursorReconcileOnShrink(t *testing.T) {
	root := widget.NewTreeNode("r", "root")
	kids := []*widget.TreeNode{
		widget.NewTreeNode("k1", "kid-one", widget.WithLeaf()),
		widget.NewTreeNode("k2", "kid-two", widget.WithLeaf()),
		widget.NewTreeNode("k3", "kid-three", widget.WithLeaf()),
	}
	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)
	h.inject(key('l'))
	h.barrier(sh)
	h.onLoop(func() { root.SetChildren(reqs.events()[0].Gen, kids) })
	h.barrier(sh)

	// Cursor to the last row, then Reset shrinks the tree to one row.
	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	if id := selectedID(h, tr); id != "k3" {
		t.Fatalf("selected = %q, want k3", id)
	}
	h.onLoop(func() { root.Reset() })
	h.barrier(sh)
	if id := selectedID(h, tr); id != "r" {
		t.Fatalf("after shrink: selected = %q, want r (cursor reconciled)", id)
	}
	// The very next key must operate on the reconciled row.
	h.inject(key('l')) // expand root again → a NEW request
	h.barrier(sh)
	if reqs.count() != 2 {
		t.Fatalf("requests = %d, want 2 (key acted on the real row)", reqs.count())
	}
}
