package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
)

var (
	lookHeading = color.NRGBA{R: 0x40, G: 0x80, B: 0xff, A: 0xff}
	lookStrong  = color.NRGBA{R: 0x40, G: 0xc0, B: 0x60, A: 0xff}
	lookCode    = color.NRGBA{R: 0xe0, G: 0xa0, B: 0x40, A: 0xff}
	lookMarker  = color.NRGBA{R: 0xc0, G: 0x60, B: 0xc0, A: 0xff}
	lookWiki    = color.NRGBA{R: 0x6b, G: 0x52, B: 0x00, A: 0xff}
	lookTag     = color.NRGBA{R: 0x2f, G: 0x5e, B: 0x6b, A: 0xff}
)

// lookLay lays line ln of doc out off the cursor, with a theme carrying the Raw view's colours.
func lookLay(t *testing.T, doc string, ln int) (BlockLayout, Theme) {
	t.Helper()
	lines := strings.Split(doc, "\n")
	th := Theme{Text: color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, Background: color.NRGBA{A: 0xff},
		Muted: color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}, Prose: gui.Font{Size: 16}, Mono: gui.Font{Family: gui.MonospaceFamily(), Size: 14},
		Heading: lookHeading, Strong: lookStrong, Code: lookCode, Marker: lookMarker, Wiki: lookWiki, Tag: lookTag,
		CodeBackground: color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}}
	r := NewMarkdownRenderer()
	sh := gui.NewRecordingCanvas(gui.Size{W: 400, H: 400}, gui.Size{W: 8, H: 16}).Text()
	for _, b := range r.Blocks(lines, 0, len(lines)) {
		if b.From <= ln && ln < b.To {
			return r.LayOut(b, lines, 400, false, sh, th, nil), th
		}
	}
	t.Fatalf("no block holds line %d", ln)
	return BlockLayout{}, th
}

func colorOfText(spans []flow.Span, text string) (color.NRGBA, bool) {
	for _, s := range spans {
		if s.Text == text && !s.Hidden {
			return s.Color, true
		}
	}
	return color.NRGBA{}, false
}

// The Rendered view colours Markdown as the Raw view does: a heading, strong and code in their
// syntax colours.
func TestRenderedTakesTheRawColours(t *testing.T) {
	bl, _ := lookLay(t, "# Title", 0)
	if c, ok := colorOfText(bl.Lines[0].Spans, "Title"); !ok || c != lookHeading {
		t.Errorf("heading %v, want %v", c, lookHeading)
	}
	bl, _ = lookLay(t, "a **b** `c`", 0)
	if c, _ := colorOfText(bl.Lines[0].Spans, "b"); c != lookStrong {
		t.Errorf("strong %v, want %v", c, lookStrong)
	}
	if c, _ := colorOfText(bl.Lines[0].Spans, "c"); c != lookCode {
		t.Errorf("code %v, want %v", c, lookCode)
	}
	if lh := bl.Lines[0].Para.Lines[0].H; lh < 16*proseLineHeight-0.5 {
		t.Errorf("a prose line %vpx tall at 16px, want the prose line height %v", lh, proseLineHeight)
	}
}

// A rule is hidden text over a one-pixel line across the width; "---" under text is not one.
func TestRuleIsALine(t *testing.T) {
	bl, _ := lookLay(t, "para\n\n---", 2)
	ll := bl.Lines[0]
	if len(ll.Marks) != 1 || ll.Marks[0].Rect.W != 400 || ll.Marks[0].Rect.H != 1 || !ll.Spans[0].Hidden {
		t.Errorf("rule laid out as %+v, marks %+v", ll.Spans, ll.Marks)
	}
	bl, _ = lookLay(t, "para\n---", 1)
	if len(bl.Lines[0].Marks) != 0 {
		t.Error("a setext underline drawn as a rule")
	}
}

