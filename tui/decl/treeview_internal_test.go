package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

func newTreeForTest() *widget.Tree { return widget.NewTree() }

// TestReplacingChildrenForgetsTheOldOnes: children set again under a row drop
// what the view knew of the rows they replace — no stale path to route a later
// change to, nothing retained.
func TestReplacingChildrenForgetsTheOldOnes(t *testing.T) {
	m := NewTreeListModel("key", "label")
	m.SetChildren(nil, []TreeRow{{Row: Row{"key": "p", "label": "parent"}, HasChildren: true}})
	n := &treeViewNode{}
	n.tree = newTreeForTest()
	n.textRole = "label"
	n.setModel(m)
	top := Index{Row: 0}
	for round := range 3 {
		m.SetChildren(&top, []TreeRow{{Row: Row{"key": "c", "label": "child"}}, {Row: Row{"key": "d", "label": "other"}}})
		n.nodes(&top) // as an open shows them
		if want := 3; len(n.byPath) != want || len(n.at) != want {
			t.Fatalf("round %d: %d paths, %d nodes known; want %d each", round, len(n.byPath), len(n.at), want)
		}
	}
}

// TestAKnownRowOpensInTheKeysOwnHandling: a TreeView as a document builds it fills a row whose
// children its model already has as the row opens, not when a published request is handled later;
// so the key after the one that opened it finds them.
func TestAKnownRowOpensInTheKeysOwnHandling(t *testing.T) {
	comp, _, err := buildTreeView(Build{})
	if err != nil {
		t.Fatal(err)
	}
	n := comp.(*treeViewNode)
	n.textRole = "label"
	m := NewTreeListModel("key", "label")
	m.SetChildren(nil, []TreeRow{{Row: Row{"key": "p", "label": "parent"}, HasChildren: true}})
	top := Index{Row: 0}
	m.SetChildren(&top, []TreeRow{{Row: Row{"key": "c", "label": "child"}}})
	n.setModel(m)
	n.tree.ToggleExpanded(n.tree.VisibleRows()[0])
	if rows := n.tree.VisibleRows(); len(rows) != 2 || rows[1].ID() != "c" {
		t.Fatalf("after opening the parent, %d rows; want the parent and its child", len(rows))
	}
}
