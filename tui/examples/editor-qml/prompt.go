package main

import (
	"strings"
)

// THE COMMAND PROMPT — what `onAccepted: App.runCommand(text)` runs.
//
// The document owns the prompt: a Dialog holding a TextField, opened by Ctrl+P
// or File > Command. The host owns what a command DOES — the same commands the
// menu and the keys reach, so a command is one more way in, not a second
// implementation:
//
//	w          save (asks for a name if the buffer has none)
//	q          quit — asks first when there are unsaved changes
//	wq         save, then quit once the file is written
//	e <file>   open a file (refused over unsaved changes)
//
// The prompt closes BEFORE the command runs: a command may open a dialog of
// its own — the quit question, Save As — and closing the prompt after would
// hand the keyboard back past it. One it does not know is reported in the
// status line.
func (h *Host) runCommand(line string) error {
	if err := h.p.Call("prompt", "close"); err != nil {
		return err
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)
	switch {
	case cmd == "w" && arg == "":
		return h.saveFile()
	case cmd == "q" && arg == "":
		return h.quitAsking()
	case cmd == "wq" && arg == "":
		if h.path == "" {
			// The Save dialog asks for a name; the quit waits for the file
			// to be written (saveAs), and is dropped if the dialog is
			// cancelled (saveCancelled) or the write fails.
			h.quitAfterSave = true
			return h.saveFile()
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
