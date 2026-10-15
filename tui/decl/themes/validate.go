package themes

import (
	"errors"
	"fmt"
	"slices"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Role is a required colour in a complete toolkit Theme, and the surface it colours.
type Role struct{ Key, Description string }

// Schema describes complete themes. Every role requires a string accepted by style.ParseColor.
// The returned slice is a copy; editing it never changes validation's vocabulary.
func Schema() []Role { return slices.Clone(roles) }

var roles = []Role{
	{"app.window", "application chrome background"},
	{"app.windowText", "application chrome text"},
	{"app.button", "button background"},
	{"app.buttonText", "button text"},
	{"app.highlight", "focused control highlight background"},
	{"app.highlightedText", "focused highlighted control text"},
	{"app.base", "default content background"},
	{"app.text", "default content text"},
	{"app.inactive.highlight", "unfocused control highlight background"},
	{"app.inactive.highlightedText", "unfocused highlighted control text"},
	{"app.mid", "muted borders and separators"},
	{"app.light", "emphasized borders and separators"},
	{"app.backdrop", "screen around the centred document page"},
	{"menu.window", "menu background"},
	{"menu.windowText", "menu text"},
	{"menu.highlight", "selected menu entry background"},
	{"menu.highlightedText", "selected menu entry text"},
	{"menu.accent", "menu and dialog access keys"},
	{"document.window", "document frame background"},
	{"document.windowText", "document frame text"},
	{"document.highlight", "selected document row background"},
	{"document.highlightedText", "selected document row text"},
	{"document.base", "editor and document preview background"},
	{"document.text", "editor and document preview text"},
	{"document.selection", "editor text-selection background"},
	{"document.selectedText", "editor selected text"},
	{"document.cursor", "editor and page-following terminal cursor"},
	{"document.lineNumber", "editor line numbers"},
	{"status.window", "status-line background"},
	{"status.windowText", "status-line text"},
	{"syntax.keyword", "syntax keywords"},
	{"syntax.controlFlow", "syntax control-flow keywords"},
	{"syntax.dataType", "syntax type names"},
	{"syntax.attribute", "syntax attributes"},
	{"syntax.function", "syntax function names"},
	{"syntax.string", "syntax strings"},
	{"syntax.specialChar", "syntax escaped and special characters"},
	{"syntax.decVal", "syntax decimal integers"},
	{"syntax.float", "syntax floating-point numbers"},
	{"syntax.baseN", "syntax nondecimal numbers"},
	{"syntax.constant", "syntax constants"},
	{"syntax.comment", "syntax comments"},
	{"syntax.alert", "syntax alerts and diagnostic notices"},
	{"syntax.import", "syntax imports"},
	{"syntax.operator", "syntax operators"},
}

// Diagnostic names one independently checkable problem. Missing keys point at the Theme root.
type Diagnostic struct {
	Pos     parse.Position
	Key     string
	Message string
}

func (d Diagnostic) Error() string { return fmt.Sprintf("%s: %s: %s", d.Pos, d.Key, d.Message) }

// Validate reads a complete constant Theme with the QML parser and the toolkit's colour reader.
// It reports all semantic problems, or the parser's positioned syntax failure. Values are nil
// when anything is wrong: consumers cannot accidentally install a partial palette.
func Validate(file string, src []byte) (map[string]string, []Diagnostic) {
	tree, err := (qml.QML{File: file}).Parse(src)
	if err != nil {
		pos := parse.Position{File: file, Line: 1, Column: 1}
		var syntax parse.SyntaxError
		if errors.As(err, &syntax) {
			pos = syntax.Pos
		}
		return nil, []Diagnostic{{pos, "Theme", err.Error()}}
	}
	n := tree.Root
	if n == nil || n.Type != "Theme" {
		return nil, []Diagnostic{{parse.Position{File: file, Line: 1, Column: 1}, "Theme", "root must be Theme"}}
	}
	var ds []Diagnostic
	add := func(pos parse.Position, key, message string) { ds = append(ds, Diagnostic{pos, key, message}) }
	if len(tree.Imports) != 0 {
		add(n.Pos, "Theme", "imports are not allowed in a constant theme")
	}
	if n.ID != "" {
		add(n.Pos, "id", "a constant theme has no id")
	}
	for _, h := range n.Handlers {
		add(h.Pos, "Theme", "handlers are not allowed in a constant theme")
	}
	for _, d := range n.Decls {
		add(d.Pos, d.Name, "property declarations are not allowed; write the colour key directly")
	}
	for _, c := range n.Children {
		add(c.Pos, c.Type, "child objects are not allowed; use grouped colour properties")
	}
	known := make(map[string]bool, len(roles))
	for _, r := range roles {
		known[r.Key] = true
	}
	seen := make(map[string]bool, len(roles))
	values := make(map[string]string, len(roles))
	for _, p := range n.Props {
		if seen[p.Name] {
			add(p.Pos, p.Name, "key is repeated")
		}
		seen[p.Name] = true
		if !known[p.Name] {
			add(p.Pos, p.Name, "unknown colour key")
		}
		if p.Value.Kind != qml.SpecValueString {
			add(p.Pos, p.Name, "colour must be a quoted string constant")
			continue
		}
		if _, err := style.ParseColor(p.Value.Raw); err != nil {
			add(p.Pos, p.Name, err.Error())
			continue
		}
		values[p.Name] = p.Value.Raw
	}
	for _, r := range roles {
		if !seen[r.Key] {
			add(n.Pos, r.Key, "required colour key is missing")
		}
	}
	if len(ds) != 0 {
		return nil, ds
	}
	return values, nil
}
