package widget_test

// Logical identity for double-click activation (lector r1 findings 2 and 3, and
// the expander design note).
//
// The App supplies timing, button, cell and delivered target. It cannot supply
// LOGICAL identity, because it does not know what a row or a node is — and an
// absolute cell is not a row: scroll the viewport between the two presses and
// the same cell addresses different data. Each widget therefore checks its own
// notion of "the same thing" before activating.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Finding 2 — THE REGRESSION CONTROL. List's own detection compared logical
// rows and refused this pair; the first conversion to the boundary count
// compared only the cell and activated row 2 after a click on row 1.
func TestListDoubleClickAcrossAScrollDoesNotActivate(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 3)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1)) // logical row 1
	h.barrier(sh)
	var first int
	h.onLoop(func() { first, _ = l.Selected() })
	if first != 1 {
		t.Fatalf("precondition: selected %d, want 1", first)
	}

	// Scroll one row: the SAME cell now addresses logical row 2.
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
	h.barrier(sh)

	h.inject(click(2, 1))
	h.barrier(sh)

	if n := acts.count(); n != 0 {
		ev, _ := acts.last()
		t.Errorf("activations = %d (%+v), want 0 — the two presses landed on "+
			"different logical rows, so this is not a double-click", n, ev)
	}
}

// The same pair WITHOUT the scroll still activates, so the control above is
// about identity and not about the wheel resetting something.
func TestListDoubleClickWithoutScrollStillActivates(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 3)
	acts := record[widget.ActivateEvent](h)
	_ = l

	h.inject(click(2, 1))
	h.barrier(sh)
	h.inject(click(2, 1))
	h.barrier(sh)

	if ev, ok := acts.last(); !ok || ev.Index != 1 {
		t.Errorf("ActivateEvent = %+v (ok=%v), want index 1", ev, ok)
	}
}

// Finding 3 — a triple-click is ONE activation, not two. With `>= 2` it fired
// on counts 2 and 3.
func TestListTripleClickActivatesOnce(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d"})
	h, _, sh := focusedList(t, src, 10, 4)
	acts := record[widget.ActivateEvent](h)

	h.inject(click(2, 1), click(2, 1), click(2, 1))
	h.barrier(sh)

	if n := acts.count(); n != 1 {
		t.Errorf("activations from a triple-click = %d, want exactly 1", n)
	}
}

func TestTreeTripleClickActivatesOnce(t *testing.T) {
	ws := widget.NewTreeNode("ws", "workspace")
	tbl := widget.NewTreeNode("tbl", "a_table")
	ws.SetChildren(0, []*widget.TreeNode{tbl})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(ws))
	acts := record[widget.ActivateEvent](h)
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
	ws := widget.NewTreeNode("ws", "workspace")
	child := widget.NewTreeNode("child", "a_child", widget.WithLeaf())
	ws.SetChildren(0, []*widget.TreeNode{child})

	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(ws))
	acts := record[widget.ActivateEvent](h)

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
