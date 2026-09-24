package qml_test

import (
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"

	"errors"
	"strings"
	"testing"
)

// qml_test.go covers qml.QML.
//
// The suite is organised around the claims the ADR makes, not around the
// parser's functions, because the claims are what a reviewer and a future
// maintainer need held: syntax-only judgment, Incomplete on truncation,
// document order, and parse.Position surviving into every error.

func mustParse(t *testing.T, src string) qml.SpecTree {
	t.Helper()
	tree, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", src, err)
	}
	return tree
}

func syntaxErr(t *testing.T, src string) parse.SyntaxError {
	t.Helper()
	_, err := qml.QML{}.Parse([]byte(src))
	if err == nil {
		t.Fatalf("Parse(%q) = nil error, want a parse.SyntaxError", src)
	}
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("Parse(%q) error %T = %v, want parse.SyntaxError", src, err, err)
	}
	return se
}

// ---------------------------------------------------------------- surface

func TestQMLImplementsParserAndNamed(t *testing.T) {
	// The compile-time assertion is the point: Parser[SpecTree] is the whole
	// required surface (parse.go), and a format that stops satisfying it
	// should fail to build rather than fail a test.
	var _ parse.Parser[qml.SpecTree] = qml.QML{}

	if name := parse.FormatNameOf(qml.QML{}); name != "qml" {
		t.Errorf("parse.FormatNameOf = %q, want %q", name, "qml")
	}
}

func TestQMLDoesNotClaimCapabilitiesItLacks(t *testing.T) {
	// Validating QML costs exactly as much as parsing it, so Validator is
	// deliberately NOT implemented (parse.go: "leave this out and let callers
	// do that themselves, visibly"). Asserting the absence keeps a future
	// "might as well add it" from landing without that argument being had.
	if _, ok := parse.AsValidator(qml.QML{}); ok {
		t.Error("QML implements Validator; validation is not cheaper than parsing here")
	}
}

// ---------------------------------------------------------------- shape

func TestQMLParsesNodesPropsHandlersChildren(t *testing.T) {
	tree := mustParse(t, `
// a comment
Column {
    id: root
    spacing: 2
    Button {
        label: "Save"
        enabled: true
        onClicked: saveDocument()
    }
    /* block comment */
    Text { text: "hello" }
}`)

	root := tree.Root
	if root == nil {
		t.Fatal("Root is nil")
	}
	if root.Type != "Column" {
		t.Errorf("root type = %q, want Column", root.Type)
	}
	if root.ID != "root" {
		t.Errorf("root id = %q, want root", root.ID)
	}
	// id is lifted OUT of Props: it addresses the node, it does not configure it.
	for _, p := range root.Props {
		if p.Name == "id" {
			t.Error("id appears in Props; it must be lifted onto SpecNode.ID")
		}
	}
	if len(root.Props) != 1 || root.Props[0].Name != "spacing" {
		t.Fatalf("root props = %+v, want only spacing", root.Props)
	}
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}

	btn := root.Children[0]
	if btn.Type != "Button" {
		t.Errorf("child 0 type = %q, want Button", btn.Type)
	}
	if len(btn.Handlers) != 1 {
		t.Fatalf("button handlers = %+v, want 1", btn.Handlers)
	}
	// onClicked -> "clicked": the adapter registers slots under the signal
	// name, not the schema spelling.
	h := btn.Handlers[0]
	if h.Signal != "clicked" {
		t.Errorf("signal = %q, want clicked", h.Signal)
	}
	v := h.Body[0].Value
	if len(h.Body) != 1 || v == nil || v.Kind != js.ExprCall || v.Left.Raw != "saveDocument" {
		t.Errorf("body = %+v, want the call to saveDocument", h.Body)
	}
}

func TestQMLPreservesDocumentOrder(t *testing.T) {
	// Document order is the order a consumer applies properties and runs
	// handlers. A map-backed implementation would pass every other test in
	// this file and fail this one, which is why it exists.
	tree := mustParse(t, `Row { c: 1 a: 2 b: 3 }`)
	var got []string
	for _, p := range tree.Root.Props {
		got = append(got, p.Name)
	}
	if strings.Join(got, ",") != "c,a,b" {
		t.Errorf("prop order = %v, want [c a b] — document order is the contract", got)
	}
}

// ---------------------------------------------------------------- values

