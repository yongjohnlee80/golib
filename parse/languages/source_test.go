package languages_test

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/parse/languages"
	"strings"
	"testing"
)

func source(t *testing.T, lang string) highlight.Source {
	t.Helper()
	catalog := highlight.NewRepository(languages.Definitions()...).Snapshot()
	d, ok := catalog.DefinitionForLanguage(lang)
	if !ok {
		t.Fatal(lang)
	}
	return d.NewSource(catalog)
}
func styleAt(t *testing.T, line string, spans []highlight.Span, word string) highlight.Style {
	t.Helper()
	at := strings.Index(line, word)
	if at < 0 {
		t.Fatal(word)
	}
	for _, span := range spans {
		if span.Start <= at && at < span.End {
			return span.Style
		}
	}
	return highlight.Normal
}
func line(s highlight.Source, text string, previous highlight.State) ([]highlight.Span, highlight.State) {
	spans, next := s.Highlighter.HighlightBlock(text, previous)
	if s.States != nil {
		s.States.Retain(next)
		s.States.Release(previous)
		s.States.Collect(10000)
	}
	return spans, next
}

func TestSourceLanguagesClassifyTheirVocabulary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lang, text, word string
		style            highlight.Style
	}{
		{"go", "func main() { println(42) }", "func", highlight.Keyword},
		{"rust", "fn main() { let x = true; }", "fn", highlight.Keyword},
		{"javascript", "const value = 1;", "const", highlight.Keyword},
		{"typescript", "interface Box { value: number }", "number", highlight.DataType},
		{"python", "def run(): return True", "def", highlight.Keyword},
		{"lua", "local value = nil", "local", highlight.Keyword},
		{"bash", "echo $HOME", "$HOME", highlight.Variable},
		{"yaml", "name: \"Ada\" # note", "name", highlight.Attribute},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			s := source(t, tc.lang)
			spans, _ := line(s, tc.text, 0)
			if got := styleAt(t, tc.text, spans, tc.word); got != tc.style {
				t.Fatalf("%q: %s, want %s; %+v", tc.text, got, tc.style, spans)
			}
		})
	}
}

func TestLanguageOwnedMultilineLiteralBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lang, opening, body, closing, word string
		style                              highlight.Style
	}{
		{"go", "var x = `", "if { return }", "`; func done() {}", "if", highlight.VerbatimString},
		{"rust", "/* outer /* inner", "fn ignored() */ still outer", "*/ fn done() {}", "fn", highlight.Comment},
		{"rust", "let x = r##\"", "fn not_code #\"", "\"##; fn done() {}", "fn", highlight.VerbatimString},
		{"python", "value = '''", "if value:", "'''; print(value)", "if", highlight.String},
		{"lua", "--[=[", "function ignored()", "]=] local x = 1", "function", highlight.Comment},
		{"lua", "local s = [==[", "end not_code", "]==]; local x = 1", "end", highlight.VerbatimString},
		{"shell", "cat <<'EOF'", "if then fi", "EOF", "if", highlight.VerbatimString},
	} {
		t.Run(tc.lang+tc.opening, func(t *testing.T) {
			s := source(t, tc.lang)
			_, state := line(s, tc.opening, 0)
			if state == 0 {
				t.Fatal("literal lost its state")
			}
			spans, state := line(s, tc.body, state)
			if got := styleAt(t, tc.body, spans, tc.word); got != tc.style {
				t.Fatalf("body is %s, want %s: %+v", got, tc.style, spans)
			}
			_, state = line(s, tc.closing, state)
			spans, _ = line(s, "return 1", state)
			if got := styleAt(t, "return 1", spans, "return"); got != highlight.ControlFlow {
				t.Fatalf("closing did not restore source: %s %+v", got, spans)
			}
		})
	}
}

