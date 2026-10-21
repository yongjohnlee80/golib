package widget

import (
	"slices"

	"github.com/yongjohnlee80/golib/tui"
)

// TreeMenu is the key that asks a Tree's owner for a context menu on the cursor row, while the
// tree has one (SetContextMenu); List's ListMenu keys.
var TreeMenu = cmd("tui.tree.contextMenu", TreeScope, "Shift+F10", "Menu")

// treeKeyCommands are every command a Tree reads keys for: its own and the menu key.
var treeKeyCommands = append(slices.Clip(treeCommands), TreeMenu)

// WithTreeOnContextMenu tells the owner at once when the tree asks for a context menu (SetContextMenu
// on): the node it is for (nil: none, a press below the rows or an empty tree) and where, in the
// tree's own cells. ContextMenuEvent follows on the bus. The tree opens no menu itself.
func WithTreeOnContextMenu(fn func(n *TreeNode, at tui.Point)) TreeOption {
	return func(t *Tree) { t.on.menu = fn }
}

// SetContextMenu is List's, for a tree (ContextMenuRequester). On, a right press on a row puts the
// cursor there, as a left press does, and then asks for a menu for that node, at the pointer; a
// right press below the rows asks for one for no node (-1); and Shift+F10 or the Menu key ask for
// one for the cursor row, at its first cell. The press and the key are consumed. Off (the default),
// neither is handled, as before.
func (t *Tree) SetContextMenu(on bool) { t.contextMenu = on }

// menuPress is a right press with the context menu on: the cursor to the row first, reported as a
// left press reports it, then the request, unless the owner told of the cursor set new roots.
func (t *Tree) menuPress(e tui.MouseEvent) bool {
	at := tui.Point{X: e.X, Y: e.Y}
	rows := t.flatten()
	idx := t.top + e.Y
	if idx < 0 || idx >= len(rows) {
		t.requestMenu(nil, -1, at)
		return true
	}
	node := rows[idx].node
	if !t.presses.Survives(func() {
		t.cursor = idx
		t.ensureVisible()
		t.MarkDirty()
		t.noteSelection()
	}) {
		return true // the owner set new roots: no menu for a row that is gone
	}
	t.requestMenu(node, idx, at)
	return true
}

// requestMenu emits ContextMenuEvent for n at row i (nil and -1: none), at at, then tells the owner.
func (t *Tree) requestMenu(n *TreeNode, i int, at tui.Point) {
	t.publish(ContextMenuEvent{Owner: t.NodeID(), Index: i, At: at})
	if t.on.menu != nil {
		t.on.menu(n, at)
	}
}

// ContextMenuRequester is a row view that can ask its owner for a context menu instead of opening
// one: List and Tree. On, a right press or the menu key is reported (ContextMenuEvent and the
// owner's hook) and consumed; off, it goes on as before. A host with one context-menu policy sets it
// on each of its views and opens its own menu at the point given.
type ContextMenuRequester interface {
	SetContextMenu(on bool)
}

// MenuOpener is a menu a view opens itself, as DataTable does on a right press: opened at a point in
// the view's own cells, and closed when the view is given another. *EditorMenu is one. A menu that
// can be switched off says so by an Enabled() bool of its own (EditorMenu's), which the view asks
// for where it opens one; a menu without it is always on.
type MenuOpener interface {
	OpenAt(at tui.Point) bool
	Close()
}

// menuOn reports whether m would open: a menu that reports Enabled is asked, any other is on.
func menuOn(m MenuOpener) bool {
	if e, ok := m.(interface{ Enabled() bool }); ok {
		return e.Enabled()
	}
	return true
}

var (
	_ ContextMenuRequester = (*List[int])(nil)
	_ ContextMenuRequester = (*Tree)(nil)
	_ MenuOpener           = (*EditorMenu)(nil)
)
