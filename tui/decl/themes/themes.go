// Package themes is the toolkit's four themes, as QML modules a layout
// imports:
//
//	import tui.theme.dark 1.0     // or light, mono, retro
//
//	Window {
//	    palette.window: Theme.app.window
//	    …
//	}
//
// A program offers them with one option, and switches between them the way it
// would between its own — the import line:
//
//	tuidecl.NewProgram(themes.Offer(), …)
//
// # The vocabulary
//
// Each theme is a Theme singleton of constants in five groups, one per kind of
// surface. A layout binds palette roles to them, so it never names a colour,
// and a theme never names a widget:
//
//	app       the application palette the Window sets and everything inherits:
//	          window, windowText, button, buttonText, highlight,
//	          highlightedText, base, text, mid, light, and an inactive group
//	          (highlight, highlightedText) for a view without focus
//	menu      the menu bar and its menus: window, windowText, highlight,
//	          highlightedText, accent (a menu's access key)
//	document  what shows a document — an editor, a table, a tree — and its
//	          frame: window, windowText, highlight, highlightedText, base,
//	          text; and selection, selectedText for a text selection, which an
//	          Editor wears as its own palette.highlight / highlightedText
//	status    the status line: window, windowText
//	syntax    KSyntaxHighlighting's styles, for the syntax.* roles: keyword,
//	          controlFlow, dataType, attribute, function, string, specialChar,
//	          decVal, float, baseN, constant, comment, alert, import, operator
//
// These are AutoDB's themes and its vocabulary, which the toolkit adopted as
// its own. A program needing a colour of its own — a warning, a gauge — keeps
// its themes in its own files, starting from these, and offers them with
// [tuidecl.Themes] under its own module name.
package themes

import (
	"embed"
	"io/fs"
	"strings"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

//go:embed *.qml
var files embed.FS

// Module is the prefix each theme is offered under: tui.theme.dark.
const Module = "tui.theme"

// Version is the version an import of a theme asks for.
const Version = "1.0"

// Offer offers every theme as the module Module.<name>, version Version. Only
// the one a layout imports is read.
func Offer() tuidecl.ProgramOption {
	return tuidecl.Themes(files, ".", Module, Version)
}

// FS is the themes' QML files, one per theme: dark.qml, light.qml, mono.qml,
// retro.qml — to copy from, or to offer under another module name.
func FS() fs.FS { return files }

// Names are the themes, in the order a menu lists them.
func Names() []string {
	entries, _ := fs.ReadDir(files, ".")
	var out []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".qml"); ok {
			out = append(out, name)
		}
	}
	return out
}