// A quote's marks are hidden, its text set in by depth, a bar drawn per level.
func TestQuoteHasBars(t *testing.T) {
	bl, _ := lookLay(t, "> > deep **x**", 0)
	ll := bl.Lines[0]
	if !ll.Spans[0].Hidden || ll.Spans[0].Text != "> > " {
		t.Errorf("quote marks %+v, want \"> > \" hidden", ll.Spans[0])
	}
	if len(ll.Marks) != 2 {
		t.Errorf("%d bars, want 2", len(ll.Marks))
	}
	if x := ll.Para.Lines[0].Frags[0].X; x <= ll.Marks[1].Rect.X {
		t.Errorf("text at %v, not past the bars", x)
	}
	if c, _ := colorOfText(ll.Spans, "x"); c != lookStrong {
		t.Errorf("Markdown in a quote not drawn: %v", c)
	}
	for _, s := range ll.Spans[1:] {
		if s.Col < 4 {
			t.Errorf("span %q at column %d, inside the marks", s.Text, s.Col)
		}
	}
}

// A list item's wrapped lines start where its text does, past the bullet.
func TestListItemHangsAfterItsBullet(t *testing.T) {
	bl, _ := lookLay(t, "- "+strings.Repeat("word ", 40), 0)
	p := bl.Lines[0].Para
	if len(p.Lines) < 2 {
		t.Fatal("no wrap")
	}
	var text float32 = -1
	for _, f := range p.Lines[0].Frags {
		if bl.Lines[0].Spans[f.Span].Col >= 2 {
			text = f.X
			break
		}
	}
	if x := p.Lines[1].Frags[0].X; text < 0 || abs32(x-text) > 0.5 {
		t.Errorf("continuation at %v, text at %v", x, text)
	}
	if c, _ := colorOfText(bl.Lines[0].Spans, "•"); c != lookMarker {
		t.Errorf("bullet %v, want the marker colour", c)
	}
}

