package main

import (
	"embed"
	"io/fs"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
)

// THE QML THIS PROGRAM SHIPS, and the modules a document imports it through.
//
//	import editor 1.0               the App singleton          (declared)
//	import tui.theme.retro 1.0      a Theme singleton, golib's (offered)
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

// dialogFiles are the dialogs, one component file each.
//
//go:embed dialogs
var dialogFiles embed.FS

// catalogFiles are the editor's own words, one Qt TS XML catalog per language
// (i18n/editor_<language>.xml). golib's own — the dialog buttons, the file dialogs — come with
// golib.
//
//go:embed i18n
var catalogFiles embed.FS

// moduleVersion is what every import of this program's modules asks for.
const moduleVersion = "1.0"

// modules are every module a document may import.
func (h *Host) modules() []tuidecl.ProgramOption {
	return h.modulesFrom(dialogFiles)
}

// modulesFrom are the modules with the dialogs' QML read from the given file
// system: the embedded copies, or a directory on disk under -dev.
func (h *Host) modulesFrom(dialogs fs.FS) []tuidecl.ProgramOption {
	return []tuidecl.ProgramOption{
		// The `editor` module exports ONE singleton, App. Everything the
		// document can reach of this program is under that name — and nothing
		// else of it is reachable at all.
		tuidecl.Singleton("editor", moduleVersion, "App"),
		// The toolkit's four themes, as tui.theme.<name>: this program keeps
		// no colours of its own.
		themes.Offer(),
		tuidecl.Components(dialogs, "dialogs", "editor.dialogs", moduleVersion),
		// TextField and Popup, for the command prompt: Qt Quick Controls'
		// types, from golib — added the way any program adds widgets of its own.
		tuidecl.Types(controls.Types()...),
	}
}
