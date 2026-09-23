package parse_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// qml_test.go covers parse.QML — the ADR-0001 P1 deliverable.
//
// The suite is organised around the claims the ADR makes, not around the
// parser's functions, because the claims are what a reviewer and a future
// maintainer need held: syntax-only judgment, Incomplete on truncation,
// document order, and Position surviving into every error.

func mustParse(t *testing.T, src string) parse.SpecTree {
	t.Helper()
	tree, err := parse.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", src, err)
	}
	return tree
}

func syntaxErr(t *testing.T, src string) parse.SyntaxError {
	t.Helper()
	_, err := parse.QML{}.Parse([]byte(src))
	if err == nil {
		t.Fatalf("Parse(%q) = nil error, want a SyntaxError", src)
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
	var _ parse.Parser[parse.SpecTree] = parse.QML{}

	if name := parse.FormatNameOf(parse.QML{}); name != "qml" {
		t.Errorf("FormatNameOf = %q, want %q", name, "qml")
	}
}

func TestQMLDoesNotClaimCapabilitiesItLacks(t *testing.T) {
	// Validating QML costs exactly as much as parsing it, so Validator is
	// deliberately NOT implemented (parse.go: "leave this out and let callers
	// do that themselves, visibly"). Asserting the absence keeps a future
	// "might as well add it" from landing without that argument being had.
	if _, ok := parse.AsValidator(parse.QML{}); ok {
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
        onClicked: saveDocument
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
	if btn.Handlers[0].Signal != "clicked" || btn.Handlers[0].Name != "saveDocument" {
		t.Errorf("handler = %+v, want {clicked saveDocument}", btn.Handlers[0])
	}
}

func TestQMLPreservesDocumentOrder(t *testing.T) {
	// D4a makes document order the application order and the handler run
	// order. A map-backed implementation would pass every other test in this
	// file and fail this one, which is why it exists.
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
        tok: @surface
        ref: someName
        call: fmtSize(2, "u")
    }`)

	want := map[string]struct {
		kind parse.ValueKind
		raw  string
	}{
		"s":    {parse.ValueString, "text"},
		"n":    {parse.ValueNumber, "12.5"},
		"neg":  {parse.ValueNumber, "-3"},
		"b":    {parse.ValueBool, "false"},
		"tok":  {parse.ValueToken, "surface"},
		"ref":  {parse.ValueRef, "someName"},
		"call": {parse.ValueCall, "fmtSize"},
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
	tree := mustParse(t, `N { x: outer(1, inner(@tok), "s") }`)
	v := tree.Root.Props[0].Value
	if v.Kind != parse.ValueCall || len(v.Args) != 3 {
		t.Fatalf("value = %+v, want a call with 3 args", v)
	}
	if v.Args[1].Kind != parse.ValueCall || len(v.Args[1].Args) != 1 {
		t.Fatalf("nested arg = %+v, want a call with 1 arg", v.Args[1])
	}
	if v.Args[1].Args[0].Kind != parse.ValueToken {
		t.Errorf("nested call arg kind = %v, want token", v.Args[1].Args[0].Kind)
	}
}

// TestQMLDoesNotJudgeValueMEANING is the parser's half of ADR-0001 D8, and it
// is the cell that stops the rule being re-implemented in the wrong layer.
//
// The review that produced D8's correction turned on exactly this input: a
// colour-shaped string is ORDINARY CONTENT for a text property, and only the
// adapter registry knows which properties are token-only. A parser that
// rejected it here would reject legitimate documents.
func TestQMLDoesNotJudgeValueMEANING(t *testing.T) {
	tree := mustParse(t, `Text { text: "#1e1e2e" foreground: @text }`)

	var text, fg parse.Value
	for _, p := range tree.Root.Props {
		switch p.Name {
		case "text":
			text = p.Value
		case "foreground":
			fg = p.Value
		}
	}
	if text.Kind != parse.ValueString || text.Raw != "#1e1e2e" {
		t.Errorf(`text = {%v %q}, want a plain string "#1e1e2e" — `+
			`the parser must not second-guess a colour-shaped string`, text.Kind, text.Raw)
	}
	if fg.Kind != parse.ValueToken || fg.Raw != "text" {
		t.Errorf("foreground = {%v %q}, want token %q", fg.Kind, fg.Raw, "text")
	}
}

func TestQMLStringEscapes(t *testing.T) {
	tree := mustParse(t, `N { s: "a\tb\nc\"d\\e" }`)
	if got := tree.Root.Props[0].Value.Raw; got != "a\tb\nc\"d\\e" {
		t.Errorf("unescaped = %q", got)
	}
}

// ---------------------------------------------------------------- errors

// TestQMLIncompleteVsWrong is the P1 half of ADR-0001 D7. A reload path holds
// the last good tree on Incomplete and surfaces the error otherwise, so the two
// must be distinguishable — and every truncation below is a file caught
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
		`Column { label: "a\qb" }`,
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

// TestQMLPositionsAreRuneColumns: Position.Column is documented as rune-counted
// "so the number matches what an editor shows". A multi-byte prefix is the only
// input that can tell a rune count from a byte count.
func TestQMLPositionsAreRuneColumns(t *testing.T) {
	// "é" is two bytes, one rune. The error is at the `!`.
	se := syntaxErr(t, `N { s: "éé" b: ! }`)
	if se.Pos.Line != 1 {
		t.Fatalf("line = %d, want 1", se.Pos.Line)
	}
	// Count runes up to '!': N,space,{,space,s,:,space,",é,é,",space,b,:,space = 15
	const wantCol = 16
	if se.Pos.Column != wantCol {
		t.Errorf("column = %d, want %d — columns must be RUNES, not bytes",
			se.Pos.Column, wantCol)
	}
}

func TestQMLValuePositionsSurvive(t *testing.T) {
	// D8 moves token-only enforcement to the registry, which reports using the
	// Position the parser recorded. If Values lose their position, that error
	// cannot point anywhere useful — so the position is part of P1's contract.
	tree := mustParse(t, "Column {\n  label: \"x\"\n  other: @tok\n}")
	for _, p := range tree.Root.Props {
		if p.Value.Pos.Line == 0 || p.Value.Pos.Column == 0 {
			t.Errorf("prop %q value has zero Position %+v", p.Name, p.Value.Pos)
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
	// The id is the reconciliation identity (D5a). One that came from a call
	// could differ between reloads, and an identity that moves is not one.
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
// a SyntaxError can be shown.
func TestQMLDepthIsBounded(t *testing.T) {
	deep := strings.Repeat("N { ", 5000) + strings.Repeat("}", 5000)
	_, err := parse.QML{}.Parse([]byte(deep))
	if err == nil {
		t.Fatal("deeply nested input parsed without error; the depth bound did not engage")
	}
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("error %T, want SyntaxError", err)
	}
	if !strings.Contains(se.Want, "nesting") {
		t.Errorf("Want = %q, should name the nesting limit", se.Want)
	}

	// And the bound is configurable, so a legitimately deep schema is not
	// stuck with the default.
	if _, err := (parse.QML{MaxDepth: 3}).Parse([]byte("A { B { C { D { } } } }")); err == nil {
		t.Error("MaxDepth=3 accepted 4 levels")
	}
	if _, err := (parse.QML{MaxDepth: 8}).Parse([]byte("A { B { C { D { } } } }")); err != nil {
		t.Errorf("MaxDepth=8 rejected 4 levels: %v", err)
	}
}

func TestQMLErrorIdentity(t *testing.T) {
	// SyntaxError carries one of TWO identities depending on Incomplete
	// (parse.go: "it selects which of the two identities this error carries").
	// This is the D7 distinction expressed as error identity rather than as a
	// bool, and it is the form a reload path should branch on: errors.Is, not
	// a field read and not string matching.
	_, incomplete := parse.QML{}.Parse([]byte(`Column {`))
	if !errors.Is(incomplete, parse.ErrUnterminated) {
		t.Errorf("truncated input: want ErrUnterminated, got %v", incomplete)
	}
	if errors.Is(incomplete, parse.ErrSyntax) {
		t.Error("truncated input also matched ErrSyntax; the identities must be distinguishable")
	}

	_, wrong := parse.QML{}.Parse([]byte(`Column { 1: 2 }`))
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

func TestQMLHandlerRejectsABody(t *testing.T) {
	// The likeliest thing a QML author tries. The message has to explain the
	// rule, not just refuse the character.
	se := syntaxErr(t, `Button { onClicked: { doThing() } }`)
	if !strings.Contains(se.Want, "handler NAME") {
		t.Errorf("Want = %q, should explain that handlers bind by name", se.Want)
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
