package main

import (
	"fmt"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// THE APP SINGLETON'S COMMANDS — what the document invokes.
//
// The table is the whole of it: a handler in editor.qml or a dialog file can
// reach exactly these, by these names, and nothing else of the program. Each
// says what it takes: nothing, or a path — `App.openFile(selectedFile)`.
func (h *Host) commands() map[string]decl.HandlerFunc {
	return map[string]decl.HandlerFunc{
		"App.newFile":    none(h.newFile),
		"App.openFile":   onePath("App.openFile", h.openFile),
		"App.saveFile":   none(h.saveFile),
		"App.saveAs":     onePath("App.saveAs", h.saveAs),
		"App.quit":       none(func() error { h.quit(); return nil }),
		"App.useVim":     none(func() error { return h.useKeyset("vim", "switched keymap to Vim (modal)") }),
		"App.useNano":    none(func() error { return h.useKeyset("nano", "switched keymap to Nano (modeless)") }),
		"App.syncStatus": none(h.syncStatus),
		"App.markDirty":  none(func() error { return h.setDirty(true) }),
	}
}

// injectCommands publishes the command table as handlers.
func (h *Host) injectCommands() error {
	for name, fn := range h.commands() {
		if err := h.tree.Inject(name, decl.Handle(fn)); err != nil {
			return err
		}
	}
	return nil
}

// none is a command that takes no arguments, and refuses any it is given.
func none(fn func() error) decl.HandlerFunc {
	return func(args []qml.SpecValue) error {
		if len(args) > 0 {
			return fmt.Errorf("takes no arguments, and was given %d", len(args))
		}
		return fn()
	}
}

// onePath is a command that takes one path.
func onePath(name string, fn func(string) error) decl.HandlerFunc {
	return func(args []qml.SpecValue) error {
		if len(args) != 1 || args[0].Kind != qml.SpecValueString {
			return fmt.Errorf("%s takes one path", name)
		}
		return fn(args[0].Raw)
	}
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
