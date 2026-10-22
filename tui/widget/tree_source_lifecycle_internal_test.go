package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// leaves is a root set of leaves, one per label.
func leaves(labels ...string) []*TreeNode {
	out := make([]*TreeNode, len(labels))
	for i, s := range labels {
		out[i] = NewTreeNode(s, s, WithLeaf())
	}
	return out
}

// A tree whose roots are emptied acts on nothing, and once given roots again it starts at the first
// of them.
func TestATreeEmptiedThenGivenRootsActsOnThem(t *testing.T) {
	var acts []string
	tr := NewTree(WithRoots(leaves("a", "b", "c")...),
		WithTreeOnActivate(func(n *TreeNode) { acts = append(acts, n.ID()) }))
	pressKey(tr, tui.KeyDown, tui.KeyDown, tui.KeyEnter)
	if len(acts) != 1 || acts[0] != "c" {
		t.Fatalf("the control: Down, Down, Enter activated %v, want [c]", acts)
	}
	tr.SetRoots()
	pressKey(tr, tui.KeyEnter, tui.KeyDown, tui.KeyEnter)
	if len(acts) != 1 || len(tr.VisibleRows()) != 0 {
		t.Fatalf("an empty tree: activations %v, rows %v", acts[1:], tr.VisibleRows())
	}
	tr.SetRoots(leaves("x", "y")...)
	pressKey(tr, tui.KeyEnter)
	pressKey(tr, tui.KeyDown, tui.KeyEnter)
	if got := acts[1:]; len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("given x and y: Enter, then Down and Enter activated %v, want [x y]", got)
	}
}

// An owner whose activation sets new roots leaves the tree on them: the owner hears nothing more of
// the old rows, and the next Enter activates a new one.
func TestATreeActivationThatReplacesTheRootsLeavesTheNewRows(t *testing.T) {
	old := leaves("old-a", "old-b")
	var tr *Tree
	var acts, told []string
	replaced := false
	tr = NewTree(WithRoots(old...),
		WithTreeOnActivate(func(n *TreeNode) {
			acts = append(acts, n.ID())
			if !replaced {
				replaced = true
				tr.SetRoots(leaves("new-a", "new-b")...)
			}
		}),
		WithTreeOnCursor(func(n *TreeNode) { told = append(told, n.ID()) }))
	pressKey(tr, tui.KeyDown)
	told = nil
	pressKey(tr, tui.KeyEnter)
	if rows := tr.VisibleRows(); len(rows) != 2 || rows[0].ID() != "new-a" {
		t.Fatalf("after the activation replaced the roots, rows %v", rows)
	}
	pressKey(tr, tui.KeyEnter)
	if len(acts) != 2 || acts[0] != "old-b" || acts[1] != "new-a" {
		t.Errorf("activations %v, want [old-b new-a]", acts)
	}
	for _, id := range told {
		if id == "old-a" || id == "old-b" {
			t.Errorf("the owner was told of %s after the roots were replaced", id)
		}
	}
}

// A press and a second press across new roots do not pair, though the second lands where the first
// did; with the roots unchanged they activate the row once.
func TestATreeDoublePressDoesNotPairAcrossNewRoots(t *testing.T) {
	press := func(tr *Tree, count int) {
		tr.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 0, Count: count})
	}
	for name, between := range map[string]func(tr *Tree){
		"unchanged roots, the control": func(*Tree) {},
		"new roots":                    func(tr *Tree) { tr.SetRoots(leaves("x", "y")...) },
	} {
		t.Run(name, func(t *testing.T) {
			var acts []string
			tr := NewTree(WithRoots(leaves("a", "b")...),
				WithTreeOnActivate(func(n *TreeNode) { acts = append(acts, n.ID()) }))
			press(tr, 1)
			between(tr)
			press(tr, 2)
			want := 1
			if name == "new roots" {
				want = 0
			}
			if len(acts) != want {
				t.Errorf("activations %v, want %d", acts, want)
			}
		})
	}
}

// An owner whose expand-request callback sets new roots hears nothing more of the node it was asked
// about: no expand callback for it, no cursor on it, and its children, answered late, show nowhere.
func TestATreeExpandRequestThatReplacesTheRootsAsksNothingMore(t *testing.T) {
	old := NewTreeNode("old", "old")
	var tr *Tree
	var expanded, told []string
	var asked uint64
	replaced := false
	tr = NewTree(WithRoots(old),
		WithTreeOnExpandRequest(func(n *TreeNode, gen uint64) {
			asked = gen
			if !replaced {
				replaced = true
				tr.SetRoots(leaves("new")...)
			}
		}),
		WithTreeOnExpand(func(n *TreeNode) { expanded = append(expanded, n.ID()) }),
		WithTreeOnCursor(func(n *TreeNode) { told = append(told, n.ID()) }))
	told = nil
	pressKey(tr, tui.KeyRight)
	old.SetChildren(asked, leaves("late"))
	rows := tr.VisibleRows()
	if !replaced || len(rows) != 1 || rows[0].ID() != "new" {
		t.Fatalf("replaced %v; rows %v, want the new root alone", replaced, rows)
	}
	if len(expanded) != 0 {
		t.Errorf("the expand callback ran for %v after the roots were replaced", expanded)
	}
	for _, id := range told {
		if id == "old" {
			t.Errorf("the owner was told of the old root after replacing it")
		}
	}
}
