package tui

import (
	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// The runtime's own commands: keys the App acts on itself, before or after routing a key to the
// widgets. They are overridable like any widget's; only a label's mnemonic is fixed.
var (
	// FocusNext and FocusPrev step focus through the current scope's ring (App.globalKey). They run
	// after every widget has declined the key, inside a modal trap too, so a widget that consumes Tab
	// (a text area inserting a tab) opts out of traversal for that press.
	FocusNext = keymap.Command{ID: "tui.app.focusNext", Scope: keymap.App, Keys: key.MustParse("Tab")}
	FocusPrev = keymap.Command{ID: "tui.app.focusPrev", Scope: keymap.App, Keys: key.MustParse("Shift+Tab")}

	// CancelCapture cancels a held pointer gesture (a drag, a resize) through its capture owner's
	// CaptureEscapeResolver, before the key is routed anywhere. Its scope is its own: it is live only
	// while a gesture holds the pointer.
	CancelCapture = keymap.Command{ID: "tui.capture.cancel", Scope: "tui.capture", Keys: key.MustParse("Esc")}
)

// Commands is the runtime's own commands, for keymap.Build beside every widget's.
func Commands() []keymap.Command { return []keymap.Command{FocusNext, FocusPrev, CancelCapture} }
