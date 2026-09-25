package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// THE BUFFER'S FILE — the commands that read and write it.

func (h *Host) newFile() error {
	h.editor.SetValue("")
	h.path = ""
	if err := h.setDirty(false); err != nil {
		return err
	}
	return h.message("new buffer")
}

// openFile is not wired to a dialog yet. It says so, rather than doing nothing.
func (h *Host) openFile() error { return h.message("File → Open: not implemented yet") }

func (h *Host) saveFile() error {
	if h.path == "" {
		return h.message("no file name — nothing written")
	}
	if err := os.WriteFile(h.path, []byte(h.editor.Value()), 0o644); err != nil {
		return h.message("write failed: " + err.Error())
	}
	if err := h.setDirty(false); err != nil {
		return err
	}
	return h.message(fmt.Sprintf("%q written", h.path))
}

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
