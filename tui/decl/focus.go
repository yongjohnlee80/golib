package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// FORCE ACTIVE FOCUS — Qt's Item.forceActiveFocus(), on every item.
//
//	Shortcut { sequence: "Ctrl+H"; onActivated: explorer.forceActiveFocus() }
//
// Focus moves INTO the item: the item itself when it takes focus, else its
// focus child — the one it nominates, else its first focusable descendant
// (tui.App.FocusInto). So a pane is focused by naming the pane, whatever its
// focusable part is. From Go, a host calls it by id:
// Program.Call("explorer", "forceActiveFocus").
//
// The call goes through the program's own focus owner, the App, so any
// component is reached — a custom one included, whatever accessors it has.
//
// NOT FOCUSABLE BY DESIGN is refused, as the document's mistake: a
// declaration with nothing on screen (a menu row, a Shortcut, a column), and
// a mounted item nothing in which takes focus at all — a Text, a StatusBar, a
// Gauge. An item that could take focus but cannot NOW — hidden, disabled, kept
// out by an open dialog, or not on screen at the moment (a closed dialog's
// content) — is left as it is, as in Qt.

const forceActiveFocus = "forceActiveFocus"

// declarationOnly is a node that is a declaration and never on screen: a menu
// row, a Shortcut, a TableViewColumn, a SyntaxHighlighter, a DialogButtonBox
// before its Dialog takes it.
type declarationOnly interface{ declarationOnly() }

// onScreenAs is a node whose screen presence is another component: a Dialog
// is on screen as its modal.
type onScreenAs interface{ onScreen() tui.Component }

// useApp gives the adapter the App its program runs on: what forceActiveFocus
// moves focus with. The Program calls it once it has built the App.
func (a *Adapter) useApp(app *tui.App) { a.app = app }

func (a *Adapter) forceActiveFocus(b built, args []qml.SpecValue) error {
	if len(args) != 0 {
		return fmt.Errorf("%s.%s: takes no arguments, and was given %d", b.typ, forceActiveFocus, len(args))
	}
	comp := b.comp
	if _, ok := comp.(declarationOnly); ok {
		return fmt.Errorf("%s.%s: a %s has nothing on screen to focus", b.typ, forceActiveFocus, b.typ)
	}
	if s, ok := comp.(onScreenAs); ok {
		comp = s.onScreen()
	}
	if a.app == nil {
		return nil // no App yet: nothing is on screen
	}
	holds, mounted := a.app.HoldsFocusable(comp)
	if !mounted {
		return nil // not on screen now — a closed dialog's content — as for a hidden item
	}
	if !holds {
		return fmt.Errorf("%s.%s: a %s is not focusable by design: nothing in it takes focus",
			b.typ, forceActiveFocus, b.typ)
	}
	a.app.FocusInto(comp)
	return nil
}
