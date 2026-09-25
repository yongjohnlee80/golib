package main

import (
	"embed"

	"github.com/yongjohnlee80/golib/decl"
)

// THE QML THIS PROGRAM SHIPS, and the modules a document imports it through.
//
//	import editor 1.0               the App singleton          (declared)
//	import editor.theme.retro 1.0   a Theme singleton          (offered)
//	import editor.dialogs 1.0       QuitDialog, AboutDialog    (offered)
//
// App is DECLARED: it is this program, and every document needs it. The
// themes and the dialogs are OFFERED: importable, and read only when an import
// line names them — so only the one theme the layout imports is ever parsed.

// layout is the screen, in QML. The Go never builds a widget: it publishes
// what the document may reach and reacts when the document says something
// happened.
//
//go:embed editor.qml
var layout []byte

//go:embed themes
var themeFiles embed.FS

// dialogFiles are the dialogs, one component file each.
//
//go:embed dialogs
var dialogFiles embed.FS

// moduleVersion is what every import of this program's modules asks for.
const moduleVersion = "1.0"

// themes maps each theme module to its file. A new theme is a file under
// themes/ and a row here.
var themes = map[string]string{
	"editor.theme.mono":  "themes/mono.qml",
	"editor.theme.retro": "themes/retro.qml",
}

// declareModules registers every module a document may import.
func (h *Host) declareModules() error {
	// The `editor` module exports ONE singleton, App. Everything the document
	// can reach of this program is under that name — and nothing else of it is
	// reachable at all.
	if err := h.tree.DeclareModule(decl.Module{
		Name: "editor", Version: moduleVersion, Exports: []string{"App"},
	}); err != nil {
		return err
	}
	for module, file := range themes {
		if err := h.tree.OfferModule(module, moduleVersion, themeLoader(file)); err != nil {
			return err
		}
	}
	return h.tree.OfferModule("editor.dialogs", moduleVersion, decl.ComponentFiles(dialogFiles, "dialogs"))
}

// themeLoader reads a theme file when its module is imported, and not before.
func themeLoader(file string) decl.ModuleLoader {
	return func() (decl.ModuleContents, error) {
		src, err := themeFiles.ReadFile(file)
		if err != nil {
			return decl.ModuleContents{}, err
		}
		return decl.ValueModule(src)()
	}
}
