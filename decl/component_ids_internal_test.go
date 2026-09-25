package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// TestScopeIDsRewritesEveryReferenceForm: a component's own ids are renamed in
// every place a document can mention one — a reference, a call and its
// arguments, an expression, a handler's statements — and nothing else is.
func TestScopeIDsRewritesEveryReferenceForm(t *testing.T) {
	src := `Box {
	id: me
	title: field.text
	hint: format(field.text, other.text)
	wide: field.width > 3
	Text { id: field; onClicked: { if (field.ok) { me.close() } else { App.fail(field.text) } } }
	onOpened: { let x = field.text; App.go(x, me.title, other.name) }
}`
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	orig := spec.Root.Props[0].Value.Raw
	out := scopeIDs(spec.Root, 7, "")
	var b strings.Builder
	var dump func(sn *qml.SpecNode)
	dump = func(sn *qml.SpecNode) {
		b.WriteString(sn.ID + "|")
		for _, p := range sn.Props {
			b.WriteString(p.Value.Raw + "|")
			for _, a := range p.Value.Args {
				b.WriteString(a.Raw + "|")
			}
			if p.Value.Expr != nil {
				b.WriteString(p.Value.Expr.Left.Left.Raw + "|")
			}
		}
		for _, c := range sn.Children {
			dump(c)
		}
	}
	dump(out)
	got := b.String()
	for _, want := range []string{"me@7|", "field@7.text|", "format|", "field@7|"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "other@7") {
		t.Errorf("an id the component does not declare was renamed: %s", got)
	}
	if spec.Root.Props[0].Value.Raw != orig {
		t.Error("scopeIDs changed the component it was given; it must copy")
	}
	// The handlers' statements: the if/else branches and the declaration.
	body := out.Children[0].Handlers[0].Body[0]
	then := body.Then.Body[0].Value.Left.Left.Raw
	els := body.Else.Body[0].Value.Args[0].Left.Raw
	decl := out.Handlers[0].Body[0].Decls[0].Init.Left.Raw
	if then != "me@7" || els != "field@7" || decl != "field@7" {
		t.Errorf("statements not rewritten: then %q, else %q, let %q", then, els, decl)
	}
	// The root takes the use site's name when it has one.
	if named := scopeIDs(spec.Root, 8, "dialog"); named.ID != "dialog" || named.Children[0].ID != "field@8" {
		t.Errorf("named use: root %q, child %q", named.ID, named.Children[0].ID)
	}
}
