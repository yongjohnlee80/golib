package html_test

import (
	"bytes"
	"sort"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

// GFMSpecVersion is the GitHub Flavored Markdown version the GFM checklist was written against. Its
// rules layer on the CommonMark version above; where the GFM spec leaves a case open, the rule says
// which reading is taken.
const GFMSpecVersion = "0.29-gfm"

// gfmRules is the GFM checklist. Ids are "gfm<section>/<name>", with the GFM spec's section numbers.
// Statements paraphrase https://github.github.com/gfm/.
var gfmRules = []rule{
	{ID: "gfm4.10/table", Statement: "a header row, then a delimiter row, then any data rows form a table"},
	{ID: "gfm4.10/delimiter-alignment", Statement: "delimiter cells are hyphens with an optional colon at the left, right or both ends, aligning the column"},
	{ID: "gfm4.10/cell-count-must-match", Statement: "the header row must have as many cells as the delimiter row"},
	{ID: "gfm4.10/edge-pipes-optional", Statement: "a row's leading and trailing pipes are optional, and spaces around cell content are trimmed", NoNegative: "edge pipes never change a row's cells"},
	{ID: "gfm4.10/escaped-pipe", Statement: "'\\|' puts a pipe in a cell, even inside a code span or emphasis"},
	{ID: "gfm4.10/table-ends", Statement: "a table ends at a blank line or another block's start; any other line is a row"},
	{ID: "gfm4.10/rows-padded-or-cut", Statement: "a row with fewer cells than the header gets empty ones; extra cells are dropped", NoNegative: "every row is fitted to the header; there is no near miss"},
	{ID: "gfm4.10/no-empty-body", Statement: "a table without data rows has no body", NoNegative: "a body exists exactly when rows do"},
	{ID: "gfm4.10/header-is-last-paragraph-line", Statement: "the header is the last line of the paragraph above the delimiter row; the lines before it stay a paragraph", NoNegative: "every earlier line stays paragraph text"},
	{ID: "gfm4.10/commonmark-starts-first", Statement: "a setext underline, thematic break or list item takes its line before it can be a delimiter row"},
	{ID: "gfm4.10/definitions-then-header", Statement: "definitions above the header are taken out first; with no line left, there is no header"},
	{ID: "gfm4.10/cells-hold-inlines", Statement: "cells hold inline content only, never blocks"},
	{ID: "gfm4.10/delimiter-indentation", Statement: "a delimiter row may be indented up to three spaces"},

	{ID: "gfm5.3/task-item", Statement: "an item whose content starts with '[ ]' or '[x]', then whitespace, on the marker's line, is a task item"},
	{ID: "gfm5.3/checked", Statement: "'x' or 'X' between the brackets checks it; whitespace leaves it unchecked"},
	{ID: "gfm5.3/marker-begins-paragraph", Statement: "the task marker begins a paragraph, so the rest of its line is paragraph text, not another block"},
	{ID: "gfm5.3/loose-task-list", Statement: "in a loose list the checkbox precedes the item's paragraph", NoNegative: "rendering form only"},

	{ID: "gfm6.5/strikethrough", Statement: "text between two-tilde runs is struck through; a run of one or of three or more tildes is text (the spec says two; cmark-gfm also accepts one)"},
	{ID: "gfm6.5/flanking", Statement: "two-tilde runs open and close by the flanking rules of '*'"},
	{ID: "gfm6.5/within-paragraph", Statement: "strikethrough cannot cross a paragraph boundary, but may cross a line ending"},
	{ID: "gfm6.5/nests-with-emphasis", Statement: "strikethrough and emphasis nest; when they overlap, the first closer wins"},

	{ID: "gfm6.9/www", Statement: "'www.' followed by a valid domain is a link to http:// and the text"},
	{ID: "gfm6.9/valid-domain", Statement: "a domain is segments of letters, digits, '_' and '-' separated by periods, with at least one period and no '_' in the last two segments (after 'www.', by the spec's reading)"},
	{ID: "gfm6.9/boundary", Statement: "an extended autolink starts only at a line's start, after whitespace, or after '*', '_', '~' or '(' (email ones too, by the spec's reading)"},
	{ID: "gfm6.9/trailing-punctuation", Statement: "a final '?', '!', '.', ',', ':', '*', '_' or '~' is not part of the link; inside it, they are"},
	{ID: "gfm6.9/trailing-parens", Statement: "final ')' not matched by a '(' in the link are not part of it; balanced or interior ones are"},
	{ID: "gfm6.9/entity-like-tail", Statement: "a final '&' and letters or digits then ';' is not part of the link"},
	{ID: "gfm6.9/less-than-ends", Statement: "'<' ends the link", NoNegative: "'<' always ends it"},
	{ID: "gfm6.9/url", Statement: "'http://', 'https://' or 'ftp://' and a valid domain start a link; other schemes do not (a domain needs a period, by the spec's reading)"},
	{ID: "gfm6.9/email", Statement: "letters, digits, '.', '-', '_' or '+', then '@', then a valid email domain is a mailto: link"},
	{ID: "gfm6.9/email-domain", Statement: "an email domain has no '+', ends in neither '-' nor '_', and loses a final '.'"},
	{ID: "gfm6.9/not-in-link-text", Statement: "extended autolinks are not read inside link text, or while a bracket is open"},
}

var casesGFM = []specCase{
	{"gfm4.10/table", true, "| a | b |\n| - | - |\n| c | d |\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>c</td>\n<td>d</td>\n</tr>\n</tbody>\n</table>\n"},
	{"gfm4.10/table", false, "| a |\n| b |\n", "<p>| a |\n| b |</p>\n"},
	{"gfm4.10/delimiter-alignment", true, "a | b | c | d\n:- | :-: | -: | -\n", "<table>\n<thead>\n<tr>\n<th align=\"left\">a</th>\n<th align=\"center\">b</th>\n<th align=\"right\">c</th>\n<th>d</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/delimiter-alignment", false, "| a |\n| -:- |\n", "<p>| a |\n| -:- |</p>\n"},
	{"gfm4.10/cell-count-must-match", true, "a | b\n-- | --\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/cell-count-must-match", false, "a | b\n-- |\n", "<p>a | b\n-- |</p>\n"},
	{"gfm4.10/edge-pipes-optional", true, "|  a|b  \n-|-|\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/escaped-pipe", true, "| a\\|b |\n| - |\n| `c\\|d` *e\\|f* |\n", "<table>\n<thead>\n<tr>\n<th>a|b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td><code>c|d</code> <em>e|f</em></td>\n</tr>\n</tbody>\n</table>\n"},
	{"gfm4.10/escaped-pipe", false, "| a|b |\n| - |\n", "<p>| a|b |\n| - |</p>\n"},
	{"gfm4.10/table-ends", true, "a\n-:\nb\n\nc\n", "<table>\n<thead>\n<tr>\n<th align=\"right\">a</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td align=\"right\">b</td>\n</tr>\n</tbody>\n</table>\n<p>c</p>\n"},
	{"gfm4.10/table-ends", true, "a\n-:\n> b\n", "<table>\n<thead>\n<tr>\n<th align=\"right\">a</th>\n</tr>\n</thead>\n</table>\n<blockquote>\n<p>b</p>\n</blockquote>\n"},
	{"gfm4.10/table-ends", false, "a\n-:\nb *c*\n", "<table>\n<thead>\n<tr>\n<th align=\"right\">a</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td align=\"right\">b <em>c</em></td>\n</tr>\n</tbody>\n</table>\n"},
	{"gfm4.10/rows-padded-or-cut", true, "a | b\n-- | --\nc\nd | e | f\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>c</td>\n<td></td>\n</tr>\n<tr>\n<td>d</td>\n<td>e</td>\n</tr>\n</tbody>\n</table>\n"},
	{"gfm4.10/no-empty-body", true, "| a |\n| - |\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/header-is-last-paragraph-line", true, "p *q*\nr\n| a |\n| - |\n", "<p>p <em>q</em>\nr</p>\n<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/commonmark-starts-first", true, "a\n:-\n", "<table>\n<thead>\n<tr>\n<th align=\"left\">a</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/commonmark-starts-first", false, "a\n--\n", "<h2>a</h2>\n"},
	{"gfm4.10/commonmark-starts-first", false, "a | b\n- | -\n", "<p>a | b</p>\n<ul>\n<li>| -</li>\n</ul>\n"},
	{"gfm4.10/definitions-then-header", true, "[r]: /u\n| [r] |\n| - |\n", "<table>\n<thead>\n<tr>\n<th><a href=\"/u\">r</a></th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/definitions-then-header", false, "[r]: /u\n| - |\n", "<p>| - |</p>\n"},
	{"gfm4.10/cells-hold-inlines", true, "| *a* | `b` |\n| - | - |\n", "<table>\n<thead>\n<tr>\n<th><em>a</em></th>\n<th><code>b</code></th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/cells-hold-inlines", false, "| # h |\n| - |\n", "<table>\n<thead>\n<tr>\n<th># h</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/delimiter-indentation", true, "| a |\n   | - |\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n</table>\n"},
	{"gfm4.10/delimiter-indentation", false, "| a |\n    | - |\n", "<p>| a |\n| - |</p>\n"},

	{"gfm5.3/task-item", true, "- [ ] a\n- [x] b\n", "<ul>\n<li><input disabled=\"\" type=\"checkbox\"> a</li>\n<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> b</li>\n</ul>\n"},
	{"gfm5.3/task-item", true, "1. [ ]\n   a\n", "<ol>\n<li><input disabled=\"\" type=\"checkbox\"> a</li>\n</ol>\n"},
	{"gfm5.3/task-item", false, "- [ ]a\n", "<ul>\n<li>[ ]a</li>\n</ul>\n"},
	{"gfm5.3/task-item", false, "[ ] a\n", "<p>[ ] a</p>\n"},
	{"gfm5.3/task-item", false, "- a\n  [ ] b\n", "<ul>\n<li>a\n[ ] b</li>\n</ul>\n"},
	{"gfm5.3/checked", true, "- [X] a\n- [\t] b\n", "<ul>\n<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> a</li>\n<li><input disabled=\"\" type=\"checkbox\"> b</li>\n</ul>\n"},
	{"gfm5.3/checked", false, "- [y] a\n", "<ul>\n<li>[y] a</li>\n</ul>\n"},
	{"gfm5.3/marker-begins-paragraph", true, "- [ ] # h\n", "<ul>\n<li><input disabled=\"\" type=\"checkbox\"> # h</li>\n</ul>\n"},
	{"gfm5.3/marker-begins-paragraph", false, "- # h\n", "<ul>\n<li>\n<h1>h</h1>\n</li>\n</ul>\n"},
	{"gfm5.3/loose-task-list", true, "- [ ] a\n\n- [x] b\n", "<ul>\n<li><input disabled=\"\" type=\"checkbox\"> \n<p>a</p>\n</li>\n<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> \n<p>b</p>\n</li>\n</ul>\n"},

	{"gfm6.5/strikethrough", true, "~~a~~ b\n", "<p><del>a</del> b</p>\n"},
	{"gfm6.5/strikethrough", false, "~a~ ~~~b~~~\n", "<p>~a~ ~~~b~~~</p>\n"},
	{"gfm6.5/flanking", true, "a~~b~~c\n", "<p>a<del>b</del>c</p>\n"},
	{"gfm6.5/flanking", false, "~~ a~~ ~~b ~~\n", "<p>~~ a~~ ~~b ~~</p>\n"},
	{"gfm6.5/within-paragraph", true, "~~a\nb~~\n", "<p><del>a\nb</del></p>\n"},
	{"gfm6.5/within-paragraph", false, "~~a\n\nb~~\n", "<p>~~a</p>\n<p>b~~</p>\n"},
	{"gfm6.5/nests-with-emphasis", true, "~~*a*~~ *~~b~~*\n", "<p><del><em>a</em></del> <em><del>b</del></em></p>\n"},
	{"gfm6.5/nests-with-emphasis", false, "*~~a*~~\n", "<p><em>~~a</em>~~</p>\n"},

	{"gfm6.9/www", true, "www.a.b/c?d\n", "<p><a href=\"http://www.a.b/c?d\">www.a.b/c?d</a></p>\n"},
	{"gfm6.9/www", false, "www. a.b\n", "<p>www. a.b</p>\n"},
	{"gfm6.9/valid-domain", true, "www.a_b.c-d.e\n", "<p><a href=\"http://www.a_b.c-d.e\">www.a_b.c-d.e</a></p>\n"},
	{"gfm6.9/valid-domain", true, "www.bücher.de\n", "<p><a href=\"http://www.b%C3%BCcher.de\">www.bücher.de</a></p>\n"},
	{"gfm6.9/valid-domain", false, "www.a.b_c www.a_b.c www.com\n", "<p>www.a.b_c www.a_b.c www.com</p>\n"},
	{"gfm6.9/boundary", true, "(www.a.b) *www.c.d*\n", "<p>(<a href=\"http://www.a.b\">www.a.b</a>) <em><a href=\"http://www.c.d\">www.c.d</a></em></p>\n"},
	{"gfm6.9/boundary", false, "xwww.a.b x.www.c.d x!e@f.g\n", "<p>xwww.a.b x.www.c.d x!e@f.g</p>\n"},
	{"gfm6.9/trailing-punctuation", true, "www.a.b/c.\n", "<p><a href=\"http://www.a.b/c\">www.a.b/c</a>.</p>\n"},
	{"gfm6.9/trailing-punctuation", false, "www.a.b/c.d\n", "<p><a href=\"http://www.a.b/c.d\">www.a.b/c.d</a></p>\n"},
	{"gfm6.9/trailing-parens", true, "(www.a.b/(c)))\n", "<p>(<a href=\"http://www.a.b/(c)\">www.a.b/(c)</a>))</p>\n"},
	{"gfm6.9/trailing-parens", false, "www.a.b/(c))d\n", "<p><a href=\"http://www.a.b/(c))d\">www.a.b/(c))d</a></p>\n"},
	{"gfm6.9/entity-like-tail", true, "www.a.b/?q&hl;\n", "<p><a href=\"http://www.a.b/?q\">www.a.b/?q</a>&amp;hl;</p>\n"},
	{"gfm6.9/entity-like-tail", false, "www.a.b/?q&hl=x\n", "<p><a href=\"http://www.a.b/?q&amp;hl=x\">www.a.b/?q&amp;hl=x</a></p>\n"},
	{"gfm6.9/less-than-ends", true, "www.a.b/c<d\n", "<p><a href=\"http://www.a.b/c\">www.a.b/c</a>&lt;d</p>\n"},
	{"gfm6.9/url", true, "http://a.b https://c.d/e ftp://f.g\n", "<p><a href=\"http://a.b\">http://a.b</a> <a href=\"https://c.d/e\">https://c.d/e</a> <a href=\"ftp://f.g\">ftp://f.g</a></p>\n"},
	{"gfm6.9/url", false, "gopher://a.b http://ab\n", "<p>gopher://a.b http://ab</p>\n"},
	{"gfm6.9/email", true, "a.b-c_d+e@f.g\n", "<p><a href=\"mailto:a.b-c_d+e@f.g\">a.b-c_d+e@f.g</a></p>\n"},
	{"gfm6.9/email", false, "a@b @c.d\n", "<p>a@b @c.d</p>\n"},
	{"gfm6.9/email-domain", true, "a@b.c.\n", "<p><a href=\"mailto:a@b.c\">a@b.c</a>.</p>\n"},
	{"gfm6.9/email-domain", false, "a@b+c.d a@b.c- a@b.c_\n", "<p>a@b+c.d a@b.c- a@b.c_</p>\n"},
	{"gfm6.9/not-in-link-text", true, "[x www.a.b a@c.d](/u)\n", "<p><a href=\"/u\">x www.a.b a@c.d</a></p>\n"},
	{"gfm6.9/not-in-link-text", true, "[x www.a.b\n", "<p>[x www.a.b</p>\n"},
	{"gfm6.9/not-in-link-text", false, "[x] www.a.b\n", "<p>[x] <a href=\"http://www.a.b\">www.a.b</a></p>\n"},
}

func renderGFM(t *testing.T, in string) string {
	t.Helper()
	var b bytes.Buffer
	if err := html.Render(&b, markdown.Parse([]byte(in), markdown.GFM()), html.Unsafe()); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestGFM(t *testing.T) {
	for _, c := range casesGFM {
		if got := renderGFM(t, c.In); got != c.Want {
			t.Errorf("%s (pos=%v)\n in: %q\ngot: %q\nwant %q", c.Rule, c.Pos, c.In, got, c.Want)
		}
	}
}

// TestGFMRuleCoverage holds the GFM checklist to the CommonMark one's standard.
func TestGFMRuleCoverage(t *testing.T) {
	pos, neg, known := map[string]int{}, map[string]int{}, map[string]bool{}
	for _, r := range gfmRules {
		if known[r.ID] {
			t.Errorf("rule %s is listed twice", r.ID)
		}
		known[r.ID] = true
	}
	for _, c := range casesGFM {
		if !known[c.Rule] {
			t.Errorf("case for unknown rule %q (in: %q)", c.Rule, c.In)
		}
		if c.Pos {
			pos[c.Rule]++
		} else {
			neg[c.Rule]++
		}
	}
	var exempt []string
	for _, r := range gfmRules {
		if pos[r.ID] == 0 {
			t.Errorf("rule %s has no positive case", r.ID)
		}
		if neg[r.ID] == 0 {
			if r.NoNegative == "" {
				t.Errorf("rule %s has no negative case and no NoNegative reason", r.ID)
			} else {
				exempt = append(exempt, r.ID+": "+r.NoNegative)
			}
		}
	}
	sort.Strings(exempt)
	for _, e := range exempt {
		t.Logf("no negative case, by exemption — %s", e)
	}
	t.Logf("GFM %s: %d rules, %d cases", GFMSpecVersion, len(gfmRules), len(casesGFM))
}

// TestGFMInvariance runs every CommonMark case the GFM extension does not recognize with GFM on:
// the output must not change. Selection is by Recognizes, not by hand, so a collision nobody
// thought of still fails here.
func TestGFMInvariance(t *testing.T) {
	gfm := markdown.GFMExtension()
	ran, skipped := 0, 0
	for _, c := range allCases() {
		if gfm.Recognizes([]byte(c.In)) {
			skipped++
			continue
		}
		ran++
		if got := renderGFM(t, c.In); got != c.Want {
			t.Errorf("%s: GFM changed a document it does not recognize\n in: %q\ngot: %q\nwant %q", c.Rule, c.In, got, c.Want)
		}
	}
	t.Logf("%d CommonMark cases unchanged under GFM; %d recognized and left to the collision cases", ran, skipped)
}

// TestGFMCollisions pins valid CommonMark that GFM reads differently, both ways.
func TestGFMCollisions(t *testing.T) {
	for _, c := range []struct{ in, commonmark, gfm string }{
		{"| a |\n| - |\n", "<p>| a |\n| - |</p>\n", "<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n</table>\n"},
		{"~~a~~\n", "<p>~~a~~</p>\n", "<p><del>a</del></p>\n"},
		{"- [x] a\n", "<ul>\n<li>[x] a</li>\n</ul>\n", "<ul>\n<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> a</li>\n</ul>\n"},
		{"www.a.b\n", "<p>www.a.b</p>\n", "<p><a href=\"http://www.a.b\">www.a.b</a></p>\n"},
		{"a@b.c\n", "<p>a@b.c</p>\n", "<p><a href=\"mailto:a@b.c\">a@b.c</a></p>\n"},
		{"- [x] a\n\n[x]: /u\n", "<ul>\n<li><a href=\"/u\">x</a> a</li>\n</ul>\n", "<ul>\n<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> a</li>\n</ul>\n"},
	} {
		if !markdown.GFMExtension().Recognizes([]byte(c.in)) {
			t.Errorf("Recognizes misses %q, which GFM reads differently", c.in)
		}
		if got := render(t, c.in); got != c.commonmark {
			t.Errorf("CommonMark %q = %q, want %q", c.in, got, c.commonmark)
		}
		if got := renderGFM(t, c.in); got != c.gfm {
			t.Errorf("GFM %q = %q, want %q", c.in, got, c.gfm)
		}
	}
}

// TestGFMRecognizesEveryCase checks the other direction of Recognizes: every GFM rule case whose
// output differs from CommonMark's must be recognized, or the invariance run could skip nothing
// and still pass.
func TestGFMRecognizesEveryCase(t *testing.T) {
	for _, c := range casesGFM {
		if render(t, c.In) != renderGFM(t, c.In) && !markdown.GFMExtension().Recognizes([]byte(c.In)) {
			t.Errorf("%s: %q reads differently under GFM but is not recognized", c.Rule, c.In)
		}
	}
}
