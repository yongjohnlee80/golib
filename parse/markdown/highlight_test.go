package markdown_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/markdown"
)

// runs is a line's spans as `text=style` runs, in order.
func runs(line string, spans []highlight.Span) string {
	var out []string
	for _, s := range spans {
		out = append(out, fmt.Sprintf("%s=%s", line[s.Start:s.End], s.Style))
	}
	return strings.Join(out, " | ")
}

// TestTheHighlighterColoursADocument: one document, a line at a time, as an
// editor hands it over — each line's runs, and the frontmatter, the fence and
// the comment carried into the lines after the one that opened them.
func TestTheHighlighterColoursADocument(t *testing.T) {
	h := markdown.Highlighter()
	st := highlight.State(0)
	for _, c := range []struct{ line, want string }{
		{"---", "---=regionMarker"},
		{"title: Notes", "title=attribute | : Notes=documentation"},
		{"  - one", "  - one=documentation"}, // a list in YAML: no key
		{"---", "---=regionMarker"},
		{"# Heading `code`", "# Heading =keyword | `code`=verbatimString"},
		{"Some **bold** and *em* and ~~gone~~.", "**bold**=dataType | *em*=attribute | ~~gone~~=comment"},
		{"See [link](http://x), <http://y>, [[wiki]] and ![[pic.png]].", "[link](http://x)=string | <http://y>=import | [[wiki]]=function | ![[pic.png]]=function"},
		{"Tagged #idea here", "#idea=constant"},
		{"- [ ] a task", "- [ ] =operator"},
		{"12. an item", "12. =operator"},
		{"> [!warning] Careful", "> =operator | [!warning] Careful=warning"},
		{"> [!note] Aside", "> =operator | [!note] Aside=information"},
		{"> quoted **text**", "> =operator | **text**=dataType"},
		{"~~~~ go", "~~~~ go=preprocessor"},
		{"# not a heading in code", "# not a heading in code=verbatimString"},
		{"~~~", "~~~=verbatimString"}, // shorter than the fence: still inside
		{"~~~~", "~~~~=preprocessor"},
		{"text <!-- a comment", "<!-- a comment=comment"},
		{"*still* the comment", "*still* the comment=comment"},
		{"--> and *after*", "-->=comment | *after*=attribute"},
		{"---", "---=regionMarker"}, // a rule in the body, not frontmatter
		{"[ref]: http://z", "[ref]: http://z=import"},
		{"plain words", ""},
	} {
		spans, next := h.HighlightBlock(c.line, st)
		if got := runs(c.line, spans); got != c.want {
			t.Errorf("%q (from state %d):\n got %s\nwant %s", c.line, st, got, c.want)
		}
		st = next
	}
}

// TestFrontmatterOpensOnlyOnTheFirstLine: "---" further down is a rule, and
// the lines after it are the body.
func TestFrontmatterOpensOnlyOnTheFirstLine(t *testing.T) {
	h := markdown.Highlighter()
	_, st := h.HighlightBlock("intro", 0)
	_, st = h.HighlightBlock("---", st)
	spans, _ := h.HighlightBlock("title: not frontmatter", st)
	if got := runs("title: not frontmatter", spans); got != "" {
		t.Fatalf("a key after a rule in the body was coloured as frontmatter: %s", got)
	}
}

// TestSpansAreOrderedAndDisjoint: what an editor walks once, in order.
func TestSpansAreOrderedAndDisjoint(t *testing.T) {
	h := markdown.Highlighter()
	st := highlight.State(0)
	for _, line := range []string{"# A **b** `c` [[d]] #e", "- [x] **[l](u)** *i `c`*", "> [!tip] **t**", "|a|b|", "<b>x</b> *y*"} {
		spans, next := h.HighlightBlock(line, st)
		end := 0
		for _, s := range spans {
			if s.Start < end || s.End <= s.Start || s.End > len(line) {
				t.Fatalf("%q: spans %v are not ordered, disjoint and inside the line", line, spans)
			}
			end = s.End
		}
		st = next
	}
}
