package html_test

import (
	"bytes"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

// ObsidianVersion names what the Obsidian checklist was written against: Obsidian's help pages
// (github.com/obsidianmd/obsidian-help), read on this date. They describe the syntax rather than
// specify it, so where they leave a case open, the rule says which reading is taken.
const ObsidianVersion = "obsidian-help 2026-09-28"

// obsidianRules is the Obsidian checklist. Ids are "obs/<construct>/<name>".
var obsidianRules = []rule{
	{ID: "obs/frontmatter/at-start", Statement: "a frontmatter block is recognized only at the very start of the note"},
	{ID: "obs/frontmatter/needs-closing-line", Statement: "without a closing '---' line there is no frontmatter, and the lines are ordinary Markdown"},
	{ID: "obs/frontmatter/fence-exact", Statement: "each fence is exactly '---', with trailing spaces or tabs allowed"},
	{ID: "obs/wikilink/basic", Statement: "[[target]] with a non-blank target is a wikilink"},
	{ID: "obs/wikilink/alias", Statement: "'|' starts the displayed alias; a blank alias shows the target"},
	{ID: "obs/wikilink/heading-and-block", Statement: "'#' starts a heading (nested ones kept), '#^' a block id; an empty page is the same note"},
	{ID: "obs/wikilink/one-line", Statement: "a wikilink's content is on one line and holds no other '[['"},
	{ID: "obs/wikilink/raw-content", Statement: "a wikilink's content is read as written, with no inline markup", NoNegative: "content is always raw"},
	{ID: "obs/wikilink/innermost-link-wins", Statement: "a wikilink is a link: as links do not nest, no '[' before it opens a link around it; an image may still hold one"},
	{ID: "obs/wikilink/before-link-brackets", Statement: "a wikilink is read before CommonMark's link brackets, so [[a]] is never a reference link inside brackets"},
	{ID: "obs/embed", Statement: "'!' directly before a wikilink makes it an embed, which, like an image, may sit inside link text"},
	{ID: "obs/tag/basic", Statement: "'#' then letters, digits, '_', '-' or '/' is a tag, but not inside a word or in code"},
	{ID: "obs/tag/needs-non-digit", Statement: "a tag holds at least one character that is not a digit"},
	{ID: "obs/tag/boundary", Statement: "a tag starts a line or follows whitespace (a '(' before it is a word's edge, by this reading)"},
	{ID: "obs/tag/ends-at-punctuation", Statement: "punctuation other than '_', '-' and '/' ends a tag", NoNegative: "it always ends there"},
	{ID: "obs/tag/unicode", Statement: "non-ASCII letters, digits and symbols such as emoji are tag characters", NoNegative: "they always are"},
	{ID: "obs/tag/not-a-heading", Statement: "'#' then a space is still an ATX heading; '#' then a tag character at a line start is a tag"},
	{ID: "obs/callout/basic", Statement: "[!type] at the start of a block quote's first line makes it a callout"},
	{ID: "obs/callout/fold", Statement: "'+' or '-' directly after the marker makes it foldable, expanded or collapsed"},
	{ID: "obs/callout/type-case", Statement: "types compare case-insensitively, rendered lowercased", NoNegative: "every case form is the same type"},
	{ID: "obs/callout/default-title", Statement: "a callout without a title is titled by its type", NoNegative: "rendering form only"},
	{ID: "obs/callout/title-apart", Statement: "the rest of the marker line is the title, not part of the body's first paragraph"},
	{ID: "obs/callout/type-chars", Statement: "a type is one or more characters other than whitespace and ']'"},
	{ID: "obs/callout/nested", Statement: "callouts nest as block quotes do", NoNegative: "nesting has no near miss"},
	{ID: "obs/callout/no-lazy-title", Statement: "a title is not a paragraph, so a following line without '>' is not its lazy continuation", NoNegative: "the title never continues"},
}

var casesObsidian = []specCase{
	{"obs/frontmatter/at-start", true, "---\na: 1\n---\n# H\n", "<h1>H</h1>\n"},
	{"obs/frontmatter/at-start", false, "x\n---\na: 1\n---\n", "<h2>x</h2>\n<h2>a: 1</h2>\n"},
	{"obs/frontmatter/needs-closing-line", true, "---\n---\n", ""},
	{"obs/frontmatter/needs-closing-line", false, "---\na: 1\n", "<hr />\n<p>a: 1</p>\n"},
	{"obs/frontmatter/fence-exact", true, "---  \na\n---\t\nb\n", "<p>b</p>\n"},
	{"obs/frontmatter/fence-exact", false, "----\na\n----\n", "<hr />\n<h2>a</h2>\n"},
	{"obs/wikilink/basic", true, "a [[Page one]] b\n", "<p>a <a class=\"wikilink\" href=\"Page%20one\">Page one</a> b</p>\n"},
	{"obs/wikilink/basic", false, "[[ ]] [[]]\n", "<p>[[ ]] [[]]</p>\n"},
	{"obs/wikilink/alias", true, "[[P|Shown *x*]]\n", "<p><a class=\"wikilink\" href=\"P\">Shown *x*</a></p>\n"},
	{"obs/wikilink/alias", false, "[[P| ]]\n", "<p><a class=\"wikilink\" href=\"P\">P</a></p>\n"},
	{"obs/wikilink/heading-and-block", true, "[[P#H#Sub]] [[#H]] [[P#^b1]] [[P#H#^b2]]\n", "<p><a class=\"wikilink\" href=\"P#H#Sub\">P#H#Sub</a> <a class=\"wikilink\" href=\"#H\">#H</a> <a class=\"wikilink\" href=\"P#%5Eb1\">P#^b1</a> <a class=\"wikilink\" href=\"P#H#%5Eb2\">P#H#^b2</a></p>\n"},
	{"obs/wikilink/heading-and-block", false, "[[P#a^b]]\n", "<p><a class=\"wikilink\" href=\"P#a%5Eb\">P#a^b</a></p>\n"},
	{"obs/wikilink/one-line", true, "[[a]] [[b]]\n", "<p><a class=\"wikilink\" href=\"a\">a</a> <a class=\"wikilink\" href=\"b\">b</a></p>\n"},
	{"obs/wikilink/one-line", false, "[[a\nb]] [[c[[d]]\n", "<p>[[a\nb]] [[c<a class=\"wikilink\" href=\"d\">d</a></p>\n"},
	{"obs/wikilink/raw-content", true, "[[a*b*`c`]]\n", "<p><a class=\"wikilink\" href=\"a*b*%60c%60\">a*b*`c`</a></p>\n"},
	{"obs/wikilink/innermost-link-wins", true, "[x [[P]]](/u)\n", "<p>[x <a class=\"wikilink\" href=\"P\">P</a>](/u)</p>\n"},
	{"obs/wikilink/innermost-link-wins", false, "![x [[P]]](/u)\n", "<p><img src=\"/u\" alt=\"x P\" /></p>\n"},
	{"obs/wikilink/before-link-brackets", true, "[[a]]\n\n[a]: /u\n", "<p><a class=\"wikilink\" href=\"a\">a</a></p>\n"},
	{"obs/wikilink/before-link-brackets", false, "[a]\n\n[a]: /u\n", "<p><a href=\"/u\">a</a></p>\n"},
	{"obs/embed", true, "![[img.png|100x145]] ![[Note#H]]\n", "<p><span class=\"embed\" data-href=\"img.png\">100x145</span> <span class=\"embed\" data-href=\"Note#H\">Note#H</span></p>\n"},
	{"obs/embed", true, "[x ![[i]]](/u)\n", "<p><a href=\"/u\">x <span class=\"embed\" data-href=\"i\">i</span></a></p>\n"},
	{"obs/embed", false, "! [[P]]\n", "<p>! <a class=\"wikilink\" href=\"P\">P</a></p>\n"},
	{"obs/tag/basic", true, "#tag and #a/b-c_d\n", "<p><a class=\"tag\" href=\"#tag\">#tag</a> and <a class=\"tag\" href=\"#a/b-c_d\">#a/b-c_d</a></p>\n"},
	{"obs/tag/basic", false, "a#b `#c` C#\n", "<p>a#b <code>#c</code> C#</p>\n"},
	{"obs/tag/needs-non-digit", true, "#y1984 #1984y\n", "<p><a class=\"tag\" href=\"#y1984\">#y1984</a> <a class=\"tag\" href=\"#1984y\">#1984y</a></p>\n"},
	{"obs/tag/needs-non-digit", false, "#1984\n", "<p>#1984</p>\n"},
	{"obs/tag/boundary", true, "a\n#t\tu #v\n", "<p>a\n<a class=\"tag\" href=\"#t\">#t</a>\tu <a class=\"tag\" href=\"#v\">#v</a></p>\n"},
	{"obs/tag/boundary", false, "(#x) *#y*\n", "<p>(#x) <em>#y</em></p>\n"},
	{"obs/tag/ends-at-punctuation", true, "#tag. #a,b\n", "<p><a class=\"tag\" href=\"#tag\">#tag</a>. <a class=\"tag\" href=\"#a\">#a</a>,b</p>\n"},
	{"obs/tag/unicode", true, "#日本 #🎉ok\n", "<p><a class=\"tag\" href=\"#%E6%97%A5%E6%9C%AC\">#日本</a> <a class=\"tag\" href=\"#%F0%9F%8E%89ok\">#🎉ok</a></p>\n"},
	{"obs/tag/not-a-heading", true, "#h\n", "<p><a class=\"tag\" href=\"#h\">#h</a></p>\n"},
	{"obs/tag/not-a-heading", false, "# h #t\n", "<h1>h <a class=\"tag\" href=\"#t\">#t</a></h1>\n"},
	{"obs/callout/basic", true, "> [!info] T\n> b\n", "<div class=\"callout\" data-callout=\"info\">\n<div class=\"callout-title\">T</div>\n<div class=\"callout-content\">\n<p>b</p>\n</div>\n</div>\n"},
	{"obs/callout/basic", false, "> x\n> [!info] T\n", "<blockquote>\n<p>x\n[!info] T</p>\n</blockquote>\n"},
	{"obs/callout/fold", true, "> [!faq]- Q\n> a\n", "<div class=\"callout\" data-callout=\"faq\" data-callout-fold=\"-\">\n<div class=\"callout-title\">Q</div>\n<div class=\"callout-content\">\n<p>a</p>\n</div>\n</div>\n"},
	{"obs/callout/fold", true, "> [!faq]+\n", "<div class=\"callout\" data-callout=\"faq\" data-callout-fold=\"+\">\n<div class=\"callout-title\">Faq</div>\n</div>\n"},
	{"obs/callout/fold", false, "> [!faq]* Q\n", "<div class=\"callout\" data-callout=\"faq\">\n<div class=\"callout-title\">* Q</div>\n</div>\n"},
	{"obs/callout/type-case", true, "> [!TIP]\n", "<div class=\"callout\" data-callout=\"tip\">\n<div class=\"callout-title\">Tip</div>\n</div>\n"},
	{"obs/callout/default-title", true, "> [!note]\n> a\n", "<div class=\"callout\" data-callout=\"note\">\n<div class=\"callout-title\">Note</div>\n<div class=\"callout-content\">\n<p>a</p>\n</div>\n</div>\n"},
	{"obs/callout/title-apart", true, "> [!info] T\n> body\n", "<div class=\"callout\" data-callout=\"info\">\n<div class=\"callout-title\">T</div>\n<div class=\"callout-content\">\n<p>body</p>\n</div>\n</div>\n"},
	{"obs/callout/title-apart", false, "> [info] T\n> body\n", "<blockquote>\n<p>[info] T\nbody</p>\n</blockquote>\n"},
	{"obs/callout/type-chars", true, "> [!my-type_2] x\n", "<div class=\"callout\" data-callout=\"my-type_2\">\n<div class=\"callout-title\">x</div>\n</div>\n"},
	{"obs/callout/type-chars", false, "> [!] x\n", "<blockquote>\n<p>[!] x</p>\n</blockquote>\n"},
	{"obs/callout/type-chars", false, "> [!a b] y\n", "<blockquote>\n<p>[!a b] y</p>\n</blockquote>\n"},
	{"obs/callout/nested", true, "> [!q] Q\n> > [!todo] Y\n", "<div class=\"callout\" data-callout=\"q\">\n<div class=\"callout-title\">Q</div>\n<div class=\"callout-content\">\n<div class=\"callout\" data-callout=\"todo\">\n<div class=\"callout-title\">Y</div>\n</div>\n</div>\n</div>\n"},
	{"obs/callout/no-lazy-title", true, "> [!note] T\nlazy\n", "<div class=\"callout\" data-callout=\"note\">\n<div class=\"callout-title\">T</div>\n</div>\n<p>lazy</p>\n"},
}

func renderWithOpts(t *testing.T, in string, opts ...markdown.Option) string {
	t.Helper()
	var b bytes.Buffer
	if err := html.Render(&b, markdown.Parse([]byte(in), opts...), html.Unsafe()); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestObsidian(t *testing.T) {
	for _, c := range casesObsidian {
		if got := renderWithOpts(t, c.In, markdown.Obsidian()); got != c.Want {
			t.Errorf("%s (pos=%v)\n in: %q\ngot: %q\nwant %q", c.Rule, c.Pos, c.In, got, c.Want)
		}
	}
}

func TestObsidianRuleCoverage(t *testing.T) {
	pos, neg, known := map[string]int{}, map[string]int{}, map[string]bool{}
	for _, r := range obsidianRules {
		if known[r.ID] {
			t.Errorf("rule %s is listed twice", r.ID)
		}
		known[r.ID] = true
	}
	for _, c := range casesObsidian {
		if !known[c.Rule] {
			t.Errorf("case for unknown rule %q (in: %q)", c.Rule, c.In)
		}
		if c.Pos {
			pos[c.Rule]++
		} else {
			neg[c.Rule]++
		}
	}
	for _, r := range obsidianRules {
		if pos[r.ID] == 0 {
			t.Errorf("rule %s has no positive case", r.ID)
		}
		if neg[r.ID] == 0 && r.NoNegative == "" {
			t.Errorf("rule %s has no negative case and no NoNegative reason", r.ID)
		} else if neg[r.ID] == 0 {
			t.Logf("no negative case, by exemption — %s: %s", r.ID, r.NoNegative)
		}
	}
	t.Logf("Obsidian (%s): %d rules, %d cases", ObsidianVersion, len(obsidianRules), len(casesObsidian))
}

// TestObsidianInvariance: CommonMark cases, and GFM cases with GFM on, that the Obsidian extension
// does not recognize must render the same with it on, since AutoDoc parses with both.
func TestObsidianInvariance(t *testing.T) {
	obs := markdown.ObsidianExtension()
	ran := 0
	for _, c := range allCases() {
		if obs.Recognizes([]byte(c.In)) {
			continue
		}
		ran++
		if got := renderWithOpts(t, c.In, markdown.Obsidian()); got != c.Want {
			t.Errorf("%s: Obsidian changed a document it does not recognize\n in: %q\ngot: %q\nwant %q", c.Rule, c.In, got, c.Want)
		}
	}
	for _, c := range casesGFM {
		if obs.Recognizes([]byte(c.In)) {
			continue
		}
		ran++
		if got := renderWithOpts(t, c.In, markdown.GFM(), markdown.Obsidian()); got != c.Want {
			t.Errorf("%s: Obsidian changed a GFM document it does not recognize\n in: %q\ngot: %q\nwant %q", c.Rule, c.In, got, c.Want)
		}
	}
	t.Logf("%d CommonMark and GFM cases unchanged with Obsidian on", ran)
}

// TestObsidianCollisions pins valid CommonMark that the Obsidian syntax reads differently, both ways,
// and one interaction with GFM: a wikilink's '|' escaped inside a table cell.
func TestObsidianCollisions(t *testing.T) {
	for _, c := range []struct{ in, commonmark, obsidian string }{
		{"---\na: 1\n---\nb\n", "<hr />\n<h2>a: 1</h2>\n<p>b</p>\n", "<p>b</p>\n"},
		{"[[a]]\n\n[a]: /u\n", "<p>[<a href=\"/u\">a</a>]</p>\n", "<p><a class=\"wikilink\" href=\"a\">a</a></p>\n"},
		{"#tag\n", "<p>#tag</p>\n", "<p><a class=\"tag\" href=\"#tag\">#tag</a></p>\n"},
		{"> [!note] T\n> b\n", "<blockquote>\n<p>[!note] T\nb</p>\n</blockquote>\n", "<div class=\"callout\" data-callout=\"note\">\n<div class=\"callout-title\">T</div>\n<div class=\"callout-content\">\n<p>b</p>\n</div>\n</div>\n"},
	} {
		if !markdown.ObsidianExtension().Recognizes([]byte(c.in)) {
			t.Errorf("Recognizes misses %q, which Obsidian reads differently", c.in)
		}
		if got := render(t, c.in); got != c.commonmark {
			t.Errorf("CommonMark %q = %q, want %q", c.in, got, c.commonmark)
		}
		if got := renderWithOpts(t, c.in, markdown.Obsidian()); got != c.obsidian {
			t.Errorf("Obsidian %q = %q, want %q", c.in, got, c.obsidian)
		}
	}
	in := "| a |\n| - |\n| [[P\\|al]] |\n"
	want := "<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td><a class=\"wikilink\" href=\"P\">al</a></td>\n</tr>\n</tbody>\n</table>\n"
	if got := renderWithOpts(t, in, markdown.GFM(), markdown.Obsidian()); got != want {
		t.Errorf("GFM+Obsidian %q = %q, want %q", in, got, want)
	}
}

// TestObsidianRecognizesEveryCase is the converse of the invariance run.
func TestObsidianRecognizesEveryCase(t *testing.T) {
	for _, c := range casesObsidian {
		if render(t, c.In) != renderWithOpts(t, c.In, markdown.Obsidian()) && !markdown.ObsidianExtension().Recognizes([]byte(c.In)) {
			t.Errorf("%s: %q reads differently under Obsidian but is not recognized", c.Rule, c.In)
		}
	}
}

// TestObsidianTree checks what the renderer does not show: the frontmatter's raw bytes and span,
// a wikilink's Target, a tag's name, and a callout's type and fold.
func TestObsidianTree(t *testing.T) {
	src := []byte("---\r\ntitle: x\r\n---\r\n> [!Warn]- T\n> [[Page#H|al]] #a/b\n")
	d := markdown.Parse(src, markdown.Obsidian())
	fm := d.Root.FirstChild
	if fm.Kind != markdown.KindFrontmatter || string(fm.Literal) != "title: x\r\n" || string(src[fm.Span.Start:fm.Span.End]) != "---\r\ntitle: x\r\n---" {
		t.Fatalf("frontmatter %s %q spans %q", fm.Kind, fm.Literal, src[fm.Span.Start:fm.Span.End])
	}
	bq := fm.Next
	if bq.Callout == nil || string(bq.Callout.Type) != "Warn" || bq.Callout.Fold != '-' {
		t.Fatalf("callout %+v", bq.Callout)
	}
	para := bq.FirstChild.Next
	wl := para.FirstChild
	if wl.Kind != markdown.KindWikilink || string(wl.Target.Page) != "Page" || string(wl.Target.Heading) != "H" ||
		string(wl.Target.Alias) != "al" || string(src[wl.Span.Start:wl.Span.End]) != "[[Page#H|al]]" {
		t.Fatalf("wikilink %s %+v spans %q", wl.Kind, wl.Target, src[wl.Span.Start:wl.Span.End])
	}
	tag := para.LastChild
	if tag.Kind != markdown.KindTag || string(tag.Label) != "a/b" || string(src[tag.Span.Start:tag.Span.End]) != "#a/b" {
		t.Fatalf("tag %s %q spans %q", tag.Kind, tag.Label, src[tag.Span.Start:tag.Span.End])
	}
}
