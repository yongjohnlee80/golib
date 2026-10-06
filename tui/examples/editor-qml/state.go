package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE APP SINGLETON'S STATE — what the document reads.
//
// Each is a SOURCE, so changing one repaints exactly the bindings that read it
// and nothing else. The host changes them only through the setters below, so
// what the screen says and what the host knows cannot drift apart.

// state is App's state with its starting values; theme is the one the layout
// imports, lang the language the editor starts in.
//
// UI text is published as a catalog message (tui.Msg), not as words: the widget showing it
// looks it up in the current language, so a language switch republishes nothing. A file's
// name is content, and is published as it is.
func (h *Host) state(path, theme, lang string) map[string]any {
	st := map[string]any{
		"App.mode":   modeMessage(widget.ModeNormal),
		"App.status": statusName(path),
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
	// And which language: the one the editor starts in.
	for k, v := range languageState(lang) {
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
	return h.p.Set("App.mode", modeMessage(h.editor.Mode()))
}

// modeMessage is how the status bar names an editor mode. The mode's String is its
// identity, compared in code; what the user reads is the catalog's word for it.
func modeMessage(m widget.EditorMode) any {
	switch m {
	case widget.ModeNormal:
		return tui.Msg("editor.mode.normal")
	case widget.ModeInsert:
		return tui.Msg("editor.mode.insert")
	case widget.ModeVisual:
		return tui.Msg("editor.mode.visual")
	case widget.ModeVisualLine:
		return tui.Msg("editor.mode.visualLine")
	}
	return m.String()
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
func quitQuestion(dirty bool) tui.Message {
	if dirty {
		return tui.Msg("editor.quit.questionDirty")
	}
	return tui.Msg("editor.quit.question")
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
		"App.status": statusName(path),
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