func TestSourceIndentUsesVerifiedLanguageStructure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lang, opening, current string
		op                     indent.Operation
		want                   string
	}{
		{"go", "", "func main() {", indent.Newline, "\t"},
		{"rust", "", "fn main() {", indent.Newline, "    "},
		{"javascript", "", "function run() {", indent.Newline, "  "},
		{"typescript", "", "interface Box {", indent.Newline, "  "},
		{"python", "", "if ready:", indent.Newline, "    "},
		{"lua", "", "for i = 1, 3 do", indent.Newline, "  "},
		{"shell", "", "if true; then", indent.Newline, "  "},
		{"yaml", "", "root:", indent.Newline, "  "},
		{"go", "func main() {", "\t}", indent.Closing, ""},
		{"lua", "for i = 1, 3 do", "  end ", indent.Closing, ""},
		{"shell", "if true; then", "  fi ", indent.Closing, ""},
		{"python", "if ready:", "    else:", indent.Closing, ""},
		{"python", "", "print('if x:')", indent.Newline, ""},
		{"go", "", "println(\"{\")", indent.Newline, ""},
	} {
		t.Run(tc.lang+tc.current, func(t *testing.T) {
			s := source(t, tc.lang)
			state := highlight.State(0)
			if tc.opening != "" {
				_, state = line(s, tc.opening, state)
			}
			r := indent.Request{Line: tc.current, Column: len(tc.current), Operation: tc.op, StateKnown: true, PreviousState: int(state)}
			d, ok := s.Indenter.Indent(r)
			if !ok || d.Prefix != tc.want {
				t.Fatalf("%+v %v, want %q", d, ok, tc.want)
			}
			r.StateKnown = false
			if _, ok = s.Indenter.Indent(r); ok {
				t.Fatal("unverified context adjusted indentation")
			}
		})
	}
}

func TestJavaScriptTemplateInterpolationAndRegexpDivision(t *testing.T) {
	s := source(t, "js")
	text := "const s = `text ${call(42)} end`; return /ab[c/]/g; value / 2;"
	spans, _ := line(s, text, 0)
	for word, want := range map[string]highlight.Style{"text": highlight.String, "call": highlight.Function, "42": highlight.DecVal, "/ab": highlight.SpecialString, "/ 2": highlight.Operator} {
		if got := styleAt(t, text, spans, word); got != want {
			t.Errorf("%q is %s, want %s; %+v", word, got, want, spans)
		}
	}
}

func TestYAMLBlockScalarDoesNotParseItsContents(t *testing.T) {
	s := source(t, "yaml")
	_, state := line(s, "message: |", 0)
	spans, state := line(s, "  name: true # literal", state)
	if len(spans) != 1 || spans[0].Style != highlight.String {
		t.Fatal(spans)
	}
	spans, _ = line(s, "enabled: true", state)
	if styleAt(t, "enabled: true", spans, "true") != highlight.Constant {
		t.Fatal(spans)
	}
}

func TestJavaScriptMarkupSeparatesTextFromEmbeddedExpressions(t *testing.T) {
	s := source(t, "tsx")
	text := "const view = <Box title={call(1)}>return <span>{value}</span></Box>; const next = 2"
	spans, _ := line(s, text, 0)
	for word, want := range map[string]highlight.Style{"Box": highlight.DataType, "title": highlight.Attribute, "call": highlight.Function, "return": highlight.Normal, "next": highlight.Normal, "2": highlight.DecVal} {
		if got := styleAt(t, text, spans, word); got != want {
			t.Errorf("%q: %s, want %s; %+v", word, got, want, spans)
		}
	}
	_, state := line(s, "const view = <Box>", 0)
	spans, state = line(s, "return text", state)
	if styleAt(t, "return text", spans, "return") != highlight.Normal {
		t.Fatal(spans)
	}
	spans, _ = line(s, "</Box>; return 1", state)
	if styleAt(t, "</Box>; return 1", spans, "return") != highlight.ControlFlow {
		t.Fatal(spans)
	}
}

