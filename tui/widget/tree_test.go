package widget_test

// Tree contract: generation-tokened lazy loading where every
// outcome settles the spinner and stale results are inert; owner-stamped
// attachment; vim navigation over the flattened rows.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/files"
)

func focusedTree(t *testing.T, w, h int, opts ...files.TreeOption) (*harness, *files.Tree, *shell) {
	t.Helper()
	tr := files.NewTree(opts...)
	sh := newShell(tr)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, tr, sh
}

func selectedID(h *harness, tr *files.Tree) string {
	var id string
	h.onLoop(func() {
		if n, ok := tr.Selected(); ok {
			id = n.ID()
		}
	})
	return id
}

func TestTreeExpandLifecycle(t *testing.T) {
	root := files.NewTreeNode("conns", "connections")
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(root))
	reqs := record[files.ExpandRequestEvent](h)

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
		root.SetChildren(gen, []*files.TreeNode{
			files.NewTreeNode("a", "alpha", files.WithLeaf()),
			files.NewTreeNode("b", "beta", files.WithLeaf()),
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
	collapses := record[files.CollapseEvent](h)
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
	root := files.NewTreeNode("r", "root")
	h, _, sh := focusedTree(t, 40, 10, files.WithRoots(root))
	reqs := record[files.ExpandRequestEvent](h)

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
		root.SetChildren(gen1, []*files.TreeNode{files.NewTreeNode("stale", "STALE", files.WithLeaf())})
	})
	h.barrier(sh)
	h.wantNotContains("STALE")

	// The current gen2 result lands.
	h.onLoop(func() {
		root.SetChildren(gen2, []*files.TreeNode{files.NewTreeNode("fresh", "FRESH", files.WithLeaf())})
	})
	h.barrier(sh)
	h.wantContains("FRESH")
}

func TestTreeLoadErrorAndRetry(t *testing.T) {
	root := files.NewTreeNode("r", "root")
	h, _, sh := focusedTree(t, 44, 10, files.WithRoots(root))
	reqs := record[files.ExpandRequestEvent](h)

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
	shared := files.NewTreeNode("x", "x")
	files.NewTree(files.WithRoots(shared))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("cross-tree reuse did not panic")
			}
		}()
		files.NewTree(files.WithRoots(shared))
	}()

	// Duplicate sibling IDs panic.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("duplicate sibling ids did not panic")
			}
		}()
		files.NewTree(files.WithRoots(
			files.NewTreeNode("dup", "a"), files.NewTreeNode("dup", "b")))
	}()

	// Static pre-assembly (gen 0 on an unowned node) is allowed; attaching
	// the assembled subtree stamps every node, so re-attaching a descendant
	// panics.
	parent := files.NewTreeNode("p", "parent")
	child := files.NewTreeNode("c", "child", files.WithLeaf())
	parent.SetChildren(0, []*files.TreeNode{child})
	files.NewTree(files.WithRoots(parent))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("descendant re-attachment did not panic")
			}
		}()
		files.NewTree(files.WithRoots(child))
	}()
}

func TestTreeSetRootsReleasesAndLateResultsAreInert(t *testing.T) {
	rootA := files.NewTreeNode("a", "rootA")
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(rootA))
	reqs := record[files.ExpandRequestEvent](h)

	h.inject(key('l')) // gen in flight on rootA
	h.barrier(sh)
	gen := reqs.events()[0].Gen

	// Replace the roots: rootA is released; its late result must be inert.
	rootB := files.NewTreeNode("b", "rootB", files.WithLeaf())
	h.onLoop(func() { tr.SetRoots(rootB) })
	h.barrier(sh)
	h.onLoop(func() {
		rootA.SetChildren(gen, []*files.TreeNode{files.NewTreeNode("ghost", "GHOST", files.WithLeaf())})
	})
	h.barrier(sh)
	h.wantNotContains("GHOST")

	// A released node is re-attachable.
	h.onLoop(func() { tr.SetRoots(rootA) })
	h.barrier(sh)
	h.wantContains("rootA")
}

func TestTreeExpandPathAndActivate(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	conns := files.NewTreeNode("conns", "connections")
	db := files.NewTreeNode("db1", "maindb", files.WithLeaf())
	conns.SetChildren(0, []*files.TreeNode{db})
	ws.SetChildren(0, []*files.TreeNode{conns})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)

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

