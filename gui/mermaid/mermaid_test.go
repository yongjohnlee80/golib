package mermaid

import (
	"context"
	"errors"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
)

var testTheme = Theme{
	Background:    color.NRGBA{R: 0xe6, G: 0xd9, B: 0xb9, A: 0xff},
	Text:          color.NRGBA{R: 0x5b, G: 0x46, B: 0x36, A: 0xff},
	Line:          color.NRGBA{R: 0x8a, G: 0x73, B: 0x50, A: 0xff},
	NodeFill:      color.NRGBA{R: 0xda, G: 0xcc, B: 0xa9, A: 0xff},
	NodeStroke:    color.NRGBA{R: 0x8a, G: 0x5a, B: 0x2b, A: 0xff},
	ClusterFill:   color.NRGBA{R: 0xe0, G: 0xd2, B: 0xb0, A: 0xff},
	ClusterStroke: color.NRGBA{R: 0xb5, G: 0xa3, B: 0x80, A: 0xff},
	Font:          gui.Font{Size: 14},
}

func lay(t *testing.T, src string, width float32) (*Laid, error) {
	t.Helper()
	m, release := gui.AcquireMeasurer(1)
	defer release()
	return Lay(context.Background(), src, width, testTheme, m)
}

// A flowchart lays out with no node over another, and paints its shapes, edges and every label.
func TestAFlowchartIsLaidOutAndPainted(t *testing.T) {
	l, err := lay(t, "flowchart LR\n  A[Start] --> B{Decide}\n  B -->|yes| C((Done))\n  B -.->|no| D[(Store)]\n  subgraph S [Later]\n    D\n  end", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 4 || len(l.edges) != 3 || len(l.group) != 1 {
		t.Fatalf("laid %d nodes, %d edges, %d groups; want 4, 3, 1", len(l.nodes), len(l.edges), len(l.group))
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if a.box.X < b.box.X+b.box.W && b.box.X < a.box.X+a.box.W && a.box.Y < b.box.Y+b.box.H && b.box.Y < a.box.Y+a.box.H {
				t.Errorf("%q's box %+v overlaps %q's %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	if s := l.Size(); s.W <= 0 || s.H <= 0 || l.fit != 1 {
		t.Errorf("size %+v fit %v", s, l.fit)
	}
	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 1000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts, paths := 0, 0
	for _, c := range rc.Calls {
		switch c.Op {
		case "DrawText":
			texts++
		case "FillPath", "StrokePath":
			paths++
		}
	}
	// four node labels, two edge labels, the group's title
	if texts != 7 {
		t.Errorf("%d texts drawn, want 7", texts)
	}
	if paths == 0 {
		t.Error("no paths drawn: no diamond, edge or arrowhead")
	}
}

// A diagram wider than the width asked is scaled down to it.
func TestAWideDiagramFitsItsWidth(t *testing.T) {
	l, err := lay(t, "flowchart LR\n  A --> B --> C --> D --> E --> F --> G", 200)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 200 || l.fit >= 1 {
		t.Errorf("size %+v fit %v: want 200 wide, scaled down", l.Size(), l.fit)
	}
}

// A type golib does not draw is ErrUnsupported, for the fallback; a malformed flowchart is a
// SyntaxError, shown as one.
func TestWhatIsNotDrawnSaysWhy(t *testing.T) {
	if _, err := lay(t, "gantt\n  title x", 400); !errors.Is(err, pm.ErrUnsupported) {
		t.Errorf("a gantt: %v, want ErrUnsupported", err)
	}
	if _, err := lay(t, "pie\n  \"a\": 1", 400); !errors.Is(err, pm.ErrUnsupported) {
		t.Errorf("a pie: %v, want ErrUnsupported", err)
	}
	var se *pm.SyntaxError
	if _, err := lay(t, "flowchart TB\n  A[unclosed --> B", 400); !errors.As(err, &se) {
		t.Errorf("a broken flowchart: %v, want a SyntaxError", err)
	}
}

// A style's fill, stroke and text colours reach the node.
func TestANodesStyleIsDrawn(t *testing.T) {
	l, err := lay(t, "flowchart TB\n  A --> B\n  style A fill:#f9f,stroke:#333,color:#fff", 400)
	if err != nil {
		t.Fatal(err)
	}
	a := l.nodes[0]
	if a.fill != (color.NRGBA{R: 0xff, G: 0x99, B: 0xff, A: 0xff}) || a.stroke != (color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff}) || a.label.color != (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("A styled as %v / %v / %v", a.fill, a.stroke, a.label.color)
	}
	if b := l.nodes[1]; b.fill != testTheme.NodeFill {
		t.Errorf("an unstyled node's fill %v, want the theme's", b.fill)
	}
}

// A style that fills a node and names no text colour gets the theme's text raised to read on that
// fill: a pale fill, written for a light page, in a dark theme whose text is light. A style's own
// color is the author's, kept as written; an unstyled node keeps the theme's text; and a theme
// whose text already reads on the fill keeps it as it is.
func TestALabelReadsOnTheFillItsStyleChose(t *testing.T) {
	dark := testTheme
	dark.Background, dark.Text = color.NRGBA{R: 0x1e, G: 0x23, B: 0x2b, A: 0xff}, color.NRGBA{R: 0xd8, G: 0xde, B: 0xe9, A: 0xff}
	dark.NodeFill, dark.NodeStroke = color.NRGBA{R: 0x2e, G: 0x34, B: 0x40, A: 0xff}, color.NRGBA{R: 0x81, G: 0xa1, B: 0xc1, A: 0xff}
	src := "flowchart LR\n  A[admin reset] --> B[gold-http]\n  B --> C[plain]\n  C --> D[own colour]\n  E[by class]:::ext --> A\n" +
		"  classDef ext fill:#e8f6f0,stroke:#3a9\n  class B ext\n  style A fill:#e8f6f0\n  style D fill:#e8f6f0,color:#eeeeee"
	ratio := func(fg, bg color.NRGBA) float64 {
		r, _ := style.ContrastRatio(style.RGB(fg.R, fg.G, fg.B), style.RGB(bg.R, bg.G, bg.B))
		return r
	}
	for i, th := range []Theme{dark, testTheme} {
		light := i == 1
		m, release := gui.AcquireMeasurer(1)
		l, err := Lay(context.Background(), src, 2000, th, m)
		release()
		if err != nil {
			t.Fatal(err)
		}
		by := map[string]laidNode{} // by label: the layer keeps no flowchart node's id
		for _, n := range l.nodes {
			if len(n.label.lines) > 0 {
				by[n.label.lines[0]] = n
			}
		}
		for _, id := range []string{"admin reset", "gold-http", "by class"} {
			n := by[id]
			if r := ratio(n.label.color, n.fill); r < labelContrast {
				t.Errorf("text %v on %s's fill %v reads at %.2f, want %g or more", th.Text, id, n.fill, r, labelContrast)
			}
			if light && n.label.color != th.Text {
				t.Errorf("the light theme's text, which reads on %s's fill, became %v", id, n.label.color)
			}
		}
		if c := by["plain"].label.color; c != th.Text {
			t.Errorf("an unstyled node's text %v, want the theme's %v", c, th.Text)
		}
		if c := by["own colour"].label.color; c != (color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}) {
			t.Errorf("a style's own color became %v, want #eeeeee as written", c)
		}
	}
}

// A translucent fill keeps the theme's text: the reader sees the page (or a subgraph) through it, not
// its RGB, so raising the text against that RGB would darken text that already reads (light text on
// a dark page, under a transparent white fill).
func TestATranslucentFillKeepsTheThemesText(t *testing.T) {
	dark := testTheme
	dark.Background, dark.Text = color.NRGBA{R: 0x1e, G: 0x23, B: 0x2b, A: 0xff}, color.NRGBA{R: 0xd8, G: 0xde, B: 0xe9, A: 0xff}
	if r, _ := style.ContrastRatio(style.RGB(dark.Text.R, dark.Text.G, dark.Text.B), style.RGB(dark.Background.R, dark.Background.G, dark.Background.B)); r < labelContrast {
		t.Fatalf("the control: the theme's text reads at %.2f on its page, want %g or more", r, labelContrast)
	}
	m, release := gui.AcquireMeasurer(1)
	l, err := Lay(context.Background(), "flowchart LR\n  A[clear] --> B[half]\n  style A fill:#ffffff00\n  style B fill:#ffffff80", 2000, dark, m)
	release()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range l.nodes {
		if n.fill.A == 0xff {
			t.Fatalf("%q's fill %v came out opaque: the test no longer has a translucent fill", n.label.lines[0], n.fill)
		}
		if n.label.color != dark.Text {
			t.Errorf("%q under a translucent fill %v: text %v, want the theme's %v", n.label.lines[0], n.fill, n.label.color, dark.Text)
		}
	}
}

// Laid at a scale, every label fits its node as a shaper at that scale measures it: the window
// draws at its own scale, so a diagram measured at another overflows its boxes.
func TestLabelsFitTheirNodesAtTheirScale(t *testing.T) {
	src := "flowchart LR\n  A[Interpolation of widths] --> B{Is it narrow enough?}\n  B --> C((Done here))"
	for _, scale := range []float32{1, 1.25, 2} {
		m, release := gui.AcquireMeasurer(scale)
		l, err := Lay(context.Background(), src, 2000, testTheme, m)
		release()
		if err != nil {
			t.Fatal(err)
		}
		sh := gui.NewTextShaper(scale)
		for _, n := range l.nodes {
			for _, line := range n.label.lines {
				if w := sh.Layout(line, n.label.font, 0).Width; w > n.label.box.W+0.01 || n.label.box.W > n.box.W {
					t.Errorf("at scale %g, %q is %v wide in a label %v wide, in a node %v wide", scale, line, w, n.label.box.W, n.box.W)
				}
			}
		}
	}
}

// A shaper asked for no scale, or a negative one, measures at 1 rather than dividing by zero.
func TestAShaperWithoutAScaleMeasuresAtOne(t *testing.T) {
	for _, scale := range []float32{0, -2} {
		sh := gui.NewTextShaper(scale)
		if sh.Scale() != 1 {
			t.Errorf("NewTextShaper(%g).Scale() = %g, want 1", scale, sh.Scale())
		}
		if w := sh.Layout("Start", testTheme.Font, 0).Width; w <= 0 || w != w {
			t.Errorf("NewTextShaper(%g) measured %q as %v wide", scale, "Start", w)
		}
	}
}

// A subgraph's title has room above its members, and a label on an edge inside one subgraph sits
// on its fill, not a patch of page.
func TestASubgraphsTitleAndLabelsSitInIt(t *testing.T) {
	l, err := lay(t, "flowchart TB\n  subgraph S [A long subgraph title]\n    A[First] -->|inside| B[Second]\n  end\n  B -->|leaving| C[Third]", 2000)
	if err != nil {
		t.Fatal(err)
	}
	g := l.group[0]
	for _, n := range l.nodes[:2] {
		if overlaps(g.title.box, n.box) {
			t.Errorf("the title %+v runs into %q at %+v", g.title.box, n.label.lines, n.box)
		}
	}
	in, out := l.edges[0], l.edges[1]
	if in.ground != testTheme.ClusterFill || out.ground != (color.NRGBA{}) {
		t.Errorf("label grounds: inside %v, leaving %v; want the cluster fill, then the page", in.ground, out.ground)
	}
}
