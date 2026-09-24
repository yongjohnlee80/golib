package parse_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// TestADottedPropertyNameCarriesItsPath — ADR-parse-0002 ledger, grouped and
// attached properties.
//
// The parser records the spelling and rules on NEITHER kind. QML tells a
// grouped property from an attached one by whether the first segment is
// capitalised, but which a given name *means* depends on what a consumer has
// registered — and that is not knowledge a parser has.
func TestADottedPropertyNameCarriesItsPath(t *testing.T) {
	for _, c := range []struct {
		src  string
		name string
		path []string
	}{
		{`Text { text: "x" }`, "text", []string{"text"}},
		{`Text { font.bold: true }`, "font.bold", []string{"font", "bold"}},
		{`Button { Layout.fillWidth: true }`, "Layout.fillWidth", []string{"Layout", "fillWidth"}},
		{`Text { a.b.c: 1 }`, "a.b.c", []string{"a", "b", "c"}},
	} {
		tree, err := parse.QML{}.Parse([]byte(c.src))
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		p := tree.Root.Props[0]
		if p.Name != c.name || strings.Join(p.Path, ".") != strings.Join(c.path, ".") {
			t.Errorf("%s -> name=%q path=%v, want %q/%v", c.src, p.Name, p.Path, c.name, c.path)
		}
		if p.Grouped {
			t.Errorf("%s: a dotted name is not a block", c.src)
		}
	}
}

// TestAGroupedBlockMeansTheSameAsTheDottedSpelling.
//
// `font { bold: true }` and `font.bold: true` produce the SAME Path, because
// they mean the same thing. Grouped records which was written — a consumer that
// formats or round-trips would otherwise rewrite one into the other, silently.
func TestAGroupedBlockMeansTheSameAsTheDottedSpelling(t *testing.T) {
	block, err := parse.QML{}.Parse([]byte(`Text { font { bold: true } }`))
	if err != nil {
		t.Fatal(err)
	}
	dotted, err := parse.QML{}.Parse([]byte(`Text { font.bold: true }`))
	if err != nil {
		t.Fatal(err)
	}
	b, d := block.Root.Props[0], dotted.Root.Props[0]
	if strings.Join(b.Path, ".") != strings.Join(d.Path, ".") {
		t.Errorf("paths differ: block %v, dotted %v", b.Path, d.Path)
	}
	if !b.Grouped || d.Grouped {
		t.Errorf("Grouped should distinguish the spellings: block=%v dotted=%v", b.Grouped, d.Grouped)
	}
}

