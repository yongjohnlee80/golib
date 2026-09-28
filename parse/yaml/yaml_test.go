package yaml_test

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/yongjohnlee80/golib/parse/yaml"
)

func mustParse(t *testing.T, src string, opts ...yaml.Option) *yaml.Stream {
	t.Helper()
	st, err := yaml.Parse([]byte(src), opts...)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return st
}

func text(st *yaml.Stream, n *yaml.Node) string { return string(st.Source[n.Span.Start:n.Span.End]) }

// TestTreeHoldsText: the parser resolves nothing, so "yes" is a plain scalar, not a bool.
func TestTreeHoldsText(t *testing.T) {
	st := mustParse(t, "a: yes\nb: !!int 0x1F\nc: ~\n")
	m := st.Docs[0].Root
	for i, want := range []struct{ value, tag string }{{"yes", ""}, {"0x1F", "tag:yaml.org,2002:int"}, {"~", ""}} {
		v := m.Pairs[i].Value
		if v.Kind != yaml.KindScalar || v.Style != yaml.StylePlain || string(v.Value) != want.value || v.Tag != want.tag {
			t.Errorf("pair %d value = %s %q tag %q, want the plain scalar %q tag %q", i, v.Kind, v.Value, v.Tag, want.value, want.tag)
		}
	}
}

func TestPositionsCountLineBreaks(t *testing.T) {
	st := mustParse(t, "a: 1\rb: 2\r\nc: é3\n")
	m := st.Docs[0].Root
	for i, want := range []struct{ line, col int }{{1, 4}, {2, 4}, {3, 4}} {
		p := st.Position(m.Pairs[i].Value.Span.Start)
		if p.Line != want.line || p.Column != want.col {
			t.Errorf("value %d at %d:%d, want %d:%d", i, p.Line, p.Column, want.line, want.col)
		}
	}
	// columns count characters: é is one column, so "3" follows at column 5
	if p := st.Position(m.Pairs[2].Value.Span.Start + len("é")); p.Column != 5 {
		t.Errorf("after a two-byte character, column %d, want 5", p.Column)
	}
}

func encode(s string, enc yaml.Encoding, bom bool) []byte {
	var out []byte
	r := []rune(s)
	if bom {
		r = append([]rune{0xFEFF}, r...)
	}
	switch enc {
	case yaml.UTF16LE, yaml.UTF16BE:
		for _, u := range utf16.Encode(r) {
			if enc == yaml.UTF16LE {
				out = append(out, byte(u), byte(u>>8))
			} else {
				out = append(out, byte(u>>8), byte(u))
			}
		}
	case yaml.UTF32LE, yaml.UTF32BE:
		for _, c := range r {
			v := uint32(c)
			if enc == yaml.UTF32LE {
				out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
			} else {
				out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
			}
		}
	default:
		out = []byte(string(r))
	}
	return out
}

func events(t *testing.T, src []byte) string {
	t.Helper()
	var evs []yaml.Event
	for ev, err := range yaml.Events(src) {
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		evs = append(evs, ev)
	}
	return eventNotation(evs)
}

// TestEncodings: UTF-16 and UTF-32 input, with a byte order mark or known by its null bytes, parses
// to the events of its UTF-8 form, at the same lines and columns.
func TestEncodings(t *testing.T) {
	const doc = "k: \"é𝄞\"\nl: [1, 2]\n"
	want := events(t, []byte(doc))
	wantPos := mustParse(t, doc).Position(len("k: \"é𝄞\"\nl: "))
	for _, enc := range []yaml.Encoding{yaml.UTF16LE, yaml.UTF16BE, yaml.UTF32LE, yaml.UTF32BE} {
		for _, bom := range []bool{true, false} {
			src := encode(doc, enc, bom)
			if got := events(t, src); got != want {
				t.Errorf("encoding %d (bom %v): events\n%s want\n%s", enc, bom, got, want)
			}
			st, err := yaml.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			if st.Encoding != enc {
				t.Errorf("encoding %d (bom %v) read as %d", enc, bom, st.Encoding)
			}
			seq := st.Docs[0].Root.Pairs[1].Value
			if p := st.Position(seq.Span.Start); p.Line != wantPos.Line || p.Column != wantPos.Column {
				t.Errorf("encoding %d (bom %v): the sequence at %d:%d, want %d:%d", enc, bom, p.Line, p.Column, wantPos.Line, wantPos.Column)
			}
		}
	}
}

