package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// THE BUFFER'S FILE — the commands that read and write it.

// newFile starts an empty, unnamed buffer.
//
// NOT OVER UNSAVED CHANGES, for the reason Open refuses: it would discard them
// without a word — and a buffer that has never been saved has no other copy.
// A REFUSAL, not an error: nothing failed, and the status line says why.
func (h *Host) newFile() error {
	if h.dirty {
		return h.message(unsavedRefusal("start a new file"))
	}
	h.editor.SetValue("")
	if err := h.setPath(""); err != nil {
		return err
	}
	if err := h.setDirty(false); err != nil {
		return err
	}
	return h.message("new buffer")
}

// openFile loads the file the Open dialog chose.
//
// NOT OVER UNSAVED CHANGES: opening would discard them without a word, so it
// says so and leaves the buffer as it is. Save first, then open.
func (h *Host) openFile(path string) error {
	if h.dirty {
		return h.message(unsavedRefusal("open"))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return h.failed("cannot open", err)
	}
	h.editor.SetValue(string(b))
	if err := h.setPath(path); err != nil {
		return err
	}
	return h.setDirty(false)
}

// saveFile writes the buffer to its file — or, when it has none, asks for one.
//
// The ONE place the host opens a dialog itself. Whether the buffer has a name
// is the host's to know, and a document has no way to say "if" — so the menu
// and Ctrl+S both come here, and this decides.
func (h *Host) saveFile() error {
	if h.path == "" {
		return h.openDialog("saveDialog")
	}
	return h.write(h.path)
}

// saveAs writes the buffer to the file the Save dialog named, which becomes
// the buffer's file — ONLY if the write succeeded. A failed Save As leaves the
// buffer's file, and its unsaved state, as they were.
func (h *Host) saveAs(path string) error {
	if err := h.write(path); err != nil {
		return err
	}
	return h.setPath(path)
}

func (h *Host) write(path string) error {
	if err := os.WriteFile(path, []byte(h.editor.Value()), 0o644); err != nil {
		return h.failed("write failed", err)
	}
	if err := h.setDirty(false); err != nil {
		return err
	}
	return h.message(fmt.Sprintf("%q written", filepath.Base(path)))
}

// openDialog opens a dialog the layout declared, by its id — what a handler
// does with `saveDialog.open()`, done from Go.
func (h *Host) openDialog(id string) error { return h.p.Call(id, "open") }

// load reads the file, if there is one.
//
// A path that does NOT exist is not an error: it opens empty and Save creates
// it, as vim does. A path that exists but cannot be read IS an error, because
// silently showing an empty buffer for a file that is there invites
// overwriting it.
func (h *Host) load(path string) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	h.editor.SetValue(string(b))
	return nil
}

// displayPath is how the status bar names the file.
func displayPath(p string) string {
	if p == "" {
		return "[No Name]"
	}
	return filepath.Base(p)
}

// failed reports an operation that did not happen: on the status line for the
// user, AND as the handler's error. A failure that only updated the status
// returned nil, and a caller that went on to act on "success" — Save As
// adopting a path it had not written — did the damage.
func (h *Host) failed(what string, err error) error {
	return errors.Join(fmt.Errorf("%s: %w", what, err), h.message(what+": "+err.Error()))
}

// unsavedRefusal is what New and Open say over unsaved changes.
func unsavedRefusal(action string) string {
	return "unsaved changes — save them first (Ctrl+S), then " + action
}
