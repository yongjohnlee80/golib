package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE APP SINGLETON'S STATE — what the document reads.
//
// Each is a SOURCE, so changing one repaints exactly the bindings that read it
// and nothing else. The host changes them only through the setters below, so
// what the screen says and what the host knows cannot drift apart.

// state is App's state with its starting values; theme is the one the layout
// imports.
func (h *Host) state(path, theme string) map[string]any {
	st := map[string]any{
		"App.mode":   widget.ModeNormal.String(),
		"App.status": displayPath(path),
		"App.keyset": "vim",
		// Where the file dialogs open: the current file's folder.
		"App.folder": folderOf(path),
		// The file being edited, absolute, "" for none: where Save As starts.
		"App.path":   absPath(path),
		"App.syntax": syntaxFor(path),
		// The quit dialog's question. A source, so the dialog says when there
		// is something to lose without the host reaching into it.
		"App.quitQuestion": quitQuestion(false),
	}
	// Which theme the menu shows checked: the one the layout imports.
	for k, v := range themeState(theme) {
		st[k] = v
	}
	return st
}

// message puts a line in the status bar's centre.
func (h *Host) message(s string) error {
	return h.p.Set("App.status", s)
}

// syncStatus brings the status bar's mode up to date with the editor's.
func (h *Host) syncStatus() error {
	return h.p.Set("App.mode", h.editor.Mode().String())
}

// setDirty records whether the buffer has unsaved changes, and keeps the quit
// dialog's question saying so. Only a CHANGE is published: the editor reports
// every keystroke, and republishing an unchanged question would reevaluate its
// binding for nothing.
func (h *Host) setDirty(v bool) error {
	if h.dirty == v {
		return nil
	}
	h.dirty = v
	return h.p.Set("App.quitQuestion", quitQuestion(v))
}

// quitQuestion is what the quit dialog asks.
func quitQuestion(dirty bool) string {
	if dirty {
		return "Are you sure to quit?\nUnsaved changes will be lost."
	}
	return "Are you sure to quit?"
}

// folderOf is the folder a path is in, or the working directory for none.
func folderOf(path string) string {
	if path == "" {
		wd, _ := os.Getwd()
		return wd
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Dir(path)
	}
	return filepath.Dir(abs)
}

// absPath is a path made absolute, "" for none.
func absPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// setPath makes path the buffer's file, and moves the file dialogs' folder and
// the status line's name with it.
func (h *Host) setPath(path string) error {
	h.path = path
	return h.p.SetMany(map[string]any{
		"App.folder": folderOf(path),
		"App.path":   absPath(path),
		"App.syntax": syntaxFor(path),
		"App.status": displayPath(path),
	})
}

// syntaxFor is the highlighter a file's extension calls for: the vocabulary's
// QML definition for a .qml or .js file, none for anything else. The host
// decides it — the document cannot say "if".
func syntaxFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".qml":
		return "QML"
	case ".js", ".mjs":
		return "JavaScript"
	}
	return ""
}
