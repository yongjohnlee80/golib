package widget

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	mdhtml "github.com/yongjohnlee80/golib/parse/markdown/html"
)

// A cell painted with a style that sets no background shows the terminal's own, whatever the theme:
// dark patches behind the text on a sepia page in a dark terminal. So every cell the page paints
// carries a background: plain text, a heading, a link, a quote's bar, a list's marker, a table's
// rule, a horizontal rule, a code block's box, its language and its pan marks, and a diagram's lines,
// outlines, labels and pan marks. The page holds each of them, or the cell says which it didn't see.
func TestEveryCellThePagePaintsCarriesABackground(t *testing.T) {
	src := `# A heading

Plain text, **bold**, *italic*, ` + "`code`" + `, and a [link](docs.md).

> a quote

- a list item
- [ ] a task

| a | b |
|---|---|
| 1 | 2 |

---

` + "```go\nfunc main() {}\n```\n\n```go\nvar aVeryLongNameThatRunsPastTheRoomOfThePageAndPans = 1\nvar b = 2\nvar c = 3\n```\n"
	var html bytes.Buffer
	if err := mdhtml.Render(&html, markdown.Parse([]byte(src), markdown.WithGFM(), markdown.WithObsidian())); err != nil {
		t.Fatal(err)
	}
	// a mind map doesn't narrow: wider than the page, it pans, between its « and » marks
	html.WriteString(`<pre class="mermaid">mindmap
  root((the root))
    a left child with a long label
    a right child with a long label
    another child with a long label
    one more child with a long label
</pre>`)
	const width = 40
	c := layCells(t, html.String(), width)
	wide := 0
	for bi := range c.doc {
		if c.over(bi) > 0 {
			c.panBlock(bi, 2) // past its left edge and short of its right: both marks show
			wide++
		}
	}
	if wide != 2 {
		t.Fatalf("%d blocks pan at %d columns; want the code block and the mind map: their « » to check", wide, width)
	}
	s := newRecordSurface(width, len(c.rows))
	c.PaintCells(s, 0, width, DocPos{}, DocPos{})

	var page strings.Builder
	for y := range s.text {
		for x, cl := range s.text[y] {
			page.WriteString(cl)
			if cl == " " || cl == "" {
				continue
			}
			if _, ok := s.style[y][x].GetBackground(); !ok {
				t.Errorf("row %d column %d %q has no background: it would show the terminal's own\n  row: %s",
					y, x, cl, strings.Join(s.text[y], ""))
			}
		}
		page.WriteByte('\n')
	}
	for _, want := range []string{"A heading", "Plain text", "bold", "code", "link", "a quote", "a list item", "a task",
		"func main", "go", "LongNameThatRuns", "b = 2", "child with a long label", "╭", "│", "«", "»", "─"} {
		if !strings.Contains(page.String(), want) {
			t.Errorf("the page doesn't show %q, so the cell didn't check it:\n%s", want, page.String())
		}
	}
	if t.Failed() {
		t.Logf("the page:\n%s", page.String())
	}
}