func TestRegexpAfterControlParenAndTypeScriptGenericArrow(t *testing.T) {
	s := source(t, "js")
	text := "if (ready) /done/.test(value)"
	spans, _ := line(s, text, 0)
	if styleAt(t, text, spans, "/done/") != highlight.SpecialString {
		t.Fatal(spans)
	}
	s = source(t, "ts")
	text = "const f = <T>(x: T) => x; return 1"
	spans, _ = line(s, text, 0)
	if styleAt(t, text, spans, "return") != highlight.ControlFlow {
		t.Fatal(spans)
	}
}

func TestExistingSpaceIndentationDoesNotBecomeMixedGoIndent(t *testing.T) {
	s := source(t, "go")
	_, state := line(s, "func f() {", 0)
	_, state = line(s, "    if ready {", state)
	d, ok := s.Indenter.Indent(indent.Request{Line: "        if nested {", Column: len("        if nested {"), Operation: indent.Newline, StateKnown: true, PreviousState: int(state)})
	if !ok || d.Prefix != "            " {
		t.Fatal(d, ok)
	}
}

func TestBranchTransitionsEnterTheirNewBodyAtEqualStackDepth(t *testing.T) {
	for _, tc := range []struct{ lang, opening, branch, want string }{
		{"python", "if ready:", "else:", "    "},
		{"python", "if ready:", "elif other:", "    "},
		{"python", "try:", "except Error:", "    "},
		{"python", "try:", "finally:", "    "},
		{"go", "if ready {", "} else {", "\t"},
		{"rust", "if ready {", "} else {", "    "},
		{"js", "if (ready) {", "} else {", "  "},
		{"lua", "if ready then", "else", "  "},
		{"lua", "if ready then", "elseif other then", "  "},
		{"shell", "if true; then", "else", "  "},
		{"shell", "if true; then", "elif other; then", "  "},
	} {
		t.Run(tc.lang+tc.branch, func(t *testing.T) {
			s := source(t, tc.lang)
			_, state := line(s, tc.opening, 0)
			for _, op := range []indent.Operation{indent.Newline, indent.OpenBelow} {
				d, ok := s.Indenter.Indent(indent.Request{Line: tc.branch, Column: len(tc.branch), Operation: op, StateKnown: true, PreviousState: int(state)})
				if !ok || d.Prefix != tc.want {
					t.Fatalf("operation %d: %+v %v, want %q", op, d, ok, tc.want)
				}
			}
			d, ok := s.Indenter.Indent(indent.Request{Line: tc.branch, Column: len(tc.branch), Operation: indent.Closing, StateKnown: true, PreviousState: int(state)})
			if !ok || d.Prefix != "" {
				t.Fatalf("closing line alignment changed: %+v %v", d, ok)
			}
		})
	}
}

func TestStateRetentionPlateausDuringSameDocumentChurn(t *testing.T) {
	s := source(t, "shell")
	stats := s.Highlighter.(interface{ RetainedStates() int })
	for i := 0; i < 2000; i++ {
		word := strings.Repeat("x", i%13+1) + string(rune('A'+i%26))
		_, state := line(s, "cat <<"+word, 0)
		_, next := line(s, "body", state)
		s.States.Release(next)
		for s.States.Collect(7) {
		}
		if n := stats.RetainedStates(); n != 0 {
			t.Fatalf("iteration %d retained %d old states", i, n)
		}
	}
}

func FuzzSourceSpanBoundsAndProgress(f *testing.F) {
	f.Add("js", "`hello ${f(1)}`")
	f.Add("rust", "/* nested /* */ */")
	f.Add("lua", "[=[é\x80]=]")
	f.Fuzz(func(t *testing.T, lang, text string) {
		catalog := highlight.NewRepository(languages.Definitions()...).Snapshot()
		d, ok := catalog.DefinitionForLanguage(lang)
		if !ok {
			return
		}
		s := d.NewSource(catalog)
		state := highlight.State(0)
		for _, l := range strings.Split(text, "\n") {
			var spans []highlight.Span
			spans, state = line(s, l, state)
			end := 0
			for _, span := range spans {
				if span.Start < end || span.End > len(l) || span.End <= span.Start {
					t.Fatalf("%q: %+v", l, spans)
				}
				end = span.End
			}
		}
	})
}
