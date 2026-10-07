package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// THE RIGHT-CLICK MENU.
//
// Off by default: a right press bubbles to the editor's parents as it always
// has, so an application that gives the right button its own meaning keeps
// it. WithContextMenu turns it on. The stock rows are Undo and Redo, then Copy, Cut and Paste;
// a consumer can supply its own rows instead, built fresh each time the menu
// opens so their enabled state reflects the editor at that moment.
//
// The menu acts on the editor through the same entry points a menu bar uses
// (Undo, Redo, Copy, Cut, Paste): they keep the selection, settle a pending chord or
// count, and do not need focus. Paste pastes the editor's register — what was
// last copied or cut here. A terminal gives no way to read the system
// clipboard; text from outside arrives as the terminal's own paste.

// EditorMenuAction is a context-menu row's action: when the row is chosen, the
// editor runs Run with itself. A row whose action is not an EditorMenuAction
// does nothing.
type EditorMenuAction struct {
	ID  tui.ActionID
	Run func(e *Editor)
}

// ActionID implements tui.Action.
func (a EditorMenuAction) ActionID() tui.ActionID { return a.ID }

// The stock rows' ids and actions.
const (
	EditorMenuUndo  ItemID = "editor.undo"
	EditorMenuRedo  ItemID = "editor.redo"
	EditorMenuEdits ItemID = "editor.sep.edits" // the separator after Redo
	EditorMenuCopy  ItemID = "editor.copy"
	EditorMenuCut   ItemID = "editor.cut"
	EditorMenuPaste ItemID = "editor.paste"
)

// EditorContextItems are the stock rows for e, enabled as e stands now:
//   - Undo and Redo, when there is an edit to undo or redo and e is writable;
//   - a separator;
//   - Copy, when there is a selection and copying is allowed;
//   - Cut, when there is a selection and e is writable;
//   - Paste, when e is writable and its register holds something.
//
// A consumer that wants the stock rows plus its own builds on this slice.
func EditorContextItems(e *Editor) []MenuItemModel {
	_, _, _, _, selected := e.SelectionRange()
	text, linewise := e.Register()
	undoRow := NewCommand(EditorMenuUndo, "Undo", EditorMenuAction{ID: "editor.undo", Run: (*Editor).Undo})
	undoRow.Enabled = e.CanUndo()
	redoRow := NewCommand(EditorMenuRedo, "Redo", EditorMenuAction{ID: "editor.redo", Run: (*Editor).Redo})
	redoRow.Enabled = e.CanRedo()
	copyRow := NewCommand(EditorMenuCopy, "Copy", EditorMenuAction{ID: "editor.copy", Run: (*Editor).Copy})
	copyRow.Enabled = selected && e.canYank
	cutRow := NewCommand(EditorMenuCut, "Cut", EditorMenuAction{ID: "editor.cut", Run: (*Editor).Cut})
	cutRow.Enabled = selected && !e.readOnly
	pasteRow := NewCommand(EditorMenuPaste, "Paste", EditorMenuAction{ID: "editor.paste", Run: (*Editor).Paste})
	pasteRow.Enabled = !e.readOnly && (text != "" || linewise)
	return []MenuItemModel{undoRow, redoRow, NewSeparator(EditorMenuEdits), copyRow, cutRow, pasteRow}
}

// WithContextMenu turns the right-click menu on. build returns the rows to
// show and runs each time the menu opens; nil means EditorContextItems.
func WithContextMenu(build func(e *Editor) []MenuItemModel) EditorOption {
	return func(e *Editor) { e.ctxOn, e.ctxBuild = true, build }
}

// SetContextMenu turns the right-click menu on or off, keeping the rows
// WithContextMenu supplied (the stock rows when it supplied none). Turning it
// off closes a menu that is open.
func (e *Editor) SetContextMenu(on bool) {
	e.ctxOn = on
	if !on {
		e.closeContextMenu()
	}
}

// ContextMenuOpen reports whether the right-click menu is showing.
func (e *Editor) ContextMenuOpen() bool { return e.ctxOpen != nil }

// editorContextLayer names the menu's layer on the host. One per editor is
// enough: a second right press reopens it where the pointer now is.
func (e *Editor) editorContextLayer() LayerID {
	return LayerID(fmt.Sprintf("editor.contextmenu.%d", e.NodeID()))
}

// openContextMenu opens the menu at at, in the editor's own frame. It reports
// false — and the press bubbles on — when there is nowhere to open it: no
// OverlayHost above the editor, or no rows.
func (e *Editor) openContextMenu(at tui.Point) bool {
	host, ok := hostFor[popupHost](e.Context())
	if !ok {
		return false
	}
	build := e.ctxBuild
	if build == nil {
		build = EditorContextItems
	}
	items := build(e)
	if len(items) == 0 {
		return false
	}
	e.closeContextMenu()

	var layer *popupLayer
	menu := NewMenu(WithActionExecutor(func(inv tui.ActionInvocation) bool {
		act, ok := inv.Action.(EditorMenuAction)
		if !ok || act.Run == nil {
			return false
		}
		act.Run(e)
		if layer != nil {
			layer.requestClose(DismissAccept)
		}
		return true
	}))
	if err := menu.SetModel(items); err != nil {
		return false
	}
	panel := NewBox(menu)
	l, err := host.openPopupAt(e.editorContextLayer(), e, panel, at, func(DismissReason) {
		if e.ctxOpen == layer {
			e.ctxOpen = nil
		}
	})
	if err != nil {
		return false
	}
	layer = l
	e.ctxOpen = l
	return true
}

// closeContextMenu closes the menu if it is open.
func (e *Editor) closeContextMenu() {
	l := e.ctxOpen
	if l == nil {
		return
	}
	e.ctxOpen = nil
	l.close(DismissProgrammatic)
}
