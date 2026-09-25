package main

import (
	"embed"
	"io/fs"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// THE QML THIS PROGRAM SHIPS, and the modules a document imports it through.
//
//	import editor 1.0               the App singleton          (declared)
//	import editor.theme.retro 1.0   a Theme singleton          (offered)
//	import editor.dialogs 1.0       QuitDialog, AboutDialog…   (offered)
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

// themeFiles are the themes, one file each: themes/retro.qml is the module
// editor.theme.retro. A new theme is a file here and nothing else.
//
//go:embed themes
var themeFiles embed.FS

// dialogFiles are the dialogs, one component file each.
//
//go:embed dialogs
var dialogFiles embed.FS

// moduleVersion is what every import of this program's modules asks for.
const moduleVersion = "1.0"

// modules are every module a document may import.
func (h *Host) modules() []tuidecl.ProgramOption {
	return h.modulesFrom(themeFiles, dialogFiles)
}

// modulesFrom are the modules with their QML read from the given file
// systems: the embedded copies, or a directory on disk under -dev.
func (h *Host) modulesFrom(themes, dialogs fs.FS) []tuidecl.ProgramOption {
	return []tuidecl.ProgramOption{
		// The `editor` module exports ONE singleton, App. Everything the
		// document can reach of this program is under that name — and nothing
		// else of it is reachable at all.
		tuidecl.Singleton("editor", moduleVersion, "App"),
		tuidecl.Themes(themes, "themes", "editor.theme", moduleVersion),
		tuidecl.Components(dialogs, "dialogs", "editor.dialogs", moduleVersion),
	}
}
