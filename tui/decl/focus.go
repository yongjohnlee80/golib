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
// (tui.Context.FocusInto). So a pane is focused by naming the pane, whatever
// its focusable part is. From Go, a host calls it by id:
// Program.Call("explorer", "forceActiveFocus").
//
// NOT FOCUSABLE BY DESIGN is refused, as the document's mistake: an item with
// nothing on screen (a menu row, a model), and an item nothing in which takes
// focus at all — a Text, a StatusBar, a Gauge (tui.Context.HoldsFocusable).
// An item that could take focus but cannot NOW — hidden, disabled, or kept
// out by an open dialog's focus trap — is left as it is, as in Qt.

const forceActiveFocus = "forceActiveFocus"

// contexted is a component that can be asked where it is mounted: every golib
// widget (widget.Base) and container (tui.MultiChild).
type contexted interface{ Context() *tui.Context }

func focusInto(c tui.Component, args []qml.SpecValue) error {
	if len(args) != 0 {
		return fmt.Errorf("takes no arguments, and was given %d", len(args))
	}
	cx, ok := c.(contexted)
	if !ok {
		return fmt.Errorf("a %T has nothing on screen to focus", c)
	}
	ctx := cx.Context()
	if ctx == nil {
		return nil // not mounted: nothing to focus, as for a hidden item
	}
	if !ctx.HoldsFocusable(c) {
		return fmt.Errorf("it is not focusable by design: nothing in it takes focus")
	}
	ctx.FocusInto(c)
	return nil
}
