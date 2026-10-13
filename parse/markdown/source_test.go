package markdown

import (
	"fmt"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/parse/languages"
	"strings"
	"testing"
)

func advanceSource(s highlight.Source, line string, state highlight.State) ([]highlight.Span, highlight.State) {
	spans, next := s.Highlighter.HighlightBlock(line, state)
	s.States.Retain(next)
	s.States.Release(state)
	for s.States.Collect(32) {
	}
	return spans, next
}

func TestMarkdownBorrowsAnInjectedProviderAndTranslatesOffsets(t *testing.T) {
	calls := 0
	r := highlight.NewRepository(languages.Definitions()...)
	r.Add(highlight.Definition{Name: "Custom", Extensions: []string{"*.custom"}, Aliases: []string{"custom"}, SourceFactory: func(c *highlight.Catalog) highlight.Source {
		calls++
		return highlight.Source{Highlighter: highlight.HighlighterFunc(func(line string, prev highlight.State) ([]highlight.Span, highlight.State) {
			at := strings.Index(line, "token")
			return []highlight.Span{{Start: at, End: at + 5, Style: highlight.Keyword}}, prev
		}), Indenter: indent.PolicyFunc(func(r indent.Request) (indent.Decision, bool) { return indent.Decision{Prefix: "   "}, r.StateKnown })}
	}})
	catalog := r.Snapshot()
	s := Definition().NewSource(catalog)
	_, state := advanceSource(s, "  ```custom", 0)
	text := "  é token"
	spans, state := advanceSource(s, text, state)
	if len(spans) != 1 || spans[0].Start != len("  é ") || spans[0].Style != highlight.Keyword {
		t.Fatal(spans)
	}
	d, ok := s.Indenter.Indent(indent.Request{Line: text, Column: len(text), StateKnown: true, PreviousState: int(state)})
	if !ok || d.Prefix != "     " || calls != 1 {
		t.Fatal(d, ok, calls)
	}
	_, state = advanceSource(s, "  ```", state)
	spans, _ = advanceSource(s, "# heading", state)
	if len(spans) == 0 || spans[0].Style != highlight.Keyword {
		t.Fatal(spans)
	}
}

func TestMarkdownChildRootsAreReclaimedDuringSameDocumentChurn(t *testing.T) {
	s := Definition().NewSource(nil)
	md := s.Highlighter.(*source)
	for i := 0; i < 500; i++ {
		_, state := advanceSource(s, "```shell", 0)
		_, state = advanceSource(s, fmt.Sprintf("cat <<EOF%d", i), state)
		_, state = advanceSource(s, "body", state)
		_, state = advanceSource(s, "```", state)
		s.States.Release(state)
		for s.States.Collect(1) {
		}
		if md.store.Len() != 0 {
			t.Fatal("old fence tuples retained", md.store.Len())
		}
		child := md.children["Shell"].Highlighter.(interface{ RetainedStates() int })
		if child.RetainedStates() != 0 {
			t.Fatal("old child states retained", child.RetainedStates())
		}
	}
}

func TestUnknownAndDocumentAdapterFencesStayPlain(t *testing.T) {
	r := highlight.NewRepository(Definition())
	s := Definition().NewSource(r.Snapshot())
	for _, name := range []string{"unknown", "markdown"} {
		_, state := advanceSource(s, "```"+name, 0)
		spans, state := advanceSource(s, "func is_plain", state)
		if len(spans) != 1 || spans[0].Style != highlight.String {
			t.Fatal(spans)
		}
		if _, ok := s.Indenter.Indent(indent.Request{Line: "func is_plain", StateKnown: true, PreviousState: int(state)}); ok {
			t.Fatal("unknown code invented an indent policy")
		}
		_, state = advanceSource(s, "```", state)
		s.States.Release(state)
	}
}

// A ```sql fence reads as SQL with the default catalog: SELECT is a keyword,
// at its place in the line, and each dialect's label reaches its own reading
// (# opens a comment only in MySQL).
func TestASQLFenceIsHighlightedAsSQL(t *testing.T) {
	for _, tc := range []struct {
		label, line, word string
		want              highlight.Style
	}{
		{"sql", "  SELECT id FROM t", "SELECT", highlight.Keyword},
		{"postgresql", "SELECT now()", "SELECT", highlight.Keyword},
		{"sqlite", "SELECT [id] FROM t", "SELECT", highlight.Keyword},
		{"mysql", "SELECT 1 # note", "# note", highlight.Comment},
		{"mariadb", "SELECT 1 # note", "# note", highlight.Comment},
	} {
		s := Definition().NewSource(nil)
		_, state := advanceSource(s, "```"+tc.label, 0)
		spans, state := advanceSource(s, tc.line, state)
		at := strings.Index(tc.line, tc.word)
		got := highlight.Normal
		for _, span := range spans {
			if span.Start <= at && at < span.End {
				got = span.Style
			}
		}
		if got != tc.want {
			t.Errorf("```%s: %q is %s, want %s; %+v", tc.label, tc.word, got, tc.want, spans)
		}
		_, state = advanceSource(s, "```", state)
		s.States.Release(state)
	}
}