// Attachment validates the WHOLE incoming forest before any mutation —
// a bad forest leaves the existing tree fully intact.
func TestTreePreflightBeforeMutation(t *testing.T) {
	good := files.NewTreeNode("good", "good", files.WithLeaf())
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(good))

	shared := files.NewTreeNode("shared", "shared", files.WithLeaf())
	a := files.NewTreeNode("a", "a")
	b := files.NewTreeNode("b", "b")
	a.SetChildren(0, []*files.TreeNode{shared})

	h.onLoop(func() {
		defer func() {
			if recover() == nil {
				t.Error("shared descendant did not panic")
			}
		}()
		// b would share a's descendant: rejected BEFORE the old roots are
		// released.
		b2 := files.NewTreeNode("b2", "b2")
		_ = b2
		tr.SetRoots(a, func() *files.TreeNode {
			b.SetChildren(0, []*files.TreeNode{shared}) // duplicate pointer
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

// A released multi-level subtree keeps its internal ancestry and
// navigates correctly after re-attachment.
func TestTreeReattachedSubtreeKeepsAncestry(t *testing.T) {
	parent := files.NewTreeNode("p", "parent")
	child := files.NewTreeNode("c", "child")
	leaf := files.NewTreeNode("l", "leaf", files.WithLeaf())
	child.SetChildren(0, []*files.TreeNode{leaf})
	parent.SetChildren(0, []*files.TreeNode{child})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(parent))

	// Detach the whole assembly, then re-attach it.
	other := files.NewTreeNode("o", "other", files.WithLeaf())
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

// Shrinking mutations reconcile the cursor synchronously — the next
// key operates on a real row, never a stale modulo-wrapped index.
func TestTreeCursorReconcileOnShrink(t *testing.T) {
	root := files.NewTreeNode("r", "root")
	kids := []*files.TreeNode{
		files.NewTreeNode("k1", "kid-one", files.WithLeaf()),
		files.NewTreeNode("k2", "kid-two", files.WithLeaf()),
		files.NewTreeNode("k3", "kid-three", files.WithLeaf()),
	}
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(root))
	reqs := record[files.ExpandRequestEvent](h)
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

// r2 residual: the receiver (and its ancestors) are rejected anywhere in
// the incoming forest — n.SetChildren(0, n) and longer receiver cycles
// fail BEFORE commit.
func TestTreeReceiverCycleRejected(t *testing.T) {
	n := files.NewTreeNode("n", "n")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("self-adoption did not panic")
			}
		}()
		n.SetChildren(0, []*files.TreeNode{n})
	}()

	root := files.NewTreeNode("root", "root")
	child := files.NewTreeNode("child", "child")
	root.SetChildren(0, []*files.TreeNode{child})
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("receiver-ancestor cycle did not panic")
			}
		}()
		child.SetChildren(0, []*files.TreeNode{root})
	}()
	// The graph is untouched: root still parents child, no cycle.
	tr := files.NewTree(files.WithRoots(root))
	_ = tr
}

// Ctrl-l/Ctrl-h are pane motion in vim-keyed hosts; the tree must not
// read them as its own expand/collapse letters.
func TestTreeIgnoresApplicationChords(t *testing.T) {
	root := files.NewTreeNode("conns", "connections")
	h, _, sh := focusedTree(t, 40, 10, files.WithRoots(root))
	reqs := record[files.ExpandRequestEvent](h)

	h.inject(keyMod('l', tui.ModCtrl))
	h.barrier(sh)
	if got := reqs.count(); got != 0 {
		t.Fatalf("Ctrl-l must bubble, not expand: %d expand request(s)", got)
	}
	// The bare letter still expands.
	h.inject(key('l'))
	h.barrier(sh)
	if got := reqs.count(); got != 1 {
		t.Fatalf("bare l should expand: %d expand request(s)", got)
	}
}