func TestQMLClassifiesValuesLexically(t *testing.T) {
	tree := mustParse(t, `N {
        s: "text"
        n: 12.5
        neg: -3
        b: false
        tok: Theme.surface
        ref: someName
        call: fmtSize(2, "u")
    }`)

	want := map[string]struct {
		kind qml.SpecValueKind
		raw  string
	}{
		"s":    {qml.SpecValueString, "text"},
		"n":    {qml.SpecValueNumber, "12.5"},
		"neg":  {qml.SpecValueNumber, "-3"},
		"b":    {qml.SpecValueBool, "false"},
		"tok":  {qml.SpecValueRef, "Theme.surface"},
		"ref":  {qml.SpecValueRef, "someName"},
		"call": {qml.SpecValueCall, "fmtSize"},
	}
	for _, p := range tree.Root.Props {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected prop %q", p.Name)
			continue
		}
		if p.Value.Kind != w.kind || p.Value.Raw != w.raw {
			t.Errorf("%s = {%v %q}, want {%v %q}",
				p.Name, p.Value.Kind, p.Value.Raw, w.kind, w.raw)
		}
		delete(want, p.Name)
	}
	if len(want) != 0 {
		t.Errorf("props never seen: %v", want)
	}
}

func TestQMLCallArgumentsAreValues(t *testing.T) {
	tree := mustParse(t, `N { x: outer(1, inner(Theme.tok), "s") }`)
	v := tree.Root.Props[0].Value
	if v.Kind != qml.SpecValueCall || len(v.Args) != 3 {
		t.Fatalf("value = %+v, want a call with 3 args", v)
	}
	if v.Args[1].Kind != qml.SpecValueCall || len(v.Args[1].Args) != 1 {
		t.Fatalf("nested arg = %+v, want a call with 1 arg", v.Args[1])
	}
	if v.Args[1].Args[0].Kind != qml.SpecValueRef {
		t.Errorf("nested call arg kind = %v, want token", v.Args[1].Args[0].Kind)
	}
}

// TestQMLDoesNotJudgeValueMEANING pins the layering, and it exists because the
// tempting mistake is to reject a colour-shaped literal right here.
//
// A colour-shaped string is ORDINARY CONTENT for a text property. Only the UI
// adapter's registry knows which properties accept a style token and which
// accept arbitrary text, so a parser that rejected "#1e1e2e" would refuse
// legitimate documents to enforce a rule it cannot evaluate.
func TestQMLDoesNotJudgeValueMEANING(t *testing.T) {
	tree := mustParse(t, `Text { text: "#1e1e2e" foreground: Theme.text }`)

	var text, fg qml.SpecValue
	for _, p := range tree.Root.Props {
		switch p.Name {
		case "text":
			text = p.Value
		case "foreground":
			fg = p.Value
		}
	}
	if text.Kind != qml.SpecValueString || text.Raw != "#1e1e2e" {
		t.Errorf(`text = {%v %q}, want a plain string "#1e1e2e" — `+
			`the parser must not second-guess a colour-shaped string`, text.Kind, text.Raw)
	}
	if fg.Kind != qml.SpecValueRef || fg.Raw != "Theme.text" {
		t.Errorf("foreground = {%v %q}, want a reference to %q", fg.Kind, fg.Raw, "Theme.text")
	}
}

func TestQMLStringEscapes(t *testing.T) {
	tree := mustParse(t, `N { s: "a\tb\nc\"d\\e" }`)
	if got := tree.Root.Props[0].Value.Raw; got != "a\tb\nc\"d\\e" {
		t.Errorf("unescaped = %q", got)
	}
}

// ---------------------------------------------------------------- errors

// TestQMLIncompleteVsWrong separates "unfinished" from "wrong". A reload path
// holds the last good tree on Incomplete and surfaces the error otherwise, so
// the two must be distinguishable — and every truncation below is a file caught
// mid-save, not a mistake.
func TestQMLIncompleteVsWrong(t *testing.T) {
	incomplete := []string{
		``,
		`Column {`,
		`Column { Button {`,
		`Column { label:`,
		`Column { label: "unterminated`,
		`Column { x: f(1,`,
		`Column { onClicked:`,
	}
	for _, src := range incomplete {
		se := syntaxErr(t, src)
		if !se.Incomplete {
			t.Errorf("Parse(%q): Incomplete = false, want true (a mid-save truncation)", src)
		}
	}

	wrong := []string{
		`Column { 123: "x" }`,
		`Column { label: ! }`,
		`Column { label: "a" } trailing`,
		`lowercase { }`,
		`Column { label: 1.2.3 }`,
	}
	for _, src := range wrong {
		se := syntaxErr(t, src)
		if se.Incomplete {
			t.Errorf("Parse(%q): Incomplete = true, want false (this is wrong, not unfinished)", src)
		}
	}
}

