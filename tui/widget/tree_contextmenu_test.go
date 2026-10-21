package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tree_contextmenu_test.go: a node's context menu (Tree.SetContextMenu), as List's.

func threeRoots() []*widget.TreeNode {
	return []*widget.TreeNode{
		widget.NewTreeNode("a", "alpha", widget.WithLeaf()),
		widget.NewTreeNode("b", "beta", widget.WithLeaf()),
		widget.NewTreeNode("c", "gamma", widget.WithLeaf()),
	}
}

// Without SetContextMenu a right press and Shift+F10 are not the tree's: the cursor stays, no menu
// is asked for, and the press goes on to the tree's parent, as before.
func TestTreeContextMenuIsOffByDefault(t *testing.T) {
	asked := 0
	h, tr, sh := focusedTree(t, 30, 6, widget.WithRoots(threeRoots()...),
		widget.WithTreeOnContextMenu(func(*widget.TreeNode, tui.Point) { asked++ }))
	menus := record[widget.ContextMenuEvent](h)
	h.inject(rightPress(3, 2), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10, Mods: tui.ModShift})
	h.barrier(sh)
	if id := selectedID(h, tr); id != "a" || asked != 0 || len(menus.events()) != 0 || shellMice(h, sh) != 1 {
		t.Fatalf("off: cursor on %q, owner asked %d times, menus %+v, the parent saw %d presses",
			id, asked, menus.events(), shellMice(h, sh))
	}
}

// On, a right press on a row puts the cursor there first, reported as a left press reports it, and
// then asks the owner for that node's menu at the pointer; ContextMenuEvent follows on the bus with
// the row. Below the rows it asks for none (nil, -1), the cursor left where it is.
func TestTreeContextMenuAsksForTheNodePressed(t *testing.T) {
	var log []string
	var at tui.Point
	h, tr, sh := focusedTree(t, 30, 6, widget.WithRoots(threeRoots()...),
		widget.WithTreeOnCursor(func(n *widget.TreeNode) { log = append(log, "cursor "+n.ID()) }),
		widget.WithTreeOnContextMenu(func(n *widget.TreeNode, p tui.Point) {
			id := "none"
			if n != nil {
				id = n.ID()
			}
			log, at = append(log, "menu "+id), p
		}))
	h.onLoop(func() { tr.SetContextMenu(true); log = nil })
	menus := record[widget.ContextMenuEvent](h)

	h.inject(rightPress(3, 1))
	h.barrier(sh)
	var got []string
	h.onLoop(func() { got = append(got, log...) })
	if len(got) != 2 || got[0] != "cursor b" || got[1] != "menu b" || at != (tui.Point{X: 3, Y: 1}) {
		t.Fatalf("a right press on beta: owner told %q at %v; want the cursor, then beta's menu at {3 1}", got, at)
	}
	if ev, ok := menus.last(); !ok || ev.Index != 1 || ev.At != (tui.Point{X: 3, Y: 1}) {
		t.Errorf("ContextMenuEvent %+v, want row 1 at {3 1}", ev)
	}

	h.onLoop(func() { log = nil })
	h.inject(rightPress(3, 5))
	h.barrier(sh)
	h.onLoop(func() { got = append(got[:0], log...) })
	if id := selectedID(h, tr); len(got) != 1 || got[0] != "menu none" || id != "b" {
		t.Errorf("a right press below the rows: owner told %q, cursor on %q; want no node's menu, the cursor kept", got, id)
	}
	if ev, ok := menus.last(); !ok || ev.Index != -1 {
		t.Errorf("below the rows: ContextMenuEvent %+v, want row -1", ev)
	}
}

// On, Shift+F10 and the Menu key ask for the cursor node's menu, at its row's first cell.
func TestTreeContextMenuKeysAskForTheCursorNode(t *testing.T) {
	var asked []string
	h, tr, sh := focusedTree(t, 30, 6, widget.WithRoots(threeRoots()...),
		widget.WithTreeOnContextMenu(func(n *widget.TreeNode, p tui.Point) {
			if n != nil && p == (tui.Point{Y: 2}) {
				asked = append(asked, n.ID())
			}
		}))
	h.onLoop(func() { tr.SetContextMenu(true); tr.SetCursor(2) })
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10, Mods: tui.ModShift}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyMenu})
	h.barrier(sh)
	var got []string
	h.onLoop(func() { got = append(got, asked...) })
	if len(got) != 2 || got[0] != "c" || got[1] != "c" {
		t.Errorf("Shift+F10 and Menu: owner asked for %q, want gamma's menu twice at {0 2}", got)
	}
}

// An owner told of the cursor that sets new roots gets no menu for the row that is gone.
func TestTreeContextMenuForARowItsOwnerReplacedIsNotAsked(t *testing.T) {
	asked := 0
	var tr *widget.Tree
	h, tr, sh := focusedTree(t, 30, 6, widget.WithRoots(threeRoots()...),
		widget.WithTreeOnCursor(func(n *widget.TreeNode) {
			if n.ID() == "b" {
				tr.SetRoots(widget.NewTreeNode("x", "other", widget.WithLeaf()))
			}
		}),
		widget.WithTreeOnContextMenu(func(*widget.TreeNode, tui.Point) { asked++ }))
	h.onLoop(func() { tr.SetContextMenu(true) })
	h.inject(rightPress(3, 1))
	h.barrier(sh)
	var n int
	h.onLoop(func() { n = asked })
	if n != 0 {
		t.Errorf("the owner replaced the roots when told of the cursor; a menu was still asked %d times", n)
	}
}