func TestTreeHostControls(t *testing.T) {
	root := files.NewTreeNode("ws", "workspace")
	root.SetChildren(0, []*files.TreeNode{
		files.NewTreeNode("a", "alpha", files.WithLeaf()),
		files.NewTreeNode("b", "beta", files.WithLeaf()),
	})
	th, tr, tsh := focusedTree(t, 40, 10, files.WithRoots(root))
	th.inject(key('l')) // expand the pre-assembled root
	th.barrier(tsh)

	var labels []string
	var cur int
	th.onLoop(func() {
		tr.SetStyles(core.ListStyles{CursorRow: style.New().Background(style.ANSI(8))})
		for _, n := range tr.VisibleRows() {
			labels = append(labels, n.Label())
		}
		tr.SetCursor(2)
		cur = tr.Cursor()
	})
	th.barrier(tsh)
	if len(labels) != 3 || labels[0] != "workspace" || labels[2] != "beta" {
		t.Fatalf("VisibleRows/Label: %v", labels)
	}
	if cur != 2 {
		t.Fatalf("Tree.SetCursor: cursor = %d, want 2", cur)
	}
	if id := selectedID(th, tr); id != "b" {
		t.Fatalf("cursor should sit on beta, got %q", id)
	}
}

// Tree.Reload refreshes a subtree in place: an expanded node re-requests
// its children under a new generation, the cursor stays put, and a
// collapsed node simply drops what it cached.
func TestTreeReloadSubtree(t *testing.T) {
	root := files.NewTreeNode("ws", "workspace")
	notes := files.NewTreeNode("notes", "notes")
	root.SetChildren(0, []*files.TreeNode{notes})
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(root))
	reqs := record[files.ExpandRequestEvent](h)

	h.inject(key('l'), key('j'), key('l')) // expand ws, onto notes, expand it
	h.barrier(sh)
	if reqs.count() != 1 {
		t.Fatalf("expected one expand request, got %d", reqs.count())
	}
	ev, _ := reqs.last()
	h.onLoop(func() {
		ev.Node.SetChildren(ev.Gen, []*files.TreeNode{
			files.NewTreeNode("a", "a.sql", files.WithLeaf()),
		})
	})
	h.barrier(sh)
	h.wantContains("a.sql")

	before := selectedID(h, tr)
	var ok bool
	h.onLoop(func() { ok = tr.Reload("notes") })
	h.barrier(sh)
	if !ok {
		t.Fatal("Reload reported no such node")
	}
	if reqs.count() != 2 {
		t.Fatalf("an expanded node should re-request: %d requests", reqs.count())
	}
	if after := selectedID(h, tr); after != before {
		t.Fatalf("Reload moved the cursor: %q → %q", before, after)
	}
	// The new generation differs, so the stale load cannot install.
	ev2, _ := reqs.last()
	if ev2.Gen == ev.Gen {
		t.Fatalf("Reload reused generation %d", ev.Gen)
	}
	h.onLoop(func() {
		ev.Node.SetChildren(ev.Gen, []*files.TreeNode{
			files.NewTreeNode("stale", "stale.sql", files.WithLeaf()),
		})
	})
	h.barrier(sh)
	h.wantNotContains("stale.sql")
}

// singlePress is one press. A test CANNOT fake a double-click by setting Count:
// dispatch recomputes it for every press, precisely so a component can trust it
// and no producer can forge it. A double-click is therefore two real presses on
// the same cell inside the window (the harness uses the 400ms default).
func singlePress(y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: y}
}

// A double-click on a BRANCH activates it and leaves its expanded
// state alone.
func TestTreeDoubleClickActivatesABranchWithoutExpanding(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	tbl := files.NewTreeNode("tbl", "a_table") // a BRANCH: children are columns
	col := files.NewTreeNode("col", "id", files.WithLeaf())
	tbl.SetChildren(0, []*files.TreeNode{col})
	ws.SetChildren(0, []*files.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)

	h.onLoop(func() { tr.ExpandPath("ws") })
	h.barrier(sh)

	var rowsBefore int
	h.onLoop(func() { rowsBefore = len(tr.VisibleRows()) })

	// Row 1 is the table under the expanded workspace.
	h.inject(singlePress(1), singlePress(1))
	h.barrier(sh)

	var rowsAfter int
	h.onLoop(func() { rowsAfter = len(tr.VisibleRows()) })

	if acts.count() != 1 {
		t.Errorf("activations = %d, want 1 — a double-click must activate a branch", acts.count())
	}
	if rowsAfter != rowsBefore {
		t.Errorf("visible rows %d → %d: the Tree expanded on double-click, which it "+
			"must not do — the host cannot then distinguish activate from expand",
			rowsBefore, rowsAfter)
	}
	if id := selectedID(h, tr); id != "tbl" {
		t.Errorf("selected = %q, want tbl — activation targets the row under the pointer", id)
	}
}