// TestQMLErrorsPointAtTheOpeningConstruct: an unclosed brace reported at EOF
// sends the writer to the bottom of the file, which is never where the mistake
// is. sql.go sets this precedent for block comments; QML follows it.
// TestQMLUnterminatedCommentIsReported covers the case a "skip" function is
// most likely to lose: a block comment that runs off the end of the file.
//
// Skipping is exactly where an unterminated construct can vanish, because the
// skipper's job is to consume and say nothing. After a COMPLETE root node
// nothing expects another token, so an error deferred to "whatever reads next"
// is an error nobody reads — and the file parses as a success. That is the
// worst outcome available here: a half-written file silently accepted.
func TestQMLUnterminatedCommentIsReported(t *testing.T) {
	cases := map[string]string{
		"leading":  "/* never closed\nN { }",
		"in body":  "N {\n  /* never closed\n  a: 1\n}",
		"trailing": "N { }\n/* never closed",
	}
	for where, src := range cases {
		t.Run(where, func(t *testing.T) {
			_, err := qml.QML{}.Parse([]byte(src))
			if err == nil {
				t.Fatalf("Parse(%q) = nil error; an unterminated comment was accepted", src)
			}
			var se parse.SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error %T, want parse.SyntaxError", err)
			}
			if !se.Incomplete {
				t.Error("Incomplete = false; a comment left open is a truncation, not a mistake")
			}
			if !errors.Is(err, parse.ErrUnterminated) {
				t.Errorf("identity = %v, want ErrUnterminated", err)
			}
			if !strings.Contains(se.Want, "*/") {
				t.Errorf("Want = %q, should name the missing */", se.Want)
			}
			// Reported at the OPENER, not at end of input.
			if !strings.HasPrefix(src[se.Pos.Offset:], "/*") {
				t.Errorf("Pos %d points at %q, want the /* that opened the comment",
					se.Pos.Offset, src[se.Pos.Offset:])
			}
		})
	}

	// Control: a comment that IS closed, in each position, still parses — so
	// the rule above is not "reject block comments".
	for _, src := range []string{
		"/* ok */ N { }",
		"N { /* ok */ a: 1 }",
		"N { }\n/* ok */",
		"N { } // a line comment needs no terminator",
	} {
		if _, err := (qml.QML{}).Parse([]byte(src)); err != nil {
			t.Errorf("Parse(%q) failed: %v", src, err)
		}
	}
}

// TestQMLDepthBoundsEveryRecursivePath: the budget exists to keep a malformed
// or half-written file from exhausting the stack, and the stack does not care
// which function recursed. Nodes were bounded first; a call argument list
// recurses just as deeply through value().
func TestQMLDepthBoundsEveryRecursivePath(t *testing.T) {
	deepCall := "N { x: " + strings.Repeat("f(", 5000) + strings.Repeat(")", 5000) + " }"
	_, err := qml.QML{}.Parse([]byte(deepCall))
	if err == nil {
		t.Fatal("5,000-deep call list parsed without error; value recursion is unbounded")
	}
	var se parse.SyntaxError
	// Both grammars word the limit the same way; the sub-parser names itself in
	// Format, which is accurate — a nesting limit hit inside an expression WAS
	// hit inside an expression.
	if !errors.As(err, &se) || !strings.Contains(se.Want, "no deeper than") {
		t.Errorf("error = %v, want a nesting-limit parse.SyntaxError", err)
	}

	// Nodes and expressions share ONE budget.
	//
	// The property is asserted WITHOUT hardcoding what an expression costs in
	// frames, because that is an implementation detail that a faithful grammar
	// is entitled to change — and an earlier version of this test pinned it,
	// then failed for a reason that had nothing to do with the claim.
	//
	// Instead: find the smallest limit at which a document parses, then assert
	// that ONE MORE ENCLOSING NODE — with the expression untouched — needs
	// exactly one more. That is only true if node frames and expression frames
	// draw on the same pool, and it is not visible from either kind alone.
	const inner = "x: f(g(1))"
	limitFor := func(src string) int {
		t.Helper()
		for n := 1; n <= 64; n++ {
			if _, err := (qml.QML{MaxDepth: n}).Parse([]byte(src)); err == nil {
				return n
			}
		}
		t.Fatalf("Parse(%q) never succeeded up to MaxDepth=64", src)
		return 0
	}
	two := limitFor("A { B { " + inner + " } }")
	three := limitFor("A { B { C { " + inner + " } } }")
	if three != two+1 {
		t.Errorf("limits are %d and %d; one more enclosing node must cost exactly "+
			"one more level, or nodes and expressions are not sharing a budget",
			two, three)
	}
}

