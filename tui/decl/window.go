package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE WINDOW — the top-level surface a document mounts into.
//
// It is a real container, mounting golib's OverlayHost as its child, rather
// than the OverlayHost itself. That is the Go editor's own shape — its App
// mounts the host the same way — and it is what puts the Window on the path
// every key bubbles up, which three things need:
//
//   - Shortcut: a sequence must fire whichever widget holds focus;
//   - menu access keys: Alt+F opens File, F10 goes to the bar, as in Qt;
//   - focus: `focus: true` says where the keyboard starts, and it must come
//     back there when a menu action finishes or Escape leaves the menu.
//
// It mounts its child rather than embedding it. Embedding would make every
// OverlayHost method calling its own receiver bypass anything overridden here —
// Go has no virtual dispatch — and a mounted child keeps every capability it
// has without this type forwarding a single interface.

// windowNode is the component a `Window` declaration builds.
type windowNode struct {
	ctx  *tui.Context
	host *widget.OverlayHost

	// focus is the document's `focus: true` target, or nil.
	focus     tui.Component
	shortcuts []*shortcutNode
	menus     []*menuBarNode
}

func buildWindow(b Build) (tui.Component, []string, error) {
	if len(b.Children) == 0 {
		return nil, nil, fmt.Errorf("Window needs at least one child (at %s)", b.Pos)
	}
	w := &windowNode{focus: b.FocusNominee}
	dock := tui.NewDock()
	for i, child := range b.Children {
		switch c := child.(type) {
		case *menuNode:
			return nil, nil, fmt.Errorf("a %s belongs inside a MenuBar, not directly in a Window (at %s)",
				c.kind, b.Pos)
		case *shortcutNode:
			// Keys, not layout: a Shortcut takes no place on the screen.
			w.shortcuts = append(w.shortcuts, c)
			continue
		case *menuBarNode:
			w.menus = append(w.menus, c)
		}
		v, pinned := b.Attached(i, "Dock.edge")
		if !pinned {
			dock.Add(child)
			continue
		}
		edge, err := dockEdges.read(v)
		if err != nil {
			return nil, nil, err
		}
		dock.Pin(edge, child)
	}
	w.host = widget.NewOverlayHost(dock)
	return w, nil, nil
}

// Init mounts the host, listens for finished menu actions, and gives the
// keyboard to the document's `focus: true` target.
//
// Initial focus is set the way golib/tui expects: one ctx.FocusComponent call
// at mount. It is NOT an InitialFocusProvider, which is a focus scope's
// standing preference that the runtime re-asserts on every repair — right for
// a dialog, wrong for an application screen, where it would pull the keyboard
// back to the editor every time the user moved to the menu. Dialogs keep their
// own trapping scopes; nothing here competes with them.
func (w *windowNode) Init(ctx *tui.Context) {
	w.ctx = ctx
	ctx.Mount(w.host)
	// A menu row that ran closes the menu. Focus goes back where the document
	// said it lives; left on the closed menu, the next keystroke would go
	// nowhere the user can see.
	tui.SubscribeScoped(ctx, func(ev widget.MenuActivatedEvent) {
		if ev.Handled {
			w.restoreFocus()
		}
	})
	w.restoreFocus()
}

func (w *windowNode) restoreFocus() {
	if w.focus != nil && w.ctx != nil {
		w.ctx.FocusComponent(w.focus)
	}
}

func (w *windowNode) Layout(c tui.Constraints) tui.Size {
	sz := w.ctx.LayoutChild(w.host, c)
	w.ctx.PlaceChild(w.host, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (w *windowNode) Render(tui.Surface) {}

// HandleEvent sees every key the focused widget did not consume.
//
// The document's own Shortcuts are tried first, so a document can claim any
// sequence — including one the menu would otherwise take.
func (w *windowNode) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.FocusEvent); ok {
		// A FocusEvent bubbles up from whichever node lost or gained focus, so
		// this is where the Window learns the user clicked into the document
		// while a dropdown was open. Not consumed: other components are
		// entitled to the same news.
		for _, m := range w.menus {
			m.closeOnBlur()
		}
		return false
	}
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind != tui.KeyPress {
		return false
	}
	for _, s := range w.shortcuts {
		if s.seq.matches(k) {
			s.trigger()
			return true
		}
	}
	for _, m := range w.menus {
		switch {
		case k.Code == tui.KeyF10 && k.Mods&shortcutMods == 0:
			if m.focused() {
				m.close()
				w.restoreFocus()
			} else {
				m.activate()
			}
			return true
		case k.Mods&shortcutMods == tui.ModAlt:
			if m.openHotkey(k.Code) {
				return true
			}
		case k.Code == tui.KeyEscape && m.focused():
			// Escape that the menu did not consume leaves the menu entirely.
			m.close()
			w.restoreFocus()
			return true
		}
	}
	return false
}

// menuBarNode is the component a `MenuBar` declaration builds: golib's MenuBar,
// mounted, and the access keys of its categories.
type menuBarNode struct {
	ctx        *tui.Context
	bar        *widget.MenuBar
	menu       *widget.Menu
	categories []menuCategory
}

// menuCategory is one top-level Menu's access key.
type menuCategory struct {
	hotkey rune
	id     widget.ItemID
}

func (m *menuBarNode) Init(ctx *tui.Context) { m.ctx = ctx; ctx.Mount(m.bar) }

func (m *menuBarNode) Layout(c tui.Constraints) tui.Size {
	sz := m.ctx.LayoutChild(m.bar, c)
	m.ctx.PlaceChild(m.bar, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (m *menuBarNode) Render(tui.Surface)         {}
func (m *menuBarNode) HandleEvent(tui.Event) bool { return false }

func (m *menuBarNode) focused() bool {
	ctx := m.menu.Context()
	return ctx != nil && ctx.Focused()
}

// activate gives the menu focus, ALWAYS from the first category — resuming
// wherever the last visit ended is not where anybody expects to start.
func (m *menuBarNode) activate() {
	ctx := m.menu.Context()
	if ctx == nil {
		return
	}
	if len(m.categories) > 0 {
		m.menu.Select(m.categories[0].id)
	}
	ctx.RequestFocus()
}

// openHotkey opens the category whose mnemonic is r, and reports whether one
// had it.
func (m *menuBarNode) openHotkey(r rune) bool {
	for _, c := range m.categories {
		if c.hotkey == r {
			m.activate()
			// Opening the level hands the selection to its first row, which is
			// what puts the highlight on "New" when File drops down.
			_ = m.menu.Open(c.id)
			return true
		}
	}
	return false
}

// closeOnBlur closes the cascade once focus has left the menu.
//
// APPLICATION POLICY, NOT THE WIDGET'S. golib deliberately keeps a level open
// across a focus loss — a renderer drawing a cascade must still see it, and an
// involuntary loss is not a decision the user made about the menu. A document
// window wants the other rule: clicking into the content means "I am done with
// the menu", and a dropdown left hanging covers the line the user just aimed
// at. Losing focus to something that is not the menu is that click.
func (m *menuBarNode) closeOnBlur() {
	if m.menu.OpenLevels() == 0 || m.focused() {
		return
	}
	m.close()
}

func (m *menuBarNode) close() {
	m.menu.Close()
	if ctx := m.menu.Context(); ctx != nil {
		ctx.MarkDirty()
	}
}