// A SINGLE click selects and publishes nothing.
func TestTreeSingleClickDoesNotActivate(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	tbl := files.NewTreeNode("tbl", "a_table")
	ws.SetChildren(0, []*files.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)

	h.onLoop(func() { tr.ExpandPath("ws") })
	h.barrier(sh)

	h.inject(singlePress(1))
	h.barrier(sh)

	if acts.count() != 0 {
		t.Errorf("activations = %d, want 0 — a single click only selects", acts.count())
	}
	if id := selectedID(h, tr); id != "tbl" {
		t.Errorf("selected = %q, want tbl", id)
	}
}

// A leaf double-click activates too, on the same channel as ENTER — so a host
// that already handles leaf activation needs no new code.
func TestTreeDoubleClickActivatesALeaf(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	leaf := files.NewTreeNode("note", "query.sql", files.WithLeaf())
	ws.SetChildren(0, []*files.TreeNode{leaf})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)
	h.onLoop(func() { tr.ExpandPath("ws") })
	h.barrier(sh)

	h.inject(singlePress(1), singlePress(1))
	h.barrier(sh)

	if acts.count() != 1 {
		t.Errorf("activations = %d, want 1", acts.count())
	}
}

func TestTreeTripleClickActivatesOnce(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	tbl := files.NewTreeNode("tbl", "a_table")
	ws.SetChildren(0, []*files.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)
	h.onLoop(func() { tr.ExpandPath("ws") })
	h.barrier(sh)

	h.inject(singlePress(1), singlePress(1), singlePress(1))
	h.barrier(sh)

	if n := acts.count(); n != 1 {
		t.Errorf("activations from a triple-click = %d, want exactly 1", n)
	}
}

// Design note — the EXPANDER is not an activation target. Its press already
// toggles, so activating there too would make one gesture both change expansion
// and activate. A double-click on the expander toggles twice: no activation, and
// the node ends as it started.
func TestTreeDoubleClickOnTheExpanderTogglesTwiceAndDoesNotActivate(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	child := files.NewTreeNode("child", "a_child", files.WithLeaf())
	ws.SetChildren(0, []*files.TreeNode{child})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)

	var rowsBefore int
	h.onLoop(func() { rowsBefore = len(tr.VisibleRows()) })

	// x=0 is the expander column of a depth-0 row.
	expander := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 0}
	h.inject(expander, expander)
	h.barrier(sh)

	var rowsAfter int
	h.onLoop(func() { rowsAfter = len(tr.VisibleRows()) })

	if n := acts.count(); n != 0 {
		t.Errorf("activations = %d, want 0 — the expander toggles, it does not activate", n)
	}
	if rowsAfter != rowsBefore {
		t.Errorf("visible rows %d → %d: two toggles must leave the node as it started",
			rowsBefore, rowsAfter)
	}
}

// A REJECTED SetRoots must leave the Tree untouched.
// The pairing reset used to run before preflightForest, so a call that
// then panicked had already mutated pairing state — a failed operation with a
// side effect.
func TestTreeRejectedSetRootsLeavesPairingIntact(t *testing.T) {
	a := files.NewTreeNode("a", "a")
	child := files.NewTreeNode("child", "child", files.WithLeaf())
	a.SetChildren(0, []*files.TreeNode{child})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(a))
	acts := record[core.ActivateEvent](h)
	h.onLoop(func() { tr.ExpandPath("a") })
	h.barrier(sh)

	// First press of a pair, on the child row.
	h.inject(singlePress(1))
	h.barrier(sh)

	// A forest naming the same node twice is rejected.
	dup := files.NewTreeNode("dup", "dup")
	h.onLoop(func() {
		defer func() { _ = recover() }()
		tr.SetRoots(dup, dup)
	})
	h.barrier(sh)

	// The rejected call changed nothing, so the pair still completes.
	h.inject(singlePress(1))
	h.barrier(sh)

	if n := acts.count(); n != 1 {
		t.Errorf("activations = %d, want 1 — a REJECTED SetRoots must not clear "+
			"pairing state; a failed call had a side effect", n)
	}
}

