package main

import (
	"os"
	"path/filepath"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE APP SINGLETON'S STATE — what the document reads.
//
// Each is a SOURCE, so changing one repaints exactly the bindings that read it
// and nothing else. The host changes them only through the setters below, so
// what the screen says and what the host knows cannot drift apart.

// injectState publishes App's state with its starting values.
func (h *Host) injectState(path string) error {
	for name, v := range map[string]string{
		"App.mode":   widget.ModeNormal.String(),
		"App.status": displayPath(path),
		"App.keyset": "vim",
		// Where the file dialogs open: the current file's folder.
		"App.folder": folderOf(path),
		// The file being edited, absolute, "" for none: where Save As starts.
		"App.path": absPath(path),
		// The quit dialog's question. A source, so the dialog says when there
		// is something to lose without the host reaching into it.
		"App.quitQuestion": quitQuestion(false),
	} {
		if err := h.tree.Inject(name, decl.SourceValue(str(v))); err != nil {
			return err
		}
	}
	return nil
}

// message puts a line in the status bar's centre.
func (h *Host) message(s string) error {
	_, err := h.tree.SetSource("App.status", str(s))
	return err
}

// syncStatus brings the status bar's mode up to date with the editor's.
func (h *Host) syncStatus() error {
	_, err := h.tree.SetSource("App.mode", str(h.editor.Mode().String()))
	return err
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
	_, err := h.tree.SetSource("App.quitQuestion", str(quitQuestion(v)))
	return err
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
	_, err := h.tree.SetSources(map[string]qml.SpecValue{
		"App.folder": str(folderOf(path)),
		"App.path":   str(absPath(path)),
		"App.status": str(displayPath(path)),
	})
	return err
}

func str(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueString, Raw: s} }
