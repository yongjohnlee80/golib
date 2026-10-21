package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A tree whose expand callback sets new roots does nothing more for the old ones: the click, key or
// path that expanded reports none of them to the owner, and the tree shows the new roots.
//
// This exercises the Tree's survival returns; it is not a defect cell. What the Tree did after the
// callback (reveal, noteSelection) reads only the current roots, so it passes against the code before
// them too (tree-epoch-mutant). The returns keep the owner-callback rule uniform, which
// internal/audit/owner_callback_test.go enforces.
func TestATreeDoesNothingMoreForRootsItsExpandCallbackReplaced(t *testing.T) {
	for name, act := range map[string]func(tr *Tree){
		"the expander click": func(tr *Tree) {
			tr.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 0, Y: 0, Count: 1})
		},
		"the Right key": func(tr *Tree) { tr.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight}) },
		"ExpandPath":    func(tr *Tree) { tr.ExpandPath("old", "child") },
	} {
		t.Run(name, func(t *testing.T) {
			old, fresh := NewTreeNode("old", "old"), NewTreeNode("new", "new", WithLeaf())
			var tr *Tree
			replaced := false
			var told []*TreeNode
			tr = NewTree(WithRoots(old),
				WithTreeOnExpand(func(*TreeNode) {
					if !replaced {
						replaced = true
						tr.SetRoots(fresh)
					}
				}),
				WithTreeOnCursor(func(n *TreeNode) { told = append(told, n) }))
			told = nil
			act(tr)
			rows := tr.VisibleRows()
			if !replaced || len(rows) != 1 || rows[0] != fresh {
				t.Fatalf("replaced %v; rows %v, want the new root alone", replaced, rows)
			}
			for _, n := range told {
				if n == old {
					t.Errorf("the owner was told of the old root after replacing it")
				}
			}
		})
	}
}
