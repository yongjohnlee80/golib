package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// texts are a document's blocks as "kind|marker|text", deco spans in brackets.
func texts(doc []hblock) []string {
	var out []string
	for _, b := range doc {
		var sb strings.Builder
		for _, sp := range b.spans {
			if sp.deco {
				sb.WriteString("[" + sp.text + "]")
				continue
			}
			sb.WriteString(sp.text)
		}
		kinds := map[hblockKind]string{hPara: "p", hHeading: "h", hPre: "pre", hRule: "hr", hRow: "tr"}
		out = append(out, kinds[b.kind]+"|"+b.marker+"|"+sb.String())
	}
	return out
}

func eqStrings(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestHTMLDocBlocks: a page reads into blocks as a reader sees it: white space collapsed (and kept
// out of a link's or code's edges), scripts, styles and hidden elements dropped, lists marked by
// depth and number, a table's cells split, a hard break kept, an image as its alt text.
func TestHTMLDocBlocks(t *testing.T) {
	doc := readHTMLDoc([]byte(`<!doctype html><html><head><title>t</title><style>p{color:red}</style></head><body>
<h2>A  heading</h2>
<p>Some <em>em</em> and <a href="x.md">a link</a> and <code>code</code>.<br>next   line</p>
<script>alert(1)</script><p hidden>gone</p><div aria-hidden="true">gone too</div>
<ul><li>one</li><li>two<ul><li>deep</li></ul></li></ul>
<ol start="3"><li>c</li><li>d</li></ol>
<table><tr><th>H1</th><th>H2</th></tr><tr><td>a <p>b</p></td><td>c</td></tr></table>
<pre>  keep
   this</pre>
<hr>
<blockquote><p>quoted</p></blockquote>
<p><img src="a.png" alt="a picture"> after</p>
</body></html>`))
	eqStrings(t, texts(doc), []string{
		"h||A heading",
		"p||Some em and a link and code.\nnext line",
		"p|•|one",
		"p|•|two",
		"p|◦|deep",
		"p|3.|c",
		"p|4.|d",
		"tr||H1[ │ ]H2",
		"tr||a b[ │ ]c",
		"pre||  keep\n   this",
		"hr||",
		"p||quoted",
		"p||[a picture] after",
	})
	if l := doc[1].spans[3]; l.text != "a link" || l.link != "x.md" {
		t.Errorf("the link span = %+v, want \"a link\" to x.md with no edge spaces", l)
	}
	if doc[11].quote != 1 {
		t.Errorf("the quote's depth = %d, want 1", doc[11].quote)
	}
	if !doc[2].gap || doc[3].gap || doc[4].gap || !doc[5].gap || doc[6].gap {
		t.Errorf("a list starts after a blank row and its items follow without one: gaps %v %v %v %v %v",
			doc[2].gap, doc[3].gap, doc[4].gap, doc[5].gap, doc[6].gap)
	}
}

// TestHTMLDocSourceSpans: a block takes the innermost data-src that encloses it.
func TestHTMLDocSourceSpans(t *testing.T) {
	doc := readHTMLDoc([]byte(`<p data-src="0-10">one</p><div data-src="11-40"><p>two</p><p data-src="20-30">three</p></div><p>four</p><p data-src="x">five</p>`))
	want := [][2]int{{0, 10}, {11, 40}, {20, 30}, {-1, -1}, {-1, -1}}
	for i, b := range doc {
		if b.src != want[i] {
			t.Errorf("block %d (%q) src = %v, want %v", i, b.spans[0].text, b.src, want[i])
		}
	}
}

// TestHTMLDocRobust: what a page should not do never breaks the reader: unknown elements, raw
// script text, broken markup, and nesting far past the parser's depth (flattened).
func TestHTMLDocRobust(t *testing.T) {
	deep := strings.Repeat("<div>", 10000) + "deep text" + strings.Repeat("</div>", 10000)
	for name, src := range map[string]string{
		"unknown": `<foo><bar baz=1>text</bar></foo>`,
		"script":  `<p>a</p><script>if (a < b) { document.write("<p>x</p>") }</script><p>b</p>`,
		"broken":  `<p><b>bold <i>both</b> italic</i><td>cell</tr></table><li>`,
		"deep":    deep,
		"empty":   ``,
	} {
		doc := readHTMLDoc([]byte(src))
		c := newCellHTML(func(s string) int { return len([]rune(s)) })
		c.SetSource([]byte(src))
		c.SetWidth(20)
		got := strings.Join(texts(doc), " ")
		if name == "script" && strings.Contains(got, "document.write") {
			t.Errorf("a script's text was read: %q", got)
		}
		if name == "deep" && !strings.Contains(got, "deep text") {
			t.Errorf("deeply nested text was lost: %q", got)
		}
	}
}

func layCells(t *testing.T, html string, width int) *cellHTML {
	t.Helper()
	c := newCellHTML(func(s string) int { return len([]rune(s)) })
	c.SetSource([]byte(html))
	c.SetWidth(float32(width))
	return c
}

// rows are the layout's rows as text, padded clusters and all, trailing spaces trimmed.
func (c *cellHTML) rowTexts() []string {
	var out []string
	for _, r := range c.rows {
		line := []rune(strings.Repeat(" ", c.width))
		for _, k := range r.cls {
			for i, ch := range []rune(k.text) {
				if k.x+i < len(line) {
					line[k.x+i] = ch
				}
			}
		}
		out = append(out, strings.TrimRight(string(line), " "))
	}
	return out
}

// TestCellHTMLRows: the cell layout wraps at the width, hangs list markers and wrapped lines
// under the text, bars quotes, lines a fitting table's columns up and rules a rule.
func TestCellHTMLRows(t *testing.T) {
	c := layCells(t, `<h1>Title</h1><p>one two three four five</p><ul><li>a long item here</li></ul>`+
		`<table><tr><td>k</td><td>value</td></tr><tr><td>longer</td><td>v</td></tr></table>`+
		`<blockquote><p>quoted text</p></blockquote><hr>`, 14)
	eqStrings(t, c.rowTexts(), []string{
		"Title",
		"",
		"one two three",
		"four five",
		"",
		" • a long", // the wrap breaks before a word that fills the row to its last cell
		"   item here",
		"",
		"k      │ value",
		"longer │ v",
		"",
		"▎ quoted text",
		"",
		"──────────────",
	})
}

// TestCellHTMLPositions: every boundary of a wrapped paragraph maps a point to a position and the
// selection's rects and text back; a gap row answers the next block's start; past the end is the
// end.
func TestCellHTMLPositions(t *testing.T) {
	c := layCells(t, `<p>one two three</p><p>four</p>`, 8)
	// rows: "one two" / "three" / "" / "four"
	if p := c.At(0, 0); p != (DocPos{0, 0, 0}) {
		t.Errorf("At(0,0) = %v", p)
	}
	if p := c.At(2.6, 0); p != (DocPos{0, 0, 3}) {
		t.Errorf("At on the right half of 'e' = %v, want after it", p)
	}
	if p := c.At(1, 1); p != (DocPos{0, 0, 9}) {
		t.Errorf("At(1,1) on 'h' of three = %v, want offset 9", p)
	}
	if p := c.At(0, 2); p != (DocPos{1, 0, 0}) {
		t.Errorf("At on the gap row = %v, want the next block's start", p)
	}
	if p := c.At(0, 99); p != (DocPos{1, 0, 4}) {
		t.Errorf("At past the end = %v, want the document's end", p)
	}
	if got := c.Text(DocPos{0, 0, 4}, DocPos{1, 0, 2}); got != "two three\nfo" {
		t.Errorf("Text across a wrap and a block = %q", got)
	}
	rs := c.Rects(DocPos{0, 0, 4}, DocPos{0, 0, 11})
	if len(rs) != 2 || rs[0] != (Rect32{4, 0, 3, 1}) || rs[1] != (Rect32{0, 1, 3, 1}) {
		t.Errorf("Rects of \"two thr\" = %v", rs)
	}
}

// TestCellHTMLBlockTop: a source byte finds the block whose source holds it, else the first block
// after it; past every block, the last.
func TestCellHTMLBlockTop(t *testing.T) {
	c := layCells(t, `<p data-src="0-10">a</p><p>no source</p><p data-src="50-60">b</p>`, 20)
	// rows: "a" / "" / "no source" / "" / "b"
	for _, tc := range []struct {
		b    int
		want float32
		ok   bool
	}{{5, 0, true}, {20, 4, true}, {55, 4, true}, {60, 4, true}} { // past every block: the last
		got, ok := c.BlockTop(tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("BlockTop(%d) = %v, %v; want %v, %v", tc.b, got, ok, tc.want, tc.ok)
		}
	}
	if src, top := c.BlockAt(2); src != -1 || top != 2 {
		t.Errorf("BlockAt a block with no source = %d, %v; want -1, 2", src, top)
	}
}

// TestCellHTMLBytesAfterTheLastBlock: a source byte after every block is the last block's.
func TestCellHTMLBytesAfterTheLastBlock(t *testing.T) {
	c := layCells(t, `<p data-src="0-10">a</p><p data-src="12-20">b</p>`, 20)
	if top, ok := c.BlockTop(25); !ok || top != 2 {
		t.Errorf("BlockTop(25) = %v %v, want the last block's first row, 2", top, ok)
	}
}

// activating is a layout with a part a click acts on: the cell layout, counting Activate.
type activating struct {
	*cellHTML
	hits int
	act  bool
}

func (a *activating) Activate(x, y float32) bool { a.hits++; return a.act }

// TestHTMLViewAsksActivateBeforeALink: a click with no drag asks a layout that can act first; when
// it acts the link under the click is not followed, when it does not the link is; a drag asks
// nothing.
func TestHTMLViewAsksActivateBeforeALink(t *testing.T) {
	var links []string
	v := NewHTMLView(WithOnLink(func(href string) { links = append(links, href) }))
	v.SetHTML([]byte(`<p><a href="x">link</a> text</p>`))
	fake := &activating{cellHTML: v.cells, act: true}
	v.BindLayout(fake)
	fake.SetWidth(40)
	click := func(evs ...tui.MouseEvent) {
		for _, e := range evs {
			v.HandleEvent(e)
		}
	}
	press := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1}
	release := tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1}
	click(press, release)
	if fake.hits != 1 || len(links) != 0 {
		t.Fatalf("an acting layout: asked %d times, links %v; want asked once and no link", fake.hits, links)
	}
	fake.act = false
	click(press, release)
	if fake.hits != 2 || len(links) != 1 || links[0] != "x" {
		t.Errorf("a layout that did not act: asked %d times, links %v; want the link followed", fake.hits, links)
	}
	click(press, tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 4}, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 4})
	if fake.hits != 2 {
		t.Errorf("a drag asked the layout: %d", fake.hits)
	}
}