func TestQMLErrorsPointAtTheOpeningConstruct(t *testing.T) {
	src := "Column {\n  Button {\n    label: \"x\"\n"
	se := syntaxErr(t, src)
	if !se.Incomplete {
		t.Fatalf("Incomplete = false, want true")
	}
	// The inner Button opened on line 2 and is the innermost unclosed thing.
	if se.Pos.Line != 2 {
		t.Errorf("error line = %d, want 2 (where the unclosed node opened)", se.Pos.Line)
	}
	if !strings.Contains(se.Want, "Button") {
		t.Errorf("Want = %q, should name the unclosed node", se.Want)
	}
}

// TestQMLPositionsAreRuneColumns: parse.Position.Column is documented as rune-counted
// "so the number matches what an editor shows". A multi-byte prefix is the only
// input that can tell a rune count from a byte count.
func TestQMLPositionsAreRuneColumns(t *testing.T) {
	// "é" is two bytes, one rune. The error is at the `%`.
	//
	// `%` rather than `!`, which this grammar reads as the prefix operator it is
	// in js.JavaScript: the mistake in `b: !` is the MISSING OPERAND after it, so
	// the position correctly moves to the `}` and stops isolating the rune
	// count. `%` has no prefix reading, so the error is at the character itself.
	se := syntaxErr(t, `N { s: "éé" b: % }`)
	if se.Pos.Line != 1 {
		t.Fatalf("line = %d, want 1", se.Pos.Line)
	}
	// Count runes up to '%': N,space,{,space,s,:,space,",é,é,",space,b,:,space = 15
	const wantCol = 16
	if se.Pos.Column != wantCol {
		t.Errorf("column = %d, want %d — columns must be RUNES, not bytes",
			se.Pos.Column, wantCol)
	}
}

func TestQMLValuePositionsSurvive(t *testing.T) {
	// Token-only enforcement belongs to the registry, which reports using the
	// parse.Position recorded here. If values lose their position that error cannot
	// point anywhere useful, so carrying it is part of this parser's job.
	tree := mustParse(t, "Column {\n  label: \"x\"\n  other: Theme.tok\n}")
	for _, p := range tree.Root.Props {
		if p.Value.Pos.Line == 0 || p.Value.Pos.Column == 0 {
			t.Errorf("prop %q value has zero parse.Position %+v", p.Name, p.Value.Pos)
		}
	}
	if got := tree.Root.Props[0].Value.Pos.Line; got != 2 {
		t.Errorf("first value line = %d, want 2", got)
	}
	if got := tree.Root.Props[1].Value.Pos.Line; got != 3 {
		t.Errorf("second value line = %d, want 3", got)
	}
}

func TestQMLIDMustBeAStableIdentifier(t *testing.T) {
	// The id is the identity a reload matches on. One produced by a call could
	// differ between reloads, and an identity that moves is not an identity.
	for _, src := range []string{
		`N { id: "quoted" }`,
		`N { id: gen() }`,
		`N { id: 3 }`,
	} {
		se := syntaxErr(t, src)
		if !strings.Contains(se.Want, "identifier") {
			t.Errorf("Parse(%q): Want = %q, should ask for a bare identifier", src, se.Want)
		}
	}
	if se := syntaxErr(t, `N { id: a id: b }`); !strings.Contains(se.Want, "at most one id") {
		t.Errorf("duplicate id Want = %q", se.Want)
	}
}

