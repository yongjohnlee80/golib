package decl

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// TestSubstituteReplacesEveryReferenceForm: a declared property is replaced in
// every place a component can mention one — a property's whole value, through
// the root's id, a member read off it, a call's arguments, an expression, a
// handler's statements — and nothing else is: a member NAMED like it, or
// another object's member, is not a reference.
func TestSubstituteReplacesEveryReferenceForm(t *testing.T) {
	src := `Box {
	id: me
	property string label: "dflt"
	property real n: 1
	property var rows: App.none
	title: label
	alias: me.label
	size: label.length
	hint: fmt(label, other.label, n)
	wide: n > 3
	model: rows
	Text { text: x.label; onClicked: { if (me.label) { App.go(label, x.label) } else { let y = n; App.no(y, rows) } } }
}`
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	use, err := qml.QML{}.Parse([]byte(`Card { label: "mine"; n: -2; rows: App.rows.all }`))
	if err != nil {
		t.Fatal(err)
	}
	def := scopeIDs(spec.Root, "7", "card")
	out, err := substitute(def, use.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Decls) != 0 {
		t.Errorf("the copy still declares %v", out.Decls)
	}
	prop := func(sn *qml.SpecNode, name string) qml.SpecValue {
		for _, p := range sn.Props {
			if p.Name == name {
				return p.Value
			}
		}
		t.Fatalf("no %s", name)
		return qml.SpecValue{}
	}
	for name, want := range map[string]string{"title": "mine", "alias": "mine", "model": "App.rows.all"} {
		if got := prop(out, name).Raw; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	// A member of the value: the string's own length, kept as an expression.
	if size := prop(out, "size"); size.Kind != qml.SpecValueExpr || size.Expr.Kind != js.ExprMember ||
		size.Expr.Left.Kind != js.ExprString || size.Expr.Left.Raw != "mine" || size.Expr.Name != "length" {
		t.Errorf("size = %+v, want \"mine\".length", size)
	}
	// A call: the declared names replaced; another object's member is not.
	hint := prop(out, "hint")
	if hint.Kind != qml.SpecValueCall || hint.Args[0].Raw != "mine" || hint.Args[1].Raw != "other.label" ||
		hint.Args[2].Kind != qml.SpecValueNumber || hint.Args[2].Raw != "-2" {
		t.Errorf("hint = %+v, want fmt(\"mine\", other.label, -2)", hint)
	}
	// An expression: the number, written back as JavaScript writes it.
	wide := prop(out, "wide").Expr
	if wide.Left.Kind != js.ExprUnary || wide.Left.Raw != "-" || wide.Left.Left.Raw != "2" {
		t.Errorf("wide's left = %+v, want -2", wide.Left)
	}
	// A member NAMED label on another object is not a reference.
	if got := prop(out.Children[0], "text").Raw; got != "x.label" {
		t.Errorf("text = %q, want x.label untouched", got)
	}
	// The handler's statements: the condition, both branches, the declaration.
	body := out.Children[0].Handlers[0].Body[0]
	cond := body.Cond
	then := body.Then.Body[0].Value.Args[0]
	if member := body.Then.Body[0].Value.Args[1]; member.Kind != js.ExprMember || member.Left.Raw != "x" ||
		member.Name != "label" {
		t.Errorf("x.label in a handler = %+v, want it untouched: a member's name is not a reference", member)
	}
	decl := body.Else.Body[0].Decls[0].Init
	rows := body.Else.Body[1].Value.Args[1]
	if cond.Kind != js.ExprString || cond.Raw != "mine" || then.Raw != "mine" ||
		decl.Kind != js.ExprUnary || rows.Kind != js.ExprMember || rows.Name != "all" ||
		rows.Left.Name != "rows" || rows.Left.Left.Raw != "App" {
		t.Errorf("statements: if %+v, then %+v, let %+v, rows %+v", cond, then, decl, rows)
	}
	// The component it was given is unchanged: substitute copies.
	if spec.Root.Props[0].Value.Raw != "label" || def.Props[0].Value.Raw != "label" ||
		def.Children[0].Handlers[0].Body[0].Cond.Kind != js.ExprMember || len(def.Decls) != 3 {
		t.Error("substitute changed the component it was given; it must copy")
	}
}

// TestSubstituteTakesAnObjectWholeOnly: a template given for a property is
// that property's whole value; in an expression or a handler it is refused.
func TestSubstituteTakesAnObjectWholeOnly(t *testing.T) {
	use, err := qml.QML{}.Parse([]byte(`Card { item: Text { text: "t" } }`))
	if err != nil {
		t.Fatal(err)
	}
	for src, ok := range map[string]bool{
		"Box {\n required property var item\n delegate: item\n}":          true,
		"Box {\n required property var item\n hint: fmt(item)\n}":         true,
		"Box {\n required property var item\n wide: !item\n}":             false,
		"Box {\n required property var item\n onClicked: App.go(item)\n}": false,
		"Box {\n required property var item\n size: item.width\n}":        false,
	} {
		spec, err := qml.QML{}.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		out, err := substitute(spec.Root, use.Root)
		switch {
		case ok && err != nil:
			t.Errorf("%q: %v", src, err)
		case !ok && (!errors.Is(err, ErrComponent) || !strings.Contains(err.Error(), "an expression or a handler cannot")):
			t.Errorf("%q: err = %v, want the template refused", src, err)
		case ok && strings.Contains(src, "delegate") && out.Props[0].Value.Kind != qml.SpecValueTemplate:
			t.Errorf("delegate = %+v, want the template", out.Props[0].Value)
		}
	}
}