// Enter activates a branch as it activates a leaf — Qt's item views emit
// activated on Enter for any row — and leaves its expansion alone: the host
// decides whether the row is a folder to open or a thing to use.
func TestTreeEnterActivatesABranchWithoutExpanding(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	tbl := files.NewTreeNode("tbl", "a_table") // a BRANCH: children are columns
	tbl.SetChildren(0, []*files.TreeNode{files.NewTreeNode("col", "id", files.WithLeaf())})
	ws.SetChildren(0, []*files.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	acts := record[core.ActivateEvent](h)
	h.onLoop(func() { tr.ExpandPath("ws", "tbl") }) // the cursor on the table, closed
	h.barrier(sh)
	var before int
	h.onLoop(func() { before = len(tr.VisibleRows()) })

	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	var after int
	h.onLoop(func() { after = len(tr.VisibleRows()) })
	if acts.count() != 1 {
		t.Errorf("activations = %d, want 1 — Enter activates a branch", acts.count())
	}
	if after != before {
		t.Errorf("visible rows %d → %d: Enter expanded the branch; opening is l, or the host's toggleExpanded", before, after)
	}
}

// ToggleExpanded opens a closed branch and closes an open one, and leaves a
// leaf alone.
func TestTreeToggleExpandedOpensAndClosesABranch(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	leaf := files.NewTreeNode("col", "id", files.WithLeaf())
	ws.SetChildren(0, []*files.TreeNode{leaf})
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))

	rows := func() int {
		var n int
		h.onLoop(func() { n = len(tr.VisibleRows()) })
		return n
	}
	h.onLoop(func() { tr.ToggleExpanded(ws) })
	h.barrier(sh)
	if n := rows(); n != 2 {
		t.Fatalf("after one toggle %d rows are shown, want 2 (the branch opened)", n)
	}
	h.onLoop(func() { tr.ToggleExpanded(leaf) })
	h.barrier(sh)
	if n := rows(); n != 2 {
		t.Errorf("toggling a leaf changed the rows shown to %d", n)
	}
	h.onLoop(func() { tr.ToggleExpanded(ws) })
	h.barrier(sh)
	if n := rows(); n != 1 {
		t.Errorf("after a second toggle %d rows are shown, want 1 (the branch closed)", n)
	}
}

// ToggleExpanded on a node of another tree, or on nil, changes nothing here.
func TestTreeToggleExpandedIgnoresANodeItDoesNotHold(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	ws.SetChildren(0, []*files.TreeNode{files.NewTreeNode("col", "id", files.WithLeaf())})
	other := files.NewTreeNode("other", "elsewhere")
	other.SetChildren(0, []*files.TreeNode{files.NewTreeNode("x", "x", files.WithLeaf())})
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(ws))
	h.onLoop(func() {
		tr.ToggleExpanded(other)
		tr.ToggleExpanded(nil)
	})
	h.barrier(sh)
	var n int
	h.onLoop(func() { n = len(tr.VisibleRows()) })
	if n != 1 {
		t.Errorf("%d rows shown after toggling nodes the tree does not hold, want 1", n)
	}
}

// TestTreeReloadReopensWhatWasOpen: a Reload of an open node, its children
// loaded lazily, opens again the descendants that were open — each asked for
// as its parent's fresh children arrive — and puts the cursor back on its
// node; a node gone from the fresh children is not asked for.
func TestTreeReloadReopensWhatWasOpen(t *testing.T) {
	ws := files.NewTreeNode("ws", "workspace")
	h, tr, sh := focusedTree(t, 40, 12, files.WithRoots(ws))
	reqs := record[files.ExpandRequestEvent](h)
	answer := func(want string, kids ...*files.TreeNode) {
		t.Helper()
		ev, ok := reqs.last()
		if !ok || ev.Node.ID() != want {
			t.Fatalf("the last request is for %v, want %s", ev.Node, want)
		}
		h.onLoop(func() { ev.Node.SetChildren(ev.Gen, kids) })
		h.barrier(sh)
	}
	leaf := func(id string) *files.TreeNode { return files.NewTreeNode(id, id, files.WithLeaf()) }
	h.inject(key('l')) // ws
	h.barrier(sh)
	answer("ws", files.NewTreeNode("dir", "dir"), files.NewTreeNode("old", "old"), leaf("a"))
	h.inject(key('j'), key('l')) // dir
	h.barrier(sh)
	answer("dir", leaf("b"), leaf("c"))
	h.inject(key('j'), key('j')) // onto c
	h.barrier(sh)
	h.inject(key('j'), key('l')) // old, open
	h.barrier(sh)
	answer("old", leaf("x"))
	h.inject(key('k')) // back onto c
	h.barrier(sh)
	if got := selectedID(h, tr); got != "c" {
		t.Fatalf("fixture: the cursor is on %q, want c", got)
	}
	n := reqs.count()
	h.onLoop(func() { tr.Reload("ws") })
	h.barrier(sh)
	answer("ws", files.NewTreeNode("dir", "dir"), leaf("new"), leaf("a")) // old is gone
	answer("dir", leaf("b"), leaf("c"))                                   // asked for, as it was open
	if got := reqs.count() - n; got != 2 {
		t.Fatalf("%d requests after the reload, want 2 (ws, then dir; not the vanished old)", got)
	}
	h.wantContains("new")
	h.wantContains("c")
	if got := selectedID(h, tr); got != "c" {
		t.Fatalf("after the reload the cursor is on %q, want c", got)
	}
}