// TestQMLDepthIsBounded: the parser is recursive and the reload path may be
// handed a file from a watcher mid-write. A stack overflow cannot be recovered;
// a parse.SyntaxError can be shown.
func TestQMLDepthIsBounded(t *testing.T) {
	deep := strings.Repeat("N { ", 5000) + strings.Repeat("}", 5000)
	_, err := qml.QML{}.Parse([]byte(deep))
	if err == nil {
		t.Fatal("deeply nested input parsed without error; the depth bound did not engage")
	}
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("error %T, want parse.SyntaxError", err)
	}
	if !strings.Contains(se.Want, "nesting") {
		t.Errorf("Want = %q, should name the nesting limit", se.Want)
	}

	// And the bound is configurable, so a legitimately deep schema is not
	// stuck with the default.
	if _, err := (qml.QML{MaxDepth: 3}).Parse([]byte("A { B { C { D { } } } }")); err == nil {
		t.Error("MaxDepth=3 accepted 4 levels")
	}
	if _, err := (qml.QML{MaxDepth: 8}).Parse([]byte("A { B { C { D { } } } }")); err != nil {
		t.Errorf("MaxDepth=8 rejected 4 levels: %v", err)
	}
}

func TestQMLErrorIdentity(t *testing.T) {
	// parse.SyntaxError carries one of TWO identities depending on Incomplete
	// (parse.go: "it selects which of the two identities this error carries").
	// This is "unfinished" versus "wrong" expressed as error identity rather
	// than as a bool, and it is the form a reload path should branch on:
	// errors.Is, not a field read and not string matching.
	_, incomplete := qml.QML{}.Parse([]byte(`Column {`))
	if !errors.Is(incomplete, parse.ErrUnterminated) {
		t.Errorf("truncated input: want ErrUnterminated, got %v", incomplete)
	}
	if errors.Is(incomplete, parse.ErrSyntax) {
		t.Error("truncated input also matched ErrSyntax; the identities must be distinguishable")
	}

	_, wrong := qml.QML{}.Parse([]byte(`Column { 1: 2 }`))
	if !errors.Is(wrong, parse.ErrSyntax) {
		t.Errorf("malformed input: want ErrSyntax, got %v", wrong)
	}
	if errors.Is(wrong, parse.ErrUnterminated) {
		t.Error("malformed input matched ErrUnterminated; a mistake is not a truncation")
	}

	if got := incomplete.Error(); !strings.HasPrefix(got, "qml:") {
		t.Errorf("error text = %q, want it to name the format", got)
	}
}

// TestQMLHandlerTakesAJavaScriptBody.
//
// This test previously asserted the OPPOSITE — that a body is refused and the
// author is told "this format binds handlers by name". That refusal was the
// parser shrunk to fit the engine behind it: QML puts js.JavaScript after
// `onClicked:`, so a parser of QML reads js.JavaScript there. An engine that can
// only invoke named handlers refuses what it cannot run, by name and position,
// which is a different thing said in a different place.
func TestQMLHandlerTakesAJavaScriptBody(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		kinds []js.StmtKind
	}{
		{
			name:  "a call",
			src:   `Button { onClicked: doThing() }`,
			kinds: []js.StmtKind{js.StmtExpr},
		},
		{
			name:  "a braced body is the block's CONTENTS, not a block",
			src:   `Button { onClicked: { doThing() } }`,
			kinds: []js.StmtKind{js.StmtExpr},
		},
		{
			name:  "several statements",
			src:   "Button { onClicked: { let x = 1\n doThing(x) } }",
			kinds: []js.StmtKind{js.StmtDeclaration, js.StmtExpr},
		},
		{
			name:  "a bare name is an identifier expression, not a call",
			src:   `Button { onClicked: doThing }`,
			kinds: []js.StmtKind{js.StmtExpr},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := mustParse(t, c.src)
			if len(tree.Root.Handlers) != 1 {
				t.Fatalf("handlers = %+v, want 1", tree.Root.Handlers)
			}
			body := tree.Root.Handlers[0].Body
			if len(body) != len(c.kinds) {
				t.Fatalf("body = %+v, want %d statements", body, len(c.kinds))
			}
			for i, k := range c.kinds {
				if body[i].Kind != k {
					t.Errorf("statement %d = %s, want %s", i, body[i].Kind, k)
				}
			}
		})
	}
}