// TestMaxDepth: the bound holds in Parse and in Events, and a million levels is an error, not a
// stack overflow.
func TestMaxDepth(t *testing.T) {
	deep := "[[[[a]]]]"
	if _, err := yaml.Parse([]byte(deep), yaml.MaxDepth(3)); err == nil {
		t.Error("Parse: four levels under MaxDepth(3) parsed")
	}
	var evErr error
	for _, err := range yaml.Events([]byte(deep), yaml.MaxDepth(3)) {
		if err != nil {
			evErr = err
		}
	}
	if evErr == nil {
		t.Error("Events: four levels under MaxDepth(3) streamed without an error")
	}
	if _, err := yaml.Parse([]byte(deep), yaml.MaxDepth(4)); err != nil {
		t.Errorf("four levels under MaxDepth(4): %v", err)
	}
	for _, src := range []string{strings.Repeat("[", 1_000_000), strings.Repeat("- ", 1_000_000) + "a"} {
		_, err := yaml.Parse([]byte(src))
		var ye *yaml.Error
		if !errors.As(err, &ye) {
			t.Errorf("a million levels (%q…): %v, want an *Error", src[:6], err)
		}
	}
}

// TestAliases: an alias takes the most recent node with its anchor; an undefined one is an error.
func TestAliases(t *testing.T) {
	st := mustParse(t, "- &a first\n- *a\n- &a second\n- *a\n")
	items := st.Docs[0].Root.Items
	if items[1].Target != items[0] || items[3].Target != items[2] {
		t.Errorf("aliases bound to %q and %q, want first and second", items[1].Target.Value, items[3].Target.Value)
	}
	if items[1].Kind != yaml.KindAlias || items[1].Alias != "a" {
		t.Errorf("alias node %s %q", items[1].Kind, items[1].Alias)
	}
	for _, src := range []string{"- *a\n- &a x\n", "--- &a x\n--- *a\n"} {
		var ye *yaml.Error
		if _, err := yaml.Parse([]byte(src)); !errors.As(err, &ye) {
			t.Errorf("%q: %v, want an *Error: an alias before its anchor, or in another document", src, err)
		}
		// the event stream says so too, without a tree to bind against
		var evErr error
		for _, err := range yaml.Events([]byte(src)) {
			if err != nil {
				evErr = err
			}
		}
		if !errors.As(evErr, &ye) {
			t.Errorf("Events(%q): %v, want an *Error", src, evErr)
		}
	}
}

func TestErrorsHavePositions(t *testing.T) {
	_, err := yaml.Parse([]byte("a: 1\nb: [1, 2\n"))
	var ye *yaml.Error
	if !errors.As(err, &ye) || ye.Pos.Line < 2 || ye.Msg == "" {
		t.Fatalf("an unclosed flow sequence: %v", err)
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "yaml: "+strconv.Itoa(ye.Pos.Line)+":") {
		t.Errorf("the message %q does not lead with the position", msg)
	}
	if yaml.KindMapping.String() != "mapping" || yaml.KindAlias.String() != "alias" {
		t.Error("Kind names")
	}
}

func TestSpans(t *testing.T) {
	st := mustParse(t, "key: value\nq: 'it''s'\nf: [a, {b: c}]\nb: |\n  lit\n")
	m := st.Docs[0].Root
	for i, want := range []string{"value", "'it''s'", "[a, {b: c}]", "|\n  lit\n"} {
		if got := text(st, m.Pairs[i].Value); got != want {
			t.Errorf("value %d spans %q, want %q", i, got, want)
		}
	}
	checkSpans(t, st.Source, m, yaml.Span{Start: 0, End: len(st.Source)})
}

