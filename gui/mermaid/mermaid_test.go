package mermaid

import (
	"context"
	"errors"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
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
	if _, err := lay(t, "sequenceDiagram\n  A->>B: hi", 400); !errors.Is(err, pm.ErrUnsupported) {
		t.Errorf("a sequence diagram: %v, want ErrUnsupported", err)
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