// TestNestedGroupsAccumulateTheirPrefix.
func TestNestedGroupsAccumulateTheirPrefix(t *testing.T) {
	tree, err := parse.QML{}.Parse([]byte(`Text { font { style { weight: 700 } bold: true } }`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range tree.Root.Props {
		got = append(got, p.Name)
	}
	want := []string{"font.style.weight", "font.bold"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCapitalisationSeparatesAChildNodeFromAGroupedProperty.
//
// `Text { }` is a child node and `font { }` is a grouped property, and the ONLY
// thing separating them is QML's convention that types are capitalised. A
// parser that dispatched on the brace alone would read every grouped block as a
// node with a lower-case type name — which is the error this pins.
func TestCapitalisationSeparatesAChildNodeFromAGroupedProperty(t *testing.T) {
	tree, err := parse.QML{}.Parse([]byte(`Flex { Text { text: "x" } font { bold: true } }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root.Children) != 1 || tree.Root.Children[0].Type != "Text" {
		t.Errorf("children = %v, want one Text", tree.Root.Children)
	}
	if len(tree.Root.Props) != 1 || tree.Root.Props[0].Name != "font.bold" {
		t.Errorf("props = %v, want font.bold", tree.Root.Props)
	}
}

// TestAnAttachedHandlerCarriesItsPath.
//
// `Component.onCompleted` is a handler, and the signal name is in the LAST
// segment. Reading the prefix for the `on` convention — which the dotted name
// starts with `Component`, not `on` — would classify it as an ordinary
// property and lose the handler entirely.
func TestAnAttachedHandlerCarriesItsPath(t *testing.T) {
	tree, err := parse.QML{}.Parse([]byte(`Item { Component.onCompleted: go() }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root.Handlers) != 1 {
		t.Fatalf("handlers = %v; an attached handler was read as a property", tree.Root.Handlers)
	}
	h := tree.Root.Handlers[0]
	if h.Signal != "completed" {
		t.Errorf("signal = %q, want completed", h.Signal)
	}
	v := h.Body[0].Value
	if len(h.Body) != 1 || v == nil || v.Kind != parse.ExprCall || v.Left.Raw != "go" {
		t.Errorf("body = %+v, want the call to go", h.Body)
	}
	if strings.Join(h.Path, ".") != "Component.onCompleted" {
		t.Errorf("path = %v, want the full attached spelling", h.Path)
	}
}

// TestIdMustBeAPlainNameAndAPlainValue.
//
// `id` addresses the node. A dotted `id.x` addresses nothing, and an `id` whose
// value is a chain could change between reloads — an identity that moves is not
// an identity.
func TestIdMustBeAPlainNameAndAPlainValue(t *testing.T) {
	q := parse.QML{}
	for _, src := range []string{`Text { id.x: y }`, `Text { id: a.b }`} {
		if _, err := q.Parse([]byte(src)); err == nil {
			t.Errorf("%s was accepted", src)
		}
	}
	if _, err := q.Parse([]byte(`Text { id: root }`)); err != nil {
		t.Errorf("a plain id was refused: %v", err)
	}
}

// TestAnUnclosedGroupIsIncomplete — ADR-parse-0002 Q5.
//
// A watcher catching a half-written save must hold the last good tree rather
// than blank the screen, and that depends on every new construct reporting
// Incomplete rather than a generic error.
func TestAnUnclosedGroupIsIncomplete(t *testing.T) {
	_, err := parse.QML{}.Parse([]byte(`Text { font { bold: true`))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a SyntaxError", err)
	}
	if !se.Incomplete {
		t.Errorf("an unclosed group is not Incomplete: want=%q", se.Want)
	}
	if !strings.Contains(se.Want, "}") {
		t.Errorf("the error does not say what is missing: %q", se.Want)
	}
}

// TestAPropertyValueIsAJavaScriptExpression.
//
// QML property values ARE JavaScript, so this parser reads all of it. The kinds
// this format names — string, number, bool, token, ref, call — are a PROJECTION
// for consumers that ask about them constantly, not a smaller grammar. Anything
// the projection does not name keeps its tree instead of being refused, so an
// engine that cannot evaluate an expression declines it by name and position
// rather than the parser pretending the syntax is wrong.
func TestAPropertyValueIsAJavaScriptExpression(t *testing.T) {
	cases := []struct {
		name string
		src  string
		kind parse.SpecValueKind
		// exprKind is checked only for values that keep their tree.
		exprKind parse.ExprKind
		raw      string
	}{
		{name: "a string projects", src: `"hi"`, kind: parse.SpecValueString, raw: "hi"},
		{name: "a number projects", src: `12.5`, kind: parse.SpecValueNumber, raw: "12.5"},
		{name: "a negative number folds its sign", src: `-3`, kind: parse.SpecValueNumber, raw: "-3"},
		{name: "a bool projects", src: `false`, kind: parse.SpecValueBool, raw: "false"},
		{name: "a token projects", src: `@surface`, kind: parse.SpecValueToken, raw: "surface"},
		{name: "a name projects", src: `greeting`, kind: parse.SpecValueRef, raw: "greeting"},
		{name: "a member chain projects", src: `parent.width`, kind: parse.SpecValueRef, raw: "parent.width"},
		{name: "a call projects", src: `f(1)`, kind: parse.SpecValueCall, raw: "f"},
		{name: "a qualified call projects", src: `math.max(1, 2)`, kind: parse.SpecValueCall, raw: "math.max"},

		{name: "arithmetic keeps its tree", src: `parent.width / 2`,
			kind: parse.SpecValueExpr, exprKind: parse.ExprBinary},
		{name: "a comparison keeps its tree", src: `count > 0`,
			kind: parse.SpecValueExpr, exprKind: parse.ExprBinary},
		{name: "a conditional keeps its tree", src: `a ? b : c`,
			kind: parse.SpecValueExpr, exprKind: parse.ExprConditional},
		{name: "a logical operator keeps its tree", src: `a && b`,
			kind: parse.SpecValueExpr, exprKind: parse.ExprLogical},
		{name: "an index keeps its tree", src: `items[0]`,
			kind: parse.SpecValueExpr, exprKind: parse.ExprMember},
		{name: "a call with an un-projectable argument keeps its tree", src: `f(a + b)`,
			kind: parse.SpecValueExpr, exprKind: parse.ExprCall},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree, err := parse.QML{}.Parse([]byte("N { x: " + c.src + " }"))
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.src, err)
			}
			v := tree.Root.Props[0].Value
			if v.Kind != c.kind {
				t.Fatalf("kind = %v, want %v", v.Kind, c.kind)
			}
			if c.kind == parse.SpecValueExpr {
				if v.Expr == nil {
					t.Fatal("an expression value carries no tree")
				}
				if v.Expr.Kind != c.exprKind {
					t.Errorf("expr kind = %v, want %v", v.Expr.Kind, c.exprKind)
				}
				return
			}
			if v.Raw != c.raw {
				t.Errorf("raw = %q, want %q", v.Raw, c.raw)
			}
			// A projected value does NOT also carry a tree: one representation
			// per value, so nothing downstream can read two answers.
			if v.Expr != nil {
				t.Errorf("a projected %v also carries an Expr; that is two "+
					"representations of one value", v.Kind)
			}
		})
	}
}

// TestAnExpressionValueKeepsEveryNameItReads.
//
// The tree has to be WALKABLE, because a consumer that can evaluate expressions
// will find its dependencies there. A value that kept only the source text
// would look complete and be useless.
func TestAnExpressionValueKeepsEveryNameItReads(t *testing.T) {
	tree, err := parse.QML{}.Parse([]byte(`N { x: Theme.pad + parent.width / scale }`))
	if err != nil {
		t.Fatal(err)
	}
	v := tree.Root.Props[0].Value
	if v.Kind != parse.SpecValueExpr || v.Expr == nil {
		t.Fatalf("value = %+v, want an expression with a tree", v)
	}
	var names []string
	v.Expr.Walk(func(e *parse.Expr) bool {
		if e.Kind == parse.ExprIdent {
			names = append(names, e.Raw)
		}
		return true
	})
	got := strings.Join(names, ",")
	if got != "Theme,parent,scale" {
		t.Errorf("identifiers = %q, want %q", got, "Theme,parent,scale")
	}
}
