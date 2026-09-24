package qml_test

import (
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"

	"errors"
	"strings"
	"testing"
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
		tree, err := qml.QML{}.Parse([]byte(c.src))
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
	block, err := qml.QML{}.Parse([]byte(`Text { font { bold: true } }`))
	if err != nil {
		t.Fatal(err)
	}
	dotted, err := qml.QML{}.Parse([]byte(`Text { font.bold: true }`))
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
	tree, err := qml.QML{}.Parse([]byte(`Text { font { style { weight: 700 } bold: true } }`))
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
	tree, err := qml.QML{}.Parse([]byte(`Flex { Text { text: "x" } font { bold: true } }`))
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
	tree, err := qml.QML{}.Parse([]byte(`Item { Component.onCompleted: go() }`))
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
	if len(h.Body) != 1 || v == nil || v.Kind != js.ExprCall || v.Left.Raw != "go" {
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
	q := qml.QML{}
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
	_, err := qml.QML{}.Parse([]byte(`Text { font { bold: true`))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a parse.SyntaxError", err)
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
		kind qml.SpecValueKind
		// exprKind is checked only for values that keep their tree.
		exprKind js.ExprKind
		raw      string
	}{
		{name: "a string projects", src: `"hi"`, kind: qml.SpecValueString, raw: "hi"},
		{name: "a number projects", src: `12.5`, kind: qml.SpecValueNumber, raw: "12.5"},
		{name: "a negative number folds its sign", src: `-3`, kind: qml.SpecValueNumber, raw: "-3"},
		{name: "a bool projects", src: `false`, kind: qml.SpecValueBool, raw: "false"},
		{name: "a name projects", src: `greeting`, kind: qml.SpecValueRef, raw: "greeting"},
		{name: "a member chain projects", src: `parent.width`, kind: qml.SpecValueRef, raw: "parent.width"},
		{name: "a call projects", src: `f(1)`, kind: qml.SpecValueCall, raw: "f"},
		{name: "a qualified call projects", src: `math.max(1, 2)`, kind: qml.SpecValueCall, raw: "math.max"},

		{name: "arithmetic keeps its tree", src: `parent.width / 2`,
			kind: qml.SpecValueExpr, exprKind: js.ExprBinary},
		{name: "a comparison keeps its tree", src: `count > 0`,
			kind: qml.SpecValueExpr, exprKind: js.ExprBinary},
		{name: "a conditional keeps its tree", src: `a ? b : c`,
			kind: qml.SpecValueExpr, exprKind: js.ExprConditional},
		{name: "a logical operator keeps its tree", src: `a && b`,
			kind: qml.SpecValueExpr, exprKind: js.ExprLogical},
		{name: "an index keeps its tree", src: `items[0]`,
			kind: qml.SpecValueExpr, exprKind: js.ExprMember},
		{name: "a call with an un-projectable argument keeps its tree", src: `f(a + b)`,
			kind: qml.SpecValueExpr, exprKind: js.ExprCall},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree, err := qml.QML{}.Parse([]byte("N { x: " + c.src + " }"))
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.src, err)
			}
			v := tree.Root.Props[0].Value
			if v.Kind != c.kind {
				t.Fatalf("kind = %v, want %v", v.Kind, c.kind)
			}
			if c.kind == qml.SpecValueExpr {
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
				t.Errorf("a projected %v also carries an js.Expr; that is two "+
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
	tree, err := qml.QML{}.Parse([]byte(`N { x: Theme.pad + parent.width / scale }`))
	if err != nil {
		t.Fatal(err)
	}
	v := tree.Root.Props[0].Value
	if v.Kind != qml.SpecValueExpr || v.Expr == nil {
		t.Fatalf("value = %+v, want an expression with a tree", v)
	}
	var names []string
	v.Expr.Walk(func(e *js.Expr) bool {
		if e.Kind == js.ExprIdent {
			names = append(names, e.Raw)
		}
		return true
	})
	got := strings.Join(names, ",")
	if got != "Theme,parent,scale" {
		t.Errorf("identifiers = %q, want %q", got, "Theme,parent,scale")
	}
}

// TestTheDiagnosticsForAMalformedImportOrPropertyName.
//
// These messages are the whole value of a hand-written parser over a generated
// one, and nothing asserted any of them: every branch below was reachable and
// unexercised, so a refactor could have turned any of them into "unexpected
// token" and no test would have noticed.
//
// Each row is checked for the SPECIFIC want text, not merely that an error
// happened — the wrong reason is its own defect.
func TestTheDiagnosticsForAMalformedImportOrPropertyName(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		want       string
		incomplete bool
	}{
		{
			name: "import with no module",
			src:  "import 1.0\nN { }",
			want: "a module name after import",
		},
		{
			name: "import with a trailing dot",
			src:  "import tui.\nN { }",
			want: "a name after . in the module name",
		},
		{
			name: "import with as and no name on its line",
			src:  "import tui 1.0 as\nN { }",
			want: "a name after as",
		},
		{
			name: "import with as and a number",
			src:  "import tui 1.0 as 123\nN { }",
			want: "a name after as",
		},
		{
			name: "import with a lower-case qualifier",
			src:  "import tui 1.0 as t\nN { }",
			want: "an upper-case qualifier after as",
		},
		{
			name: "a property name with a trailing dot",
			src:  "N { font. : 1 }",
			want: "a name after . in the property name",
		},
		{
			name: "a grouped entry with no colon",
			src:  "N { font { bold } }",
			want: ": or { after the property name in the group",
		},
		{
			name: "a grouped entry name with a trailing dot",
			src:  "N { font { weight. : 1 } }",
			want: "a name after . in the property name",
		},
		{
			name:       "an import cut off at end of input",
			src:        "import ",
			want:       "a module name after import",
			incomplete: true,
		},
		{
			name:       "a grouped block cut off at end of input",
			src:        "N { font { bold: true ",
			want:       "",
			incomplete: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			se := syntaxErr(t, c.src)
			if c.want != "" && !strings.Contains(se.Want, c.want) {
				t.Errorf("Want = %q, want it to contain %q", se.Want, c.want)
			}
			if se.Incomplete != c.incomplete {
				t.Errorf("Incomplete = %v, want %v — truncation and a mistake are "+
					"different things to a file watcher", se.Incomplete, c.incomplete)
			}
			if se.Pos.Line == 0 {
				t.Error("the error carries no position")
			}
		})
	}
}