func checkSpans(t *testing.T, src []byte, n *yaml.Node, parent yaml.Span) {
	t.Helper()
	if n == nil {
		return
	}
	if n.Span.Start < parent.Start || n.Span.End > parent.End || n.Span.Start > n.Span.End {
		t.Fatalf("%s spans %v, outside %v (input %q)", n.Kind, n.Span, parent, src)
	}
	for _, c := range n.Items {
		checkSpans(t, src, c, n.Span)
	}
	for _, p := range n.Pairs {
		checkSpans(t, src, p.Key, n.Span)
		checkSpans(t, src, p.Value, n.Span)
	}
}

// treeNotation writes a parsed stream back as events, to compare Parse's tree with Events.
func treeNotation(st *yaml.Stream) string {
	var evs []yaml.Event
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		switch n.Kind {
		case yaml.KindAlias:
			evs = append(evs, yaml.Event{Kind: yaml.EventAlias, Alias: n.Alias})
		case yaml.KindScalar:
			evs = append(evs, yaml.Event{Kind: yaml.EventScalar, Anchor: n.Anchor, Tag: n.Tag, Style: n.Style, Value: n.Value})
		case yaml.KindSequence:
			evs = append(evs, yaml.Event{Kind: yaml.EventSequenceStart, Anchor: n.Anchor, Tag: n.Tag, Style: n.Style})
			for _, c := range n.Items {
				walk(c)
			}
			evs = append(evs, yaml.Event{Kind: yaml.EventSequenceEnd})
		case yaml.KindMapping:
			evs = append(evs, yaml.Event{Kind: yaml.EventMappingStart, Anchor: n.Anchor, Tag: n.Tag, Style: n.Style})
			for _, p := range n.Pairs {
				walk(p.Key)
				walk(p.Value)
			}
			evs = append(evs, yaml.Event{Kind: yaml.EventMappingEnd})
		}
	}
	evs = append(evs, yaml.Event{Kind: yaml.EventStreamStart})
	for _, d := range st.Docs {
		evs = append(evs, yaml.Event{Kind: yaml.EventDocumentStart, Explicit: d.ExplicitStart})
		walk(d.Root)
		evs = append(evs, yaml.Event{Kind: yaml.EventDocumentEnd, Explicit: d.ExplicitEnd})
	}
	evs = append(evs, yaml.Event{Kind: yaml.EventStreamEnd})
	return eventNotation(evs)
}

// FuzzParse: no input panics; a stream that parses has spans nested in their parents and in the
// source, and its tree says exactly what Events says.
func FuzzParse(f *testing.F) {
	dirs, _ := filepath.Glob(filepath.Join(suiteDir, "*", "in.yaml"))
	subs, _ := filepath.Glob(filepath.Join(suiteDir, "*", "*", "in.yaml"))
	for _, p := range append(dirs, subs...) {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		var evs []yaml.Event
		var evErr error
		for ev, err := range yaml.Events(in) {
			if err != nil {
				evErr = err
				break
			}
			evs = append(evs, ev)
		}
		st, err := yaml.Parse(in)
		if (err == nil) != (evErr == nil) {
			t.Fatalf("Parse error %v, Events error %v (input %q)", err, evErr, in)
		}
		if err != nil {
			return
		}
		for _, d := range st.Docs {
			checkSpans(t, st.Source, d.Root, yaml.Span{Start: 0, End: len(st.Source)})
		}
		if got, want := treeNotation(st), eventNotation(evs); got != want {
			t.Fatalf("the tree says\n%s\nEvents says\n%s\n(input %q)", got, want, in)
		}
	})
}

// TestParserResolvesNothing pins the split: parse/yaml imports no schema code.
func TestParserResolvesNothing(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if path := strings.Trim(imp.Path.Value, `"`); path == "github.com/yongjohnlee80/golib/yaml" {
				t.Errorf("%s imports %s: the parser must not evaluate", name, path)
			}
		}
	}
}
