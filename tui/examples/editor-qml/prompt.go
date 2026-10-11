package main

import (
	"errors"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE COMMAND PROMPT — the editor's own. `:` in Normal mode opens it (Ctrl+P,
// Space and File > Command too, through editor.openCommandLine()); Tab
// completes. The editor runs the lines; what they DO that is this program's
// comes back to it:
//
//	w, wq, x   the editor's writeRequested(quit): App.writeFromEditor
//	q, q!      the editor's quitRequested(force): App.quitFromEditor
//	e <file>   a command this program adds to the editor's own (addCommands)
//
// So a command is one more way into the same saving and quitting the menu and
// the keys reach, not a second implementation.

// addCommands composes this program's commands into the editor's.
func (h *Host) addCommands() error {
	return h.editor.Commands().Add(widget.Command{
		Name: "e", Aliases: []string{"edit"}, Args: "<file>", Help: "open a file",
		Run: func(x widget.CommandContext) error {
			if x.Line == "" {
				return errors.New("e: which file? e <file>")
			}
			return h.openFile(x.Line)
		},
	})
}

// writeFromEditor is w (quit false) and wq (quit true): save, then quit once
// the file is written. A buffer with no name asks for one first, and the quit
// waits for it (saveAs), or is dropped if the dialog is cancelled
// (saveCancelled) or the write fails.
func (h *Host) writeFromEditor(quit bool) error {
	if h.path == "" {
		h.quitAfterSave = quit
		return h.saveFile()
	}
	if err := h.write(h.path); err != nil {
		return err
	}
	if quit {
		h.p.Quit()
	}
	return nil
}

// quitFromEditor is q, and q! with force: q asks over unsaved changes, as
// Exit does; q! quits whatever is unsaved.
func (h *Host) quitFromEditor(force bool) error {
	if force {
		h.p.Quit()
		return nil
	}
	return h.quitAsking()
}

// quitAsking quits at once over a saved buffer, and asks over unsaved changes —
// as Exit does, through the same dialog.
func (h *Host) quitAsking() error {
	if h.dirty {
		return h.p.Call("quitDialog", "open")
	}
	h.p.Quit()
	return nil
}
