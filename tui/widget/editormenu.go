package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE EDITOR'S RIGHT-CLICK MENU, AS A MODULE.
//
// EditorMenu is the menu an editor widget opens on a right press: whether it is on, the rows it
// shows, and the popup while it is open. It belongs to the widget it opens over (its owner, which
// must sit under an OverlayHost), and its stock rows act on that widget's EditorCore. The tui
// Editor holds one; so can any other editor widget.
type EditorMenu struct {
	on    bool
	build func() []MenuItemModel // nil: CoreContextItems(core)
	open  *popupLayer            // the shown popup, nil when closed
	owner tui.Component          // the widget it opens over, under that widget's OverlayHost
	core  *EditorCore            // the stock rows' target
}

// NewEditorMenu is a menu over owner whose stock rows act on core. It is off until
// SetEnabled(true). owner must have a Context (a mounted widget) when the menu opens.
func NewEditorMenu(owner tui.Component, core *EditorCore) *EditorMenu {
	return &EditorMenu{owner: owner, core: core}
}

// SetEnabled turns the menu on or off; off closes it if it is open.
func (m *EditorMenu) SetEnabled(on bool) {
	m.on = on
	if !on {
		m.Close()
	}
}

// Enabled reports whether a right press opens the menu.
func (m *EditorMenu) Enabled() bool { return m.on }

// SetRows sets what builds the rows, each time the menu opens, so their enabled state is the
// editor's at that moment; nil is the stock rows (CoreContextItems).
func (m *EditorMenu) SetRows(build func() []MenuItemModel) { m.build = build }

// OpenAt opens the menu at at, in the owner's own frame. It reports false — and the press bubbles
// on — when it is off, when there is nowhere to open it (no OverlayHost above the owner), or when
// there are no rows. An open menu reopens where the pointer now is.
func (m *EditorMenu) OpenAt(at tui.Point) bool {
	if !m.on {
		return false
	}
	ider, ok := m.owner.(interface{ Context() *tui.Context })
	if !ok || ider.Context() == nil {
		return false
	}
	host, ok := hostFor[popupHost](ider.Context())
	if !ok {
		return false
	}
	var items []MenuItemModel
	if m.build != nil {
		items = m.build()
	} else {
		items = CoreContextItems(m.core)
	}
	if len(items) == 0 {
		return false
	}
	m.Close()

	var layer *popupLayer
	// h j k l move as the arrows do: an editor's menu is opened from an editor, often a Vim one,
	// and a row's own mnemonic still wins over them (WithMenuVimNavigation)
	menu := NewMenu(WithMenuVimNavigation(true), WithActionExecutor(func(inv tui.ActionInvocation) bool {
		if !m.run(inv.Action) {
			return false
		}
		if layer != nil {
			layer.requestClose(DismissAccept)
		}
		return true
	}))
	if err := menu.SetModel(items); err != nil {
		return false
	}
	id := LayerID(fmt.Sprintf("editor.contextmenu.%d", ider.Context().ID()))
	// the box on the menu's own surface: a bare one's border cells kept the terminal's default
	// background, a band of it around the menu in a window
	box := NewBox(menu, WithStyle(style.New().Background(style.TokenPanel)))
	l, err := host.openPopupAt(id, m.owner, box, at, func(DismissReason) {
		if m.open == layer {
			m.open = nil
		}
	})
	if err != nil {
		return false
	}
	layer = l
	m.open = l
	return true
}

// run performs a chosen row's action: a CoreMenuAction on the core, an EditorMenuAction on a tui
// Editor owner. Any other action does nothing.
func (m *EditorMenu) run(a tui.Action) bool {
	switch act := a.(type) {
	case CoreMenuAction:
		if act.Run == nil {
			return false
		}
		act.Run(m.core)
		return true
	case EditorMenuAction:
		e, ok := m.owner.(*Editor)
		if !ok || act.Run == nil {
			return false
		}
		act.Run(e)
		return true
	}
	return false
}

// Close closes the menu if it is open.
func (m *EditorMenu) Close() {
	l := m.open
	if l == nil {
		return
	}
	m.open = nil
	l.close(DismissProgrammatic)
}

// IsOpen reports whether the menu is showing.
func (m *EditorMenu) IsOpen() bool { return m.open != nil }

// CoreMenuAction is a menu row's action on an EditorCore: when the row is chosen, Run runs with
// the menu's core. A graphical editor's rows use it; the tui Editor's may use EditorMenuAction.
type CoreMenuAction struct {
	ID  tui.ActionID
	Run func(c *EditorCore)
}

// ActionID implements tui.Action.
func (a CoreMenuAction) ActionID() tui.ActionID { return a.ID }

// CoreContextItems are the stock rows over c, as EditorContextItems describes them, with
// CoreMenuAction actions.
func CoreContextItems(c *EditorCore) []MenuItemModel {
	return contextRows(c, func(id tui.ActionID, run func(*EditorCore)) tui.Action {
		return CoreMenuAction{ID: id, Run: run}
	})
}

// contextRows builds the stock rows over c, each action made by act:
//   - Undo and Redo, when there is an edit to undo or redo and c is writable;
//   - a separator;
//   - Copy, when there is a selection and copying is allowed;
//   - Cut, when there is a selection and c is writable;
//   - Paste, when c is writable and its register holds something.
func contextRows(c *EditorCore, act func(id tui.ActionID, run func(*EditorCore)) tui.Action) []MenuItemModel {
	_, _, _, _, selected := c.SelectionRange()
	text, linewise := c.Register()
	row := func(id ItemID, label, msg string, run func(*EditorCore), enabled bool) MenuItemModel {
		r := NewCommand(id, label, act(tui.ActionID(id), run))
		r.LabelMsg = tui.Msg(msg)
		r.Enabled = enabled
		return r
	}
	return []MenuItemModel{
		row(EditorMenuUndo, "Undo", "tui.editor.menu.undo", (*EditorCore).Undo, c.CanUndo()),
		row(EditorMenuRedo, "Redo", "tui.editor.menu.redo", (*EditorCore).Redo, c.CanRedo()),
		NewSeparator(EditorMenuEdits),
		row(EditorMenuCopy, "Copy", "tui.editor.menu.copy", (*EditorCore).Copy, selected && c.reg.yankAllowed()),
		row(EditorMenuCut, "Cut", "tui.editor.menu.cut", (*EditorCore).Cut, selected && !c.readOnly),
		row(EditorMenuPaste, "Paste", "tui.editor.menu.paste", (*EditorCore).Paste, !c.readOnly && (text != "" || linewise)),
	}
}
