package html_test

import (
	"bytes"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

// specCase proves one rule of the specification. Rule is the rule's id in rules_test.go; Pos says
// whether the construct forms (true) or a near miss must not form it (false). Cases are written
// from the rule's statement, not copied from the specification's examples.
type specCase struct {
	Rule string
	Pos  bool
	In   string
	Want string
}

func render(t *testing.T, in string) string {
	t.Helper()
	var b bytes.Buffer
	if err := html.Render(&b, markdown.Parse([]byte(in)), html.Unsafe()); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func runCases(t *testing.T, cases []specCase) {
	t.Helper()
	for _, c := range cases {
		if got := render(t, c.In); got != c.Want {
			t.Errorf("%s (positive=%v)\n in:   %q\n got:  %q\n want: %q", c.Rule, c.Pos, c.In, got, c.Want)
		}
	}
}

var casesPreliminaries = []specCase{
	{"2.1/line-endings", true, "a\r\nb\rc\n", "<p>a\nb\nc</p>\n"},
	{"2.2/tab-indents-code", true, "\tfoo\n", "<pre><code>foo\n</code></pre>\n"},
	{"2.2/tab-in-content-kept", true, "    a\tb\n", "<pre><code>a\tb\n</code></pre>\n"},
	{"2.2/partial-tab-becomes-spaces", true, ">\t\tfoo\n", "<blockquote>\n<pre><code>  foo\n</code></pre>\n</blockquote>\n"},
	{"2.3/nul-replaced", true, "a\x00b\n", "<p>a\uFFFDb</p>\n"},
	{"2.4/escape-punct", true, "\\*not em\\*\n", "<p>*not em*</p>\n"},
	{"2.4/escape-non-punct-literal", false, "\\a\n", "<p>\\a</p>\n"},
	{"2.4/escape-in-code-literal", false, "`\\*`\n", "<p><code>\\*</code></p>\n"},
	{"2.5/named-entity", true, "&copy; &amp;\n", "<p>© &amp;</p>\n"},
	{"2.5/decimal-entity", true, "&#35; &#0;\n", "<p># \uFFFD</p>\n"},
	{"2.5/hex-entity", true, "&#x22;\n", "<p>&quot;</p>\n"},
	{"2.5/unknown-entity-literal", false, "&nosuch; &copy\n", "<p>&amp;nosuch; &amp;copy</p>\n"},
}

var casesLeaf = []specCase{
	{"4.1/thematic-break", true, "***\n---\n___\n", "<hr />\n<hr />\n<hr />\n"},
	{"4.1/spaced-break", true, " - - -\n", "<hr />\n"},
	{"4.1/two-chars-not-break", false, "**\n", "<p>**</p>\n"},
	{"4.1/mixed-chars-not-break", false, "*-*\n", "<p><em>-</em></p>\n"},
	{"4.2/atx-levels", true, "# a\n###### f\n", "<h1>a</h1>\n<h6>f</h6>\n"},
	{"4.2/seven-hashes-not-heading", false, "####### x\n", "<p>####### x</p>\n"},
	{"4.2/needs-space", false, "#x\n", "<p>#x</p>\n"},
	{"4.2/closing-sequence", true, "## b ##\n", "<h2>b</h2>\n"},
	{"4.2/closing-needs-space", false, "# c#\n", "<h1>c#</h1>\n"},
	{"4.2/empty-heading", true, "#\n", "<h1></h1>\n"},
	{"4.3/setext-levels", true, "A\n===\nB\n---\n", "<h1>A</h1>\n<h2>B</h2>\n"},
	{"4.3/setext-no-internal-space", false, "A\n= =\n", "<p>A\n= =</p>\n"},
	{"4.3x4.1/setext-before-thematic-break", true, "Foo\n---\n", "<h2>Foo</h2>\n"},
	{"4.4/indented-code", true, "    a\n      b\n", "<pre><code>a\n  b\n</code></pre>\n"},
	{"4.4/cannot-interrupt-paragraph", false, "p\n    q\n", "<p>p\nq</p>\n"},
	{"4.4/trailing-blank-lines-dropped", true, "    a\n\n\n", "<pre><code>a\n</code></pre>\n"},
	{"4.5/backtick-fence", true, "```\n<\n```\n", "<pre><code>&lt;\n</code></pre>\n"},
	{"4.5/info-string", true, "~~~ go x\nf\n~~~\n", "<pre><code class=\"language-go\">f\n</code></pre>\n"},
	{"4.5/closer-shorter-not-close", false, "````\na\n```\n", "<pre><code>a\n```\n</code></pre>\n"},
	{"4.5/backtick-info-no-backtick", false, "``` a`b\n", "<p>``` a`b</p>\n"},
	{"4.5/unclosed-runs-to-end", true, "```\nx\n", "<pre><code>x\n</code></pre>\n"},
	{"4.6/type6-block", true, "<div>\n*x*\n</div>\n", "<div>\n*x*\n</div>\n"},
	{"4.6/type7-cannot-interrupt", false, "p\n<span>\n", "<p>p\n<span></p>\n"},
	{"4.6/type1-ends-on-close", true, "<pre>\na\n</pre>\n*b*\n", "<pre>\na\n</pre>\n<p><em>b</em></p>\n"},
	{"4.6/type2-comment", true, "<!-- c -->\n", "<!-- c -->\n"},
	{"4.7/ref-definition", true, "[a]: /u \"t\"\n\n[a]\n", "<p><a href=\"/u\" title=\"t\">a</a></p>\n"},
	{"4.7/label-case-folded", true, "[\u1E9E]\n\n[SS]: /u\n", "<p><a href=\"/u\">\u1E9E</a></p>\n"},
	{"4.7/first-definition-wins", true, "[a]: /1\n[a]: /2\n\n[a]\n", "<p><a href=\"/1\">a</a></p>\n"},
	{"4.7/no-definition-is-text", false, "[b]\n", "<p>[b]</p>\n"},
	{"4.7/title-junk-not-definition", false, "[a]: /u \"t\" x\n", "<p>[a]: /u &quot;t&quot; x</p>\n"},
	{"4.8/paragraph-lines", true, "a\n  b\n", "<p>a\nb</p>\n"},
	{"4.9/blank-lines-separate", true, "a\n\n\nb\n", "<p>a</p>\n<p>b</p>\n"},
}

var casesContainer = []specCase{
	{"5.1/block-quote", true, "> a\n> b\n", "<blockquote>\n<p>a\nb</p>\n</blockquote>\n"},
	{"5.1/lazy-paragraph-continuation", true, "> a\nb\n", "<blockquote>\n<p>a\nb</p>\n</blockquote>\n"},
	{"5.1/lazy-not-for-code", false, ">     a\n    b\n", "<blockquote>\n<pre><code>a\n</code></pre>\n</blockquote>\n<pre><code>b\n</code></pre>\n"},
	{"5.1/blank-ends-quote", true, "> a\n\n> b\n", "<blockquote>\n<p>a</p>\n</blockquote>\n<blockquote>\n<p>b</p>\n</blockquote>\n"},
	{"5.2/bullet-item", true, "- a\n- b\n", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n"},
	{"5.2/ordered-start", true, "3. a\n", "<ol start=\"3\">\n<li>a</li>\n</ol>\n"},
	{"5.2/content-indent", true, "-   a\n\n    b\n", "<ul>\n<li>\n<p>a</p>\n<p>b</p>\n</li>\n</ul>\n"},
	{"5.2/under-indented-leaves-item", false, "- a\n\n b\n", "<ul>\n<li>a</li>\n</ul>\n<p>b</p>\n"},
	{"5.2/ten-digits-not-marker", false, "1234567890. a\n", "<p>1234567890. a</p>\n"},
	{"5.2/empty-item-cannot-interrupt", false, "p\n-\n", "<h2>p</h2>\n"},
	{"5.2/ordered-must-start-at-1-to-interrupt", false, "p\n2. a\n", "<p>p\n2. a</p>\n"},
	{"5.2/item-code-five-spaces", true, "-     a\n", "<ul>\n<li>\n<pre><code>a\n</code></pre>\n</li>\n</ul>\n"},
	{"5.3/marker-change-new-list", true, "- a\n+ b\n", "<ul>\n<li>a</li>\n</ul>\n<ul>\n<li>b</li>\n</ul>\n"},
	{"5.3/loose-list", true, "- a\n\n- b\n", "<ul>\n<li>\n<p>a</p>\n</li>\n<li>\n<p>b</p>\n</li>\n</ul>\n"},
	{"5.3/tight-list", false, "- a\n- b\n\n", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n"},
	{"5.1x5.2/lazy-line-in-list-in-quote", true, "> - a\nb\n", "<blockquote>\n<ul>\n<li>a\nb</li>\n</ul>\n</blockquote>\n"},
}

var casesInline = []specCase{
	{"6.1/code-span", true, "`a`\n", "<p><code>a</code></p>\n"},
	{"6.1/strip-one-space", true, "`` ` ``\n", "<p><code>`</code></p>\n"},
	{"6.1/line-ending-to-space", true, "`a\nb`\n", "<p><code>a b</code></p>\n"},
	{"6.1/unmatched-literal", false, "``a`\n", "<p>``a`</p>\n"},
	{"6.2/em-strong", true, "*a* **b** _c_ __d__\n", "<p><em>a</em> <strong>b</strong> <em>c</em> <strong>d</strong></p>\n"},
	{"6.2/intraword-underscore", false, "a_b_c\n", "<p>a_b_c</p>\n"},
	{"6.2/intraword-star", true, "a*b*c\n", "<p>a<em>b</em>c</p>\n"},
	{"6.2/space-after-opener", false, "* a*\n", "<ul>\n<li>a*</li>\n</ul>\n"},
	{"6.2/rule-of-three", true, "*a**b*\n", "<p><em>a**b</em></p>\n"},
	{"6.2/nested", true, "***a***\n", "<p><em><strong>a</strong></em></p>\n"},
	{"6.3/inline-link", true, "[a](/u \"t\")\n", "<p><a href=\"/u\" title=\"t\">a</a></p>\n"},
	{"6.3/angle-dest", true, "[a](<b c>)\n", "<p><a href=\"b%20c\">a</a></p>\n"},
	{"6.3/empty-dest", true, "[a]()\n", "<p><a href=\"\">a</a></p>\n"},
	{"6.3/no-nested-links", false, "[a [b](/2)](/1)\n", "<p>[a <a href=\"/2\">b</a>](/1)</p>\n"},
	{"6.3/full-reference", true, "[x][r]\n\n[r]: /u\n", "<p><a href=\"/u\">x</a></p>\n"},
	{"6.3/collapsed-reference", true, "[r][]\n\n[r]: /u\n", "<p><a href=\"/u\">r</a></p>\n"},
	{"6.3/unmatched-full-reference", false, "[x][nope]\n\n[x]: /u\n", "<p>[x][nope]</p>\n"},
	{"6.3/code-span-in-link-text", true, "[a `]`](/u)\n", "<p><a href=\"/u\">a <code>]</code></a></p>\n"},
	{"6.3/code-span-beats-link", false, "[not `a](/u)`\n", "<p>[not <code>a](/u)</code></p>\n"},
	{"6.4/image", true, "![a *b*](/i.png \"t\")\n", "<p><img src=\"/i.png\" alt=\"a b\" title=\"t\" /></p>\n"},
	{"6.5/uri-autolink", true, "<http://x.y/z>\n", "<p><a href=\"http://x.y/z\">http://x.y/z</a></p>\n"},
	{"6.5/email-autolink", true, "<a@b.c>\n", "<p><a href=\"mailto:a@b.c\">a@b.c</a></p>\n"},
	{"6.5/space-not-autolink", false, "<http://a b>\n", "<p>&lt;http://a b&gt;</p>\n"},
	{"6.6/raw-inline-html", true, "a <b c=\"d\">e</b>\n", "<p>a <b c=\"d\">e</b></p>\n"},
	{"6.6/bad-tag-is-text", false, "a <1b>\n", "<p>a &lt;1b&gt;</p>\n"},
	{"6.7/hard-break-spaces", true, "a  \nb\n", "<p>a<br />\nb</p>\n"},
	{"6.7/hard-break-backslash", true, "a\\\nb\n", "<p>a<br />\nb</p>\n"},
	{"6.7/no-hard-break-at-end", false, "a  \n", "<p>a</p>\n"},
	{"6.8/soft-break", true, "a \nb\n", "<p>a\nb</p>\n"},
	{"6.9/text", true, "hello $.;'there\n", "<p>hello $.;'there</p>\n"},
}

func TestCommonMark(t *testing.T) {
	for name, cases := range map[string][]specCase{
		"preliminaries": casesPreliminaries,
		"leaf":          casesLeaf,
		"container":     casesContainer,
		"inline":        casesInline,
	} {
		t.Run(name, func(t *testing.T) { runCases(t, cases) })
	}
}
