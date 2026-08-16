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
