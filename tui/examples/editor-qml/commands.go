package main

import (
	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// THE APP SINGLETON'S COMMANDS — what the document invokes.
//
// The table is the whole of it: a handler in editor.qml or a dialog file can
// reach exactly these, by these names, and nothing else of the program.
func (h *Host) commands() map[string]func() error {
	return map[string]func() error{
		"App.newFile":    h.newFile,
		"App.openFile":   h.openFile,
		"App.saveFile":   h.saveFile,
		"App.quit":       func() error { h.quit(); return nil },
		"App.useVim":     func() error { return h.useKeyset("vim", "switched keymap to Vim (modal)") },
		"App.useNano":    func() error { return h.useKeyset("nano", "switched keymap to Nano (modeless)") },
		"App.syncStatus": h.syncStatus,
		"App.markDirty":  func() error { return h.setDirty(true) },
	}
}

// injectCommands publishes the command table as handlers.
func (h *Host) injectCommands() error {
	for name, fn := range h.commands() {
		fn := fn
		if err := h.tree.Inject(name, decl.Handle(func([]qml.SpecValue) error { return fn() })); err != nil {
			return err
		}
	}
	return nil
}

// useKeyset switches the editor's keymap through its bound App.keyset.
func (h *Host) useKeyset(ks, msg string) error {
	if _, err := h.tree.SetSources(map[string]qml.SpecValue{
		"App.keyset": str(ks),
		"App.status": str(msg),
	}); err != nil {
		return err
	}
	// A keyset switch can change the mode — Nano has no Normal mode — so the
	// status line is brought up to date with it.
	return h.syncStatus()
}