// TestTreeKnownChildrenShowInTheExpandingKeysHandling: with WithKnownChildren, an unloaded node
// whose children the host has opens with them in the key's own handling, so the next key, handled
// before anything published is, finds them: l then j land on the first child, with no request
// sent. A node the host cannot answer for at once still sends its request.
func TestTreeKnownChildrenShowInTheExpandingKeysHandling(t *testing.T) {
	db := files.NewTreeNode("db", "database")
	lazy := files.NewTreeNode("lazy", "remote")
	known := func(n *files.TreeNode, gen uint64) bool {
		if n != db {
			return false
		}
		n.SetChildren(gen, []*files.TreeNode{
			files.NewTreeNode("a", "alpha", files.WithLeaf()),
			files.NewTreeNode("b", "beta", files.WithLeaf()),
		})
		return true
	}
	h, tr, sh := focusedTree(t, 40, 10, files.WithRoots(db, lazy), files.WithKnownChildren(known))
	reqs := record[files.ExpandRequestEvent](h)
	h.onLoop(func() { // one turn: nothing published is handled between the two keys
		tr.HandleEvent(key('l'))
		tr.HandleEvent(key('j'))
	})
	h.barrier(sh)
	if id := selectedID(h, tr); id != "a" {
		t.Fatalf("l j selected %q, want a, the first child", id)
	}
	if reqs.count() != 0 {
		t.Fatalf("expand requests = %d, want none for children the host had", reqs.count())
	}
	h.inject(key('G'), key('l')) // the last row, lazy: the host has nothing for it yet
	h.barrier(sh)
	if reqs.count() != 1 {
		t.Fatalf("expand requests for lazy = %d, want 1", reqs.count())
	}
}

// TestATreeTellsItsOwnerAtOnce: the owner's callbacks hear the cursor move, an expansion, the
// request for a branch's children, and an activation, each once, with the node, as the key is
// handled (data-I4, as List's).
func TestATreeTellsItsOwnerAtOnce(t *testing.T) {
	branch := files.NewTreeNode("b", "branch")
	leaf := files.NewTreeNode("l", "leaf", files.WithLeaf())
	var cursor, expanded, requested, activated []string
	h, _, sh := focusedTree(t, 40, 6, files.WithRoots(branch, leaf),
		files.WithTreeOnCursor(func(n *files.TreeNode) { cursor = append(cursor, n.ID()) }),
		files.WithTreeOnExpand(func(n *files.TreeNode) { expanded = append(expanded, n.ID()) }),
		files.WithTreeOnExpandRequest(func(n *files.TreeNode, _ uint64) { requested = append(requested, n.ID()) }),
		files.WithTreeOnActivate(func(n *files.TreeNode) { activated = append(activated, n.ID()) }))
	h.inject(key('l')) // open the branch: its children are asked for
	h.barrier(sh)
	h.inject(key(tui.KeyDown), key(tui.KeyEnter))
	h.barrier(sh)
	var c, e, r, a []string
	h.onLoop(func() { c, e, r, a = cursor, expanded, requested, activated })
	if len(e) != 1 || e[0] != "b" || len(r) != 1 || r[0] != "b" {
		t.Errorf("expand %v, request %v, want each [b] once", e, r)
	}
	if len(c) == 0 || c[len(c)-1] != "l" {
		t.Errorf("cursor %v, want it to end on l", c)
	}
	if len(a) != 1 || a[0] != "l" {
		t.Errorf("activate %v, want [l]", a)
	}
}
