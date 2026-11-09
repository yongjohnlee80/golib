package core

import (
	"reflect"

	"github.com/yongjohnlee80/golib/tui"
)

// CONTEXT MENUS — how a view asks its owner for a menu (ContextMenuRequester: List, Tree) or opens
// one itself (MenuOpener: DataTable, the editor's menu), shared by every view that has one.

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

// SameMenu reports whether a and b are the one menu. MenuOpener asks nothing of an implementation's
// comparability (a named slice satisfies it), so the values are compared only when they are of one
// type and comparable as they stand; any other pair is two menus, never a panic. golib's components
// draw through it; a widget outside golib rarely needs it.
func SameMenu(a, b MenuOpener) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.Type() == vb.Type() && va.Comparable() && va.Equal(vb)
}

// MenuWouldOpen reports whether m would open: a menu that reports Enabled is asked, any other is on. A
// view of your own that opens a MenuOpener asks it before it takes the right press.
func MenuWouldOpen(m MenuOpener) bool {
	if e, ok := m.(interface{ Enabled() bool }); ok {
		return e.Enabled()
	}
	return true
}
