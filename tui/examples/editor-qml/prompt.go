package main

import (
	"strings"
)

// THE COMMAND PROMPT — what `onAccepted: App.runCommand(text)` runs.
//
// The document owns the prompt: a Popup holding a TextField, opened by Ctrl+P
// or File > Command. The host owns what a command DOES — the same commands the
// menu and the keys reach, so a command is one more way in, not a second
// implementation:
//
//	w          save (asks for a name if the buffer has none)
//	q          quit — asks first when there are unsaved changes
//	wq         save, then quit once the file is written
//	e <file>   open a file (refused over unsaved changes)
//
// The prompt closes when a command has run; one it does not know is reported
// in the status line.
func (h *Host) runCommand(line string) error {
	defer func() { _ = h.p.Call("prompt", "close") }()
	cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)
	switch {
	case cmd == "w" && arg == "":
		return h.saveFile()
	case cmd == "q" && arg == "":
		return h.quitAsking()
	case cmd == "wq" && arg == "":
		if h.path == "" {
			return h.saveFile() // the Save dialog; quitting waits for a name
		}
		if err := h.write(h.path); err != nil {
			return err
		}
		h.p.Quit()
		return nil
	case cmd == "e" && arg != "":
		return h.openFile(arg)
	case cmd == "":
		return nil
	}
	return h.message("not a command: " + strings.TrimSpace(line))
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