// TestAGroupedBlockAndItsDottedFormMeanTheSameThing.
//
// `font { bold: true }` and `font.bold: true` are the same property. The parser
// records WHICH was written, for a consumer that round-trips and would
// otherwise rewrite one into the other, but Path and Value must agree.
func TestAGroupedBlockAndItsDottedFormMeanTheSameThing(t *testing.T) {
	grouped := mustParse(t, `N { font { bold: true  size: 12 } }`)
	dotted := mustParse(t, `N { font.bold: true  font.size: 12 }`)

	if len(grouped.Root.Props) != len(dotted.Root.Props) {
		t.Fatalf("grouped has %d props, dotted has %d",
			len(grouped.Root.Props), len(dotted.Root.Props))
	}
	for i := range grouped.Root.Props {
		g, d := grouped.Root.Props[i], dotted.Root.Props[i]
		if strings.Join(g.Path, ".") != strings.Join(d.Path, ".") {
			t.Errorf("prop %d path: grouped %v, dotted %v", i, g.Path, d.Path)
		}
		if g.Value.Kind != d.Value.Kind || g.Value.Raw != d.Value.Raw {
			t.Errorf("prop %d value: grouped %+v, dotted %+v", i, g.Value, d.Value)
		}
		if !g.Grouped {
			t.Errorf("prop %d: Grouped is false for a property written in a block", i)
		}
		if d.Grouped {
			t.Errorf("prop %d: Grouped is true for a property written with a dot", i)
		}
	}
}

// TestAComputedMemberIsNotAName.
//
// `items[0]` is decided when it runs, so it is not a path anything can resolve
// ahead of time and must not be flattened into one.
func TestAComputedMemberIsNotAName(t *testing.T) {
	for _, src := range []string{`N { x: items[0] }`, `N { x: items[0]() }`, `N { x: f()() }`} {
		tree, err := qml.QML{}.Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		v := tree.Root.Props[0].Value
		if v.Kind != qml.SpecValueExpr {
			t.Errorf("%s = %v, want it to keep its tree rather than become a name", src, v.Kind)
		}
	}
}
