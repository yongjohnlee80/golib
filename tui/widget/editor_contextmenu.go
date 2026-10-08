package widget

import "github.com/yongjohnlee80/golib/tui"

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
// editor runs Run with itself. A row whose action is neither an EditorMenuAction
// nor a CoreMenuAction (editormenu.go) does nothing.
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
// Their actions are EditorMenuActions. A consumer that wants the stock rows plus its own builds
// on this slice. CoreContextItems is the same rows over an EditorCore.
func EditorContextItems(e *Editor) []MenuItemModel {
	return contextRows(e.core, func(id tui.ActionID, run func(*EditorCore)) tui.Action {
		return EditorMenuAction{ID: id, Run: func(e *Editor) { run(e.core) }}
	})
}

// WithContextMenu turns the right-click menu on. build returns the rows to
// show and runs each time the menu opens; nil means EditorContextItems.
func WithContextMenu(build func(e *Editor) []MenuItemModel) EditorOption {
	return func(e *Editor) {
		e.menu.SetEnabled(true)
		if build != nil {
			e.menu.SetRows(func() []MenuItemModel { return build(e) })
		}
	}
}

// SetContextMenu turns the right-click menu on or off, keeping the rows
// WithContextMenu supplied (the stock rows when it supplied none). Turning it
// off closes a menu that is open.
func (e *Editor) SetContextMenu(on bool) { e.menu.SetEnabled(on) }

// ContextMenuOpen reports whether the right-click menu is showing.
func (e *Editor) ContextMenuOpen() bool { return e.menu.IsOpen() }