// Frontmatter is a quiet block, its "---" hidden off the cursor, and code to list editing.
func TestFrontmatterIsAQuietBlock(t *testing.T) {
	doc := "---\ntags: [- a]\n- x: y\n---\n# body"
	bl, _ := lookLay(t, doc, 1)
	if len(bl.Lines) != 4 || bl.Background.A == 0 {
		t.Fatalf("frontmatter laid out as %d lines, background %v", len(bl.Lines), bl.Background)
	}
	for _, i := range []int{0, 3} {
		if !bl.Lines[i].Spans[0].Hidden {
			t.Errorf("frontmatter fence %d shown", i)
		}
	}
	lines := strings.Split(doc, "\n")
	if !NewMarkdownRenderer().InCode(lines, 2) || NewMarkdownRenderer().InCode(lines, 4) {
		t.Error("InCode: frontmatter must be code, the body not")
	}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// Frontmatter still being written (no closing line) is frontmatter to the end, as Raw reads it:
// a quiet block, its last line shown, and code to list editing.
func TestUnclosedFrontmatterRunsToTheEnd(t *testing.T) {
	doc := "---\ntitle: x\n- item"
	bl, _ := lookLay(t, doc, 1)
	if len(bl.Lines) != 3 || bl.Background.A == 0 {
		t.Fatalf("unclosed frontmatter laid out as %d lines, background %v", len(bl.Lines), bl.Background)
	}
	if bl.Lines[2].Spans[0].Hidden {
		t.Error("an unclosed frontmatter's last line hidden as if it closed it")
	}
	if !NewMarkdownRenderer().InCode(strings.Split(doc, "\n"), 2) {
		t.Error("list editing allowed in unclosed frontmatter")
	}
	if end, closed := frontmatterEnd(strings.Split(doc, "\n")); end != 3 || closed {
		t.Errorf("frontmatterEnd = %d, %v; want 3, false", end, closed)
	}
}

// A table is a grid off the cursor: each cell's text starts at its column, aligned as the
// delimiter row says, the header bold, the pipes hidden with room, and borders drawn.
func TestTableIsAGrid(t *testing.T) {
	doc := "| Method | `path` | n |\n|---|:-:|--:|\n| GET | /a | 1 |\n| POST | /longer/path | 204 |\n|x||7|"
	bl, _ := lookLay(t, doc, 0)
	if len(bl.Lines) != 5 {
		t.Fatalf("table laid out as %d lines, want 5 (one block)", len(bl.Lines))
	}
	// the x where the span holding text starts, in row ln of the block
	xOf := func(row int, text string) float32 {
		ll := bl.Lines[row]
		for _, l := range ll.Para.Lines {
			for _, f := range l.Frags {
				if ll.Spans[f.Span].Text == text && !ll.Spans[f.Span].Hidden {
					return f.X
				}
			}
		}
		t.Fatalf("row %d has no %q: %+v", row, text, ll.Spans)
		return 0
	}
	// column 1 is left-aligned: GET, POST and x start together
	if a, b, c := xOf(2, "GET"), xOf(3, "POST"), xOf(4, "x"); abs32(a-b) > 0.5 || abs32(a-c) > 0.5 {
		t.Errorf("column 1 starts at %v, %v, %v", a, b, c)
	}
	// column 3 is right-aligned: 1 and 204 end together
	sh := gui.NewRecordingCanvas(gui.Size{W: 10, H: 10}, gui.Size{W: 8, H: 16}).Text()
	w := func(s string) float32 { m := sh.Measure(s, gui.Font{Size: 16}); return m.X[len(m.X)-1] }
	if e1, e2 := xOf(2, "1")+w("1"), xOf(3, "204")+w("204"); abs32(e1-e2) > 0.5 {
		t.Errorf("column 3 ends at %v and %v, want together (right-aligned)", e1, e2)
	}
	// column 2 is centred: /a's middle is /longer/path's
	if m1, m2 := xOf(2, "/a")+w("/a")/2, xOf(3, "/longer/path")+w("/longer/path")/2; abs32(m1-m2) > 0.5 {
		t.Errorf("column 2 centres at %v and %v", m1, m2)
	}
	for _, s := range bl.Lines[0].Spans {
		if !s.Hidden && !s.Font.Bold {
			t.Errorf("header %q not bold", s.Text)
		}
		if s.Hidden && strings.Contains(s.Text, "|") && s.Room <= 0 {
			t.Errorf("a pipe run %q hidden with no room", s.Text)
		}
	}
	if len(bl.Lines[2].Marks) < 4 || len(bl.Lines[1].Marks) == 0 {
		t.Errorf("borders: row %d marks, delimiter %d", len(bl.Lines[2].Marks), len(bl.Lines[1].Marks))
	}
	// every source cluster of a row keeps a span: the spans cover the line, in order
	for row, ll := range bl.Lines {
		line := strings.Split(doc, "\n")[row]
		col := 0
		for _, s := range ll.Spans {
			if s.Col != col {
				t.Errorf("row %d: span %q at column %d, want %d", row, s.Text, s.Col, col)
			}
			col += len([]rune(s.Text))
		}
		if col != len([]rune(line)) {
			t.Errorf("row %d: spans cover %d clusters of %d", row, col, len([]rune(line)))
		}
	}
	// the cursor in the table: its source, as written
	lines := strings.Split(doc, "\n")
	r := NewMarkdownRenderer()
	src := r.LayOut(Block{From: 0, To: 5}, lines, 400, true, sh, Theme{Prose: gui.Font{Size: 16}, Mono: gui.Font{Size: 14}}, nil)
	for _, ll := range src.Lines {
		for _, s := range ll.Spans {
			if s.Hidden {
				t.Errorf("the table under the cursor hides %q", s.Text)
			}
		}
	}
}

// Not a table: a pipe line without a delimiter row, or a delimiter with another cell count.
func TestNotATable(t *testing.T) {
	for _, doc := range []string{"a | b\nplain", "| a | b |\n|---|\n| c | d |", "a | b"} {
		if n := tableEnd(strings.Split(doc, "\n"), 0); n != 0 {
			t.Errorf("%q read as a table to line %d", doc, n)
		}
	}
	if got := tableCells(`| a \| b | c |`); len(got) != 2 {
		t.Errorf("an escaped pipe split a cell: %d cells", len(got))
	}
}

// The header's rule is one line: the delimiter row is a strip as thin as the rule, and the header
// draws no bottom border of its own over it.
func TestTableHeaderHasOneRule(t *testing.T) {
	bl, _ := lookLay(t, "| a | b |\n|---|---|\n| c | d |", 0)
	head, delim := bl.Lines[0], bl.Lines[1]
	for _, m := range head.Marks {
		if m.Rect.W > 1 && m.Rect.H <= 1 && m.Rect.Y > 0 {
			t.Errorf("the header draws a bottom border at %v as well as the rule", m.Rect.Y)
		}
	}
	if h := delim.Para.Height; h > 3 {
		t.Errorf("the delimiter row is %vpx tall: a gap between the header and its rule", h)
	}
	if delim.Y != head.Y+head.Para.Height {
		t.Errorf("the rule at %v, not right under the header (%v)", delim.Y, head.Y+head.Para.Height)
	}
}

// A table's body runs to a blank line or another block's start, a row with no pipe included
// (GFM 201-202); a heading with a pipe ends it.
func TestTableEndsAsGFMReadsIt(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want int
	}{
		{"| a | b |\n| - | - |\n| c |\nbar\n\nafter", 4},           // a short row, and a pipe-less one
		{"| a | b |\n| - | - |\n| c | d |\n# x | y\nmore", 3},      // a heading starts a block
		{"| a | b |\n| - | - |\n| c | d |\n> q | r", 3},            // so does a quote
		{"| a | b |\n| - | - |\n| c | d |\n- item | x", 3},         // and a list item
		{"| a | b |\n| - | - |\n| c | d |\n```\ncode | x\n```", 3}, // and a fence
	} {
		if got := tableEnd(strings.Split(tc.doc, "\n"), 0); got != tc.want {
			t.Errorf("%q: table to line %d, want %d", tc.doc, got, tc.want)
		}
	}
	if n := tableEnd([]string{"- a | b", "| - | - |"}, 0); n != 0 {
		t.Errorf("a list item read as a table's header (to line %d)", n)
	}
}

