// Package panels is the toolkit's docked panel, Panel, as a QML component module a layout imports:
//
//	import tui.panels 1.0
//
//	Panel {
//	    id: explorer
//	    heading: "Files"
//	    edge: App.explorerEdge
//	    TreeView { … }
//	}
//
// A Panel is a non-modal Drawer: movable and resizable with Alt/Option drags, minimizable so a dock can
// hide it, framed under its heading, with a FocusScope for its keys. It binds Theme.document.base, so
// the program imports one of the toolkit's themes (tui/decl/themes), and it is minimizable, so the
// program gives the adapter a window collector:
//
//	tuidecl.NewProgram(panels.Offer(), themes.Offer(),
//	    tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())), …)
//
// Where a panel sits and how big it is are preferences the program keeps; tui/panelgeometry is their
// keys, their shape and their check.
package panels

import (
	"embed"
	"io/fs"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

//go:embed Panel.qml
var files embed.FS

// Module is the module a layout imports Panel from.
const Module = "tui.panels"

// Version is the version an import of Module asks for.
const Version = "1.0"

// Offer offers Panel as the module Module, version Version.
func Offer() tuidecl.ProgramOption { return tuidecl.Components(files, ".", Module, Version) }

// FS is the module's QML: Panel.qml, to copy from or to offer under another module name.
func FS() fs.FS { return files }
