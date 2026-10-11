package qml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// qml_declaration_test.go: a node's own properties — `property`, `required
// property` and `default property alias` — recorded on the node that declares
// them, and the Qt spellings this grammar refuses, each naming what to write.

// TestADeclarationIsRecordedOnItsNode, in document order, apart from Props.
func TestADeclarationIsRecordedOnItsNode(t *testing.T) {
	tree, err := qml.QML{File: "Picker.qml"}.Parse([]byte(`Dialog {
	property string label: "Find"
	required property var model
	property list<string> names; property real ratio: -0.5
	default property alias columns: list.data
	title: label
	TableView { id: list; model: model }
}`))
	if err != nil {
		t.Fatal(err)
	}
	ds := tree.Root.Decls
	if len(ds) != 5 {
		t.Fatalf("decls = %+v, want five", ds)
	}
	type want struct {
		name, typ, def, target string
		required               bool
	}
	for i, w := range []want{
		{name: "label", typ: "string", def: "Find"},
		{name: "model", typ: "var", required: true},
		{name: "names", typ: "list<string>"},
		{name: "ratio", typ: "real", def: "-0.5"},
		{name: "columns", typ: "alias", target: "list"},
	} {
		d := ds[i]
		def := ""
		if d.Default != nil {
			def = d.Default.Raw
		}
		if d.Name != w.name || d.Type != w.typ || def != w.def || d.Target != w.target || d.Required != w.required {
			t.Errorf("decl %d = %+v (default %q), want %+v", i, d, def, w)
		}
	}
	if ds[0].Pos.Line != 2 || ds[0].Pos.File != "Picker.qml" {
		t.Errorf("label declared at %s, want Picker.qml:2", ds[0].Pos)
	}
	if len(tree.Root.Props) != 1 || tree.Root.Props[0].Name != "title" {
		t.Errorf("props = %+v, want title alone: a declaration is not a property", tree.Root.Props)
	}
	if c := tree.Root.Children; len(c) != 1 || c[0].ID != "list" || len(c[0].Decls) != 0 {
		t.Errorf("children = %+v, want the TableView, declaring nothing", c)
	}
}

// TestADeclarationWordFollowedByAColonIsAPlainProperty: one token of lookahead
// keeps `property: 3` what it always was.
func TestADeclarationWordFollowedByAColonIsAPlainProperty(t *testing.T) {
	tree, err := qml.QML{}.Parse([]byte(
		"Item { property: 3; required: true\n default: \"d\"; signal: x; readonly: false\n property { a: 1 } }"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range tree.Root.Props {
		got = append(got, p.Name)
	}
	if want := "property,required,default,signal,readonly,property.a"; strings.Join(got, ",") != want {
		t.Errorf("props = %v, want %s", got, want)
	}
	if len(tree.Root.Decls) != 0 {
		t.Errorf("decls = %+v, want none", tree.Root.Decls)
	}
}

// TestARefusedDeclarationNamesTheAlternative: Qt has these; this grammar does
// not, and says what to write instead.
func TestARefusedDeclarationNamesTheAlternative(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"property alias text: field.text", "property <type> text, used where field.text is set"},
		{"required property alias text: field.text", "property <type> text, used where field.text is set"},
		{"readonly property int n: 1", "property <type> <name> without readonly"},
		{"signal picked(int index)", "a call to an App handler with a declared property's value"},
		{"property thing x", "a property type: string, int, real, double, bool, var, url, color or list<T>"},
		{"required property string x: \"a\"", "no value for a required property"},
		{"default property string x", "default property alias <name>: <id>.data"},
		{"default property alias x: list.model", "the use site's children go to an object's data"},
		{"property string Label", "a property name starting with a lower-case letter"},
		{"property string onClicked", "a property name starting with a lower-case letter"},
		{"required string x", "property after required"},
	} {
		_, err := qml.QML{}.Parse([]byte("Dialog {\n " + c.src + "\n}"))
		var se parse.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Want, c.want) {
			t.Errorf("%s: err = %v, want a syntax error wanting %q", c.src, err, c.want)
			continue
		}
		if se.Incomplete || se.Pos.Line != 2 {
			t.Errorf("%s: placed at %s (incomplete %v), want line 2", c.src, se.Pos, se.Incomplete)
		}
	}
}

// TestAHalfWrittenDeclarationIsIncomplete, as every other construct is, so a
// reload holds the last good tree.
func TestAHalfWrittenDeclarationIsIncomplete(t *testing.T) {
	for _, src := range []string{"Dialog { property", "Dialog { property string", "Dialog { property list<",
		"Dialog { default property alias c:", "Dialog { property string a: "} {
		_, err := qml.QML{}.Parse([]byte(src))
		var se parse.SyntaxError
		if !errors.As(err, &se) || !se.Incomplete {
			t.Errorf("%q: err = %v, want Incomplete", src, err)
		}
	}
}