// A body row's extra cells are ignored (GFM 204): they neither widen the grid nor show, and the
// row's spans still cover every cluster.
func TestTableIgnoresExtraCells(t *testing.T) {
	doc := "| a | b |\n| - | - |\n| c | d | extra cell |"
	bl, _ := lookLay(t, doc, 0)
	row := bl.Lines[2]
	col := 0
	for _, s := range row.Spans {
		if s.Col != col {
			t.Errorf("span %q at column %d, want %d", s.Text, s.Col, col)
		}
		col += len([]rune(s.Text))
		if strings.Contains(s.Text, "extra") && !s.Hidden {
			t.Errorf("the extra cell is shown: %+v", s)
		}
	}
	if line := strings.Split(doc, "\n")[2]; col != len([]rune(line)) {
		t.Errorf("spans cover %d clusters of %d", col, len([]rune(line)))
	}
	var widest float32
	for _, m := range bl.Lines[0].Marks {
		widest = max(widest, m.Rect.X+m.Rect.W)
	}
	for _, m := range row.Marks {
		if m.Rect.X+m.Rect.W > widest+0.5 {
			t.Errorf("a mark at %v reaches past the header's grid (%v): the extra cell widened it", m.Rect.X+m.Rect.W, widest)
		}
	}
}

// A wikilink reads as a link: its brackets hidden, its target (or its alias, the target then
// hidden too) underlined in the Raw view's wikilink colour. A #tag wears the tag colour.
func TestWikilinksAndTags(t *testing.T) {
	bl, _ := lookLay(t, "see [[conventions/app.md]] and [[page|Alias]] #todo", 0)
	spans := bl.Lines[0].Spans
	shown := func(text string) (flow.Span, bool) {
		for _, s := range spans {
			if !s.Hidden && strings.Contains(s.Text, text) {
				return s, true
			}
		}
		return flow.Span{}, false
	}
	if s, ok := shown("conventions/app.md"); !ok || s.Color != lookWiki || !s.Underline {
		t.Errorf("the wikilink's target: %+v", s)
	}
	if s, ok := shown("Alias"); !ok || s.Color != lookWiki {
		t.Errorf("the alias: %+v", s)
	}
	for _, hide := range []string{"[[", "]]", "page|"} {
		if _, ok := shown(hide); ok {
			t.Errorf("%q is shown", hide)
		}
	}
	if s, ok := shown("todo"); !ok || s.Color != lookTag {
		t.Errorf("the tag: %+v", s)
	}
}
