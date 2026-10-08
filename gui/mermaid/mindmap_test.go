package mermaid

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const mindmapExample = `mindmap
  root((mindmap))
    Origins
      Long history
      Popularisation
        British popular psychology author Tony Buzan
    Research
      On effectiveness<br/>and features
      On Automatic creation
        Uses
            Creative techniques
            Strategic planning
            Argument mapping
    Tools
      Pen and paper
      Mermaid`

func mindmapOverlap(a, b gui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// A mind map grows both ways from its root: no node over another, branches on both sides, an
// edge to every node but the root, every label painted.
func TestAMindmapGrowsBothWaysFromItsRoot(t *testing.T) {
	l, err := lay(t, mindmapExample, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 15 || len(l.edges) != 14 {
		t.Fatalf("%d nodes, %d edges; want 15, 14", len(l.nodes), len(l.edges))
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if mindmapOverlap(a.box, b.box) {
				t.Errorf("%q %+v overlaps %q %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	root := l.nodes[0]
	if root.form != formCircle || !root.label.font.Bold {
		t.Errorf("the root: form %v, bold %v", root.form, root.label.font.Bold)
	}
	rootX := root.box.X + root.box.W/2
	left, right := 0, 0
	for _, n := range l.nodes[1:] {
		if n.box.X+n.box.W/2 < rootX {
			left++
		} else {
			right++
		}
	}
	if left == 0 || right == 0 {
		t.Errorf("%d nodes left of the root, %d right: want both sides", left, right)
	}
	rc := gui.NewRecordingCanvas(gui.Size{W: 4000, H: 2000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	if texts != 17 { // 15 nodes; two labels take two lines: a <br>, and one wrapped
		t.Errorf("%d texts drawn, want 17", texts)
	}
}

// Each shape takes its form; a branch's nodes share a tint.
func TestAMindmapsShapesAndBranches(t *testing.T) {
	l, err := lay(t, "mindmap\n  r\n    a[sq]\n      a1(ro)\n    b((ci))\n      b1))ba((\n      b2)cl(\n    c{{hx}}", 4000)
	if err != nil {
		t.Fatal(err)
	}
	want := []form{formRound, formRect, formStadium, formCircle, formDoubleCircle, formStadium, formHexagon}
	for i, f := range want {
		if l.nodes[i].form != f {
			t.Errorf("node %d %q: form %v, want %v", i, l.nodes[i].label.lines, l.nodes[i].form, f)
		}
	}
	if l.nodes[1].fill != l.nodes[2].fill || l.nodes[3].fill != l.nodes[4].fill || l.nodes[1].fill == l.nodes[3].fill {
		t.Error("a branch's nodes do not share their tint, or two branches share one")
	}
}

// An empty mind map lays out and paints; a wide one fits its width; an icon is not drawn.
func TestAMindmapEmptyNarrowAndDeclined(t *testing.T) {
	l, err := lay(t, "mindmap", 400)
	if err != nil {
		t.Fatal(err)
	}
	l.Paint(gui.NewRecordingCanvas(gui.Size{W: 400, H: 400}, gui.Size{W: 8, H: 16}))
	if l, err = lay(t, mindmapExample, 300); err != nil || l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("narrow: size %+v fit %v err %v", l.Size(), l.fit, err)
	}
	if _, err := lay(t, "mindmap\n  a\n    b\n    ::icon(fa fa-book)", 400); !errors.Is(err, pm.ErrUnsupported) {
		t.Errorf("an icon: %v, want ErrUnsupported", err)
	}
}
