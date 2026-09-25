package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// rowModel is a one-row model for bindRow.
type rowModel map[string]qml.SpecValue

func (m rowModel) RowCount(*Index) int                     { return 1 }
func (m rowModel) Data(_ Index, role string) qml.SpecValue { return m[role] }
func (m rowModel) Key(Index) string                        { return "k" }
func (m rowModel) Subscribe(func(Change)) func()           { return func() {} }

// TestBindRowWritesTheRowIntoEveryForm: `model.<role>` and `index` become the
// row's typed values in a property, a call's arguments, an expression, and a
// handler's statements — and not inside a nested Repeater's delegate, where
// `model` is that Repeater's own row.
func TestBindRowWritesTheRowIntoEveryForm(t *testing.T) {
	src := `Box {
	title: model.name
	hint: fmt(model.n, index)
	wide: model.n > 3
	onClicked: { if (model.on) { App.go(model.name) } else { App.stop(index) } }
	onOpened: { let x = model.n; App.run(x) }
	Repeater { model: model.kids; Text { text: model.name } }
}`
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	m := rowModel{
		"name": {Kind: qml.SpecValueString, Raw: "ann"},
		"n":    {Kind: qml.SpecValueNumber, Raw: "7"},
		"on":   {Kind: qml.SpecValueBool, Raw: "true"},
		"kids": {Kind: qml.SpecValueObject, Obj: rowModel{}},
	}
	out := bindRow(spec.Root, m, Index{Row: 4})
	if v := out.Props[0].Value; v.Kind != qml.SpecValueString || v.Raw != "ann" {
		t.Errorf("title = %+v, want the string ann", v)
	}
	if a := out.Props[1].Value.Args; a[0].Kind != qml.SpecValueNumber || a[0].Raw != "7" || a[1].Raw != "4" {
		t.Errorf("call args = %+v, want 7 and index 4", a)
	}
	if e := out.Props[2].Value.Expr; e.Left.Kind != js.ExprNumber || e.Left.Raw != "7" {
		t.Errorf("expression left = %+v, want the number 7", e.Left)
	}
	body := out.Handlers[0].Body[0]
	if c := body.Cond; c.Kind != js.ExprBool || c.Raw != "true" {
		t.Errorf("if condition = %+v, want true", c)
	}
	if a := body.Then.Body[0].Value.Args[0]; a.Kind != js.ExprString || a.Raw != "ann" {
		t.Errorf("then argument = %+v, want ann", a)
	}
	if a := body.Else.Body[0].Value.Args[0]; a.Kind != js.ExprNumber || a.Raw != "4" {
		t.Errorf("else argument = %+v, want index 4", a)
	}
	if d := out.Handlers[1].Body[0].Decls[0].Init; d.Raw != "7" {
		t.Errorf("let initialiser = %+v, want 7", d)
	}
	rep := out.Children[0]
	if v := rep.Props[0].Value; v.Kind != qml.SpecValueObject {
		t.Errorf("the nested Repeater's model = %+v, want the outer row's object", v)
	}
	if v := rep.Children[0].Props[0].Value; v.Kind != qml.SpecValueRef || v.Raw != "model.name" {
		t.Errorf("the nested delegate's model.name was bound by the OUTER row: %+v", v)
	}
	if spec.Root.Props[0].Value.Kind != qml.SpecValueRef {
		t.Error("bindRow changed the delegate it was given; it must copy")
	}
}
