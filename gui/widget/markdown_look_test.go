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
)

// lookLay lays line ln of doc out off the cursor, with a theme carrying the Raw view's colours.
func lookLay(t *testing.T, doc string, ln int) (BlockLayout, Theme) {
	t.Helper()
	lines := strings.Split(doc, "\n")
	th := Theme{Text: color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, Background: color.NRGBA{A: 0xff},
		Muted: color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}, Prose: gui.Font{Size: 16}, Mono: gui.Font{Family: gui.MonospaceFamily(), Size: 14},
		Heading: lookHeading, Strong: lookStrong, Code: lookCode, Marker: lookMarker,
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
