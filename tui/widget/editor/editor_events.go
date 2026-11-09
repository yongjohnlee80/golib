package editor

import "github.com/yongjohnlee80/golib/tui"

// YankEvent is emitted by Editor when an EXPLICIT yank runs — visual `y` or
// `yy` — and reports whether the text reached the system clipboard.
//
// OUTCOME ONLY. It deliberately carries no text, unlike SubmitEvent, whose
// Value IS the point of a form field. A yank's payload may be a secret: the
// widget is used read-only to display credentials, and an event is exactly the
// kind of thing that ends up in a log line or a debug dump. A parent already
// knows what it rendered; what it cannot otherwise learn is whether the copy
// landed, because CopyToClipboard's bool is consumed inside the widget and
// HandleEvent returns only "handled".
//
// ClipboardDelivered is false — truthfully — when the backend implements no
// ClipboardWriter. That keeps the optional-capability contract (the widget
// does not error on a backend that cannot copy) while still letting a parent
// tell the user their copy did not leave the application.
//
// DELETES DO NOT EMIT THIS. `x`, `D`, `dd` and visual `d` fill the same
// register, correctly, but exporting them would publish text nobody asked to
// share.
type YankEvent struct {
	Owner              tui.NodeID
	ClipboardDelivered bool
}

// MacroStoppedEvent is emitted by an Editor when a macro (@{reg}) is stopped before its end, as
// Vim ends a macro on an error: Err says why (ErrMacroTooLong). Register is the one played. The
// keys still queued are dropped; what the macro changed stays, each change its own undo step. A
// host shows Err where it shows the editor's messages.
type MacroStoppedEvent struct {
	Owner    tui.NodeID
	Register rune
	Err      error
}
