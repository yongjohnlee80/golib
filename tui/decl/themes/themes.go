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
//	          highlightedText, base, text, mid, light, an inactive group
//	          (highlight, highlightedText) for a view without focus, and
//	          backdrop, a dimmed tone of the document's base for a Window's
//	          color — the screen around a page narrower than it
//	menu      the menu bar and its menus: window, windowText, highlight,
//	          highlightedText, accent (a menu's access key)
//	document  what shows a document — an editor, a table, a tree — and its
//	          frame: window, windowText, highlight, highlightedText, base,
//	          text; selection, selectedText for a text selection, which an
//	          Editor wears as its own palette.highlight / highlightedText; and
//	          cursor, lineNumber for an Editor's cursorColor and lineNumberColor
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
	"fmt"
	"io/fs"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
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

// Values are a theme's colours by their dotted names, as its file writes them:
// "app.window" → "white", "app.inactive.highlight" → "brightblack". It is for
// a program handing the theme to something outside its layout, a plugin
// drawing in its own window, say; [style.ParseColor] reads each value. An
// unknown theme, or a file that is not a Theme of strings, is an error.
func Values(name string) (map[string]string, error) { return values(files, name) }

func values(fsys fs.FS, name string) (map[string]string, error) {
	src, err := fs.ReadFile(fsys, name+".qml")
	if err != nil {
		return nil, fmt.Errorf("themes: no theme %q (want one of %s)", name, strings.Join(Names(), ", "))
	}
	tree, err := qml.QML{File: name + ".qml"}.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("themes: %w", err)
	}
	if tree.Root == nil || tree.Root.Type != "Theme" {
		return nil, fmt.Errorf("themes: %s.qml is not a Theme", name)
	}
	out := make(map[string]string, len(tree.Root.Props))
	for _, p := range tree.Root.Props {
		if p.Value.Kind != qml.SpecValueString {
			return nil, fmt.Errorf("themes: %s is not a string (at %s)", p.Name, p.Pos)
		}
		out[p.Name] = p.Value.Raw
	}
	return out, nil
}

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