// TestABareHandlerNameIsNotACall.
//
// The distinction the old `Name string` field erased. `onClicked: save` and
// `onClicked: save()` used to produce the same SpecHandler, so an engine could
// not tell them apart even if it wanted to — the information was gone inside the
// parser, where nothing downstream could recover it.
func TestABareHandlerNameIsNotACall(t *testing.T) {
	bare := mustParse(t, `Button { onClicked: save }`).Root.Handlers[0].Body
	called := mustParse(t, `Button { onClicked: save() }`).Root.Handlers[0].Body

	if bare[0].Value == nil || called[0].Value == nil {
		t.Fatal("both bodies should be expression statements with a value")
	}
	if bare[0].Value.Kind == called[0].Value.Kind {
		t.Fatalf("both parsed as %s; a name and a call must not be the same node",
			bare[0].Value.Kind)
	}
	if called[0].Value.Kind != js.ExprCall {
		t.Errorf("save() = %s, want a call", called[0].Value.Kind)
	}
}

func TestQMLOnIsNotAlwaysASignal(t *testing.T) {
	// `onward` is a property; `on` alone is a property. Only onX with an
	// upper-case X is a signal.
	tree := mustParse(t, `N { onward: 1 on: 2 onClicked: h }`)
	if len(tree.Root.Props) != 2 {
		t.Errorf("props = %+v, want onward and on", tree.Root.Props)
	}
	if len(tree.Root.Handlers) != 1 || tree.Root.Handlers[0].Signal != "clicked" {
		t.Errorf("handlers = %+v, want only clicked", tree.Root.Handlers)
	}
}

// TestAHandlerBodySpendsTheDOCUMENTSNestingBudget.
//
// The handler body runs on a second parser, and the tempting implementation
// gives it a fresh depth counter. That would make the document's limit a
// fiction: `A { B { … } }` would be bounded and `A { onGo: f(f(f(…))) }` would
// not, although both are recursion in the same parse of the same file.
//
// The two halves are asserted together. A nesting limit that refuses everything
// is trivially "safe" and useless, so the shallow body must still be accepted at
// the same limit that refuses the deep one.
func TestAHandlerBodySpendsTheDOCUMENTSNestingBudget(t *testing.T) {
	const limit = 6
	deep := "A { onGo: " + strings.Repeat("f(", limit+2) + "x" + strings.Repeat(")", limit+2) + " }"
	shallow := "A { onGo: f(f(x)) }"

	if _, err := (qml.QML{MaxDepth: limit}).Parse([]byte(shallow)); err != nil {
		t.Fatalf("a shallow handler body was refused at MaxDepth=%d: %v", limit, err)
	}
	if _, err := (qml.QML{MaxDepth: limit}).Parse([]byte(deep)); err == nil {
		t.Errorf("a handler body nested past MaxDepth=%d was accepted; "+
			"the body is parsing its own budget rather than the document's", limit)
	}

	// And the budget is SHARED, not merely present: nodes already spent bring
	// the same body over the line.
	nested := "A { B { C { onGo: f(f(f(x))) } } }"
	if _, err := (qml.QML{MaxDepth: 12}).Parse([]byte(nested)); err != nil {
		t.Fatalf("a body under three nodes was refused with budget to spare: %v", err)
	}
	if _, err := (qml.QML{MaxDepth: 4}).Parse([]byte(nested)); err == nil {
		t.Error("nodes and a handler body together exceeded the limit and were accepted")
	}
}

// TestAnUnknownStringEscapeIsTheCharacterItself.
//
// This used to be a syntax error. js.JavaScript says otherwise — `"\q"` is `"q"` —
// and a QML string is a js.JavaScript string, so refusing it was this parser
// applying a rule of its own to a language it does not own.
//
// The escapes that DO mean something still mean it, which is the half that
// makes this a fidelity change rather than a hole.
func TestAnUnknownStringEscapeIsTheCharacterItself(t *testing.T) {
	tree := mustParse(t, `N { s: "a\qb" }`)
	if got := tree.Root.Props[0].Value.Raw; got != "aqb" {
		t.Errorf("unescaped = %q, want %q — an unknown escape is the character", got, "aqb")
	}
	tree = mustParse(t, `N { s: "a\tb\nc\"d\\e" }`)
	if got := tree.Root.Props[0].Value.Raw; got != "a\tb\nc\"d\\e" {
		t.Errorf("unescaped = %q; the escapes that mean something must still mean it", got)
	}
}
