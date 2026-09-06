package widget_test

// Tree double-click (ADR-0010 §2.5, criteria 26-27).
//
// A double-click is activation, published on the same channel ENTER already
// uses, so a host that already handles ActivateEvent gains double-click without
// changing. It fires for BRANCHES too, and must NOT also toggle expansion: the
// Tree cannot know whether a branch is activatable, and a host like autodb has
// branches (`tbl:` nodes, whose children are columns) whose activation is a
// query scaffold. A Tree that guessed would scaffold AND expand.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// singlePress is one press. A test CANNOT fake a double-click by setting Count:
// dispatch recomputes it for every press, precisely so a component can trust it
// and no producer can forge it. A double-click is therefore two real presses on
// the same cell inside the window (the harness uses the 400ms default).
func singlePress(y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: y}
}

// Criterion 26 — a double-click on a BRANCH activates it and leaves its expanded
// state alone.
func TestTreeDoubleClickActivatesABranchWithoutExpanding(t *testing.T) {
	ws := widget.NewTreeNode("ws", "workspace")
	tbl := widget.NewTreeNode("tbl", "a_table") // a BRANCH: children are columns
	col := widget.NewTreeNode("col", "id", widget.WithLeaf())
	tbl.SetChildren(0, []*widget.TreeNode{col})
	ws.SetChildren(0, []*widget.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(ws))
	acts := record[widget.ActivateEvent](h)

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

// Criterion 27 — a SINGLE click selects and publishes nothing.
func TestTreeSingleClickDoesNotActivate(t *testing.T) {
	ws := widget.NewTreeNode("ws", "workspace")
	tbl := widget.NewTreeNode("tbl", "a_table")
	ws.SetChildren(0, []*widget.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(ws))
	acts := record[widget.ActivateEvent](h)

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
	ws := widget.NewTreeNode("ws", "workspace")
	leaf := widget.NewTreeNode("note", "query.sql", widget.WithLeaf())
	ws.SetChildren(0, []*widget.TreeNode{leaf})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(ws))
	acts := record[widget.ActivateEvent](h)
	h.onLoop(func() { tr.ExpandPath("ws") })
	h.barrier(sh)

	h.inject(singlePress(1), singlePress(1))
	h.barrier(sh)

	if acts.count() != 1 {
		t.Errorf("activations = %d, want 1", acts.count())
	}
}
