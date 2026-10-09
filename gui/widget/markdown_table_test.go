package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// layTableAt lays the doc's first block at a given width, off the cursor.
func layTableAt(t *testing.T, doc string, width float32) BlockLayout {
	t.Helper()
	lines := strings.Split(doc, "\n")
	th := Theme{Text: color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, Background: color.NRGBA{A: 0xff},
		Muted:          color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
		Prose:          gui.Font{Size: 16},
		Mono:           gui.Font{Family: gui.MonospaceFamily(), Size: 14},
		CodeBackground: color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}}
	r := NewMarkdownRenderer()
	sh := gui.NewRecordingCanvas(gui.Size{W: 800, H: 400}, gui.Size{W: 8, H: 16}).Text()
	for _, b := range r.Blocks(lines, 0, len(lines)) {
		return r.LayOut(b, lines, width, false, sh, th, nil)
	}
	t.Fatal("no blocks")
	return BlockLayout{}
}

// A table wider than the page shares the page's width: no fragment of any line
// is drawn past the page, and a wide column wraps its cells' text rather than
// overflowing.
func TestAWideTableSharesThePageWidth(t *testing.T) {
	doc := "| one | two |\n|---|---|\n| " + strings.Repeat("wide ", 12) + " | short |\n| a | b |"
	const width = 200
	bl := layTableAt(t, doc, width)
	if bl.Height == 0 {
		t.Fatal("the table laid out to nothing")
	}
	for _, ll := range bl.Lines {
		for _, l := range ll.Para.Lines {
			for _, f := range l.Frags {
				if f.X+f.W > width+1 {
					t.Errorf("a frag runs to %v, past the page's %v (%q)", f.X+f.W, width, ll.Spans[f.Span].Text)
				}
			}
		}
	}
	// the wide cell's text is laid as more than one wrapped line
	var wraps int
	for _, ll := range bl.Lines {
		if ll.Unnumbered && len(ll.Spans) > 0 && strings.Contains(ll.Spans[0].Text, "wide") {
			wraps++
		}
	}
	if wraps < 2 {
		t.Errorf("the wide cell wrapped into %d continuation lines, want at least 2", wraps)
	}
}

// A table that fits is unchanged: three rows, nothing wrapped, every column at
// its natural width.
func TestAFittingTableKeepsItsColumns(t *testing.T) {
	doc := "| one | two |\n|---|---|\n| a | b |"
	bl := layTableAt(t, doc, 800)
	if len(bl.Lines) != 3 {
		t.Fatalf("a fitting table laid %d lines, want 3", len(bl.Lines))
	}
	for _, ll := range bl.Lines {
		for _, s := range ll.Spans {
			if strings.Contains(s.Text, "wide") {
				t.Error("a fitting table wrapped a line")
			}
		}
	}
}
