// Package tiles is the toolkit's tiled panel, TilePanel, as a QML component module a layout imports:
//
//	import tui.tiles 1.0
//
//	TileManager {
//	    TilePanel { id: files; heading: "Files"; edge: Tui.Left; edges: "left right"; size: 25; TreeView { … } }
//	    TilePanel { id: page; heading: "Query"; edge: Tui.Center; Editor { … } }
//	}
//
// A TilePanel is a Tile of a TileManager (tui/decl's Tiles) framed under its heading, with a
// FocusScope for its keys: the tiled counterpart of tui.panels' Panel, which floats. It shares the
// screen and its borders with the other tiles: Alt/Option-right drag moves the nearest shared border,
// Alt/Option-left drag re-docks or swaps it, and a dock hides and shows it. It binds
// Theme.document.base, so the program imports one of the toolkit's themes (tui/decl/themes), and it
// is minimizable, so the program gives the adapter a window collector:
//
//	tuidecl.NewProgram(tiles.Offer(), themes.Offer(),
//	    tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())), …)
//
// It is its own module, not one of tui.panels': a module's components are each judged on their own
// where a layout doesn't use them, and a panel with no content to frame can't be, so a program offers
// the panel it uses. Where a panel sits and how big it is are preferences the program keeps;
// tui/panelgeometry is their keys, their shape and their check.
package tiles

import (
	"embed"
	"io/fs"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

//go:embed TilePanel.qml
var files embed.FS

// Module is the module a layout imports TilePanel from.
const Module = "tui.tiles"

// Version is the version an import of Module asks for.
const Version = "1.0"

// Offer offers TilePanel as the module Module, version Version.
func Offer() tuidecl.ProgramOption { return tuidecl.Components(files, ".", Module, Version) }

// FS is the module's QML: TilePanel.qml, to copy from or to offer under another module name.
func FS() fs.FS { return files }
