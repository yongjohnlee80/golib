package mermaid

import (
	"context"
	"errors"
	"image/color"
	"reflect"
	"slices"
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

// A node the theme tints takes its own colour, root included, in place of its branch's; a node it
// does not tint keeps its branch's, and a theme with no Tint lays out as before.
func TestAMindmapsNodesTakeTheirOwnTint(t *testing.T) {
	src := "mindmap\n  r\n    a[sq]\n      a1(ro)\n    b((ci))\n      b1))ba(("
	m, release := gui.AcquireMeasurer(1)
	defer release()
	plain, err := Lay(context.Background(), src, 4000, testTheme, m)
	if err != nil {
		t.Fatal(err)
	}
	red := color.NRGBA{R: 0xff, A: 0xff}
	th := testTheme
	th.Tint = func(id string) (color.NRGBA, bool) { return red, id == "r" || id == "a1" }
	tinted, err := Lay(context.Background(), src, 4000, th, m)
	if err != nil {
		t.Fatal(err)
	}
	want := func() (color.NRGBA, color.NRGBA) { return mix(th.NodeFill, red, 0.35), mix(th.NodeStroke, red, 0.75) }
	for _, i := range []int{0, 2} { // r, a1
		if f, s := want(); tinted.nodes[i].fill != f || tinted.nodes[i].stroke != s {
			t.Errorf("node %d, tinted: fill %v stroke %v; want %v %v", i, tinted.nodes[i].fill, tinted.nodes[i].stroke, f, s)
		}
	}
	if !tinted.nodes[0].label.font.Bold {
		t.Error("the tinted root lost its bold")
	}
	for _, i := range []int{1, 3, 4} { // a, b, b1: their branches' tints
		if tinted.nodes[i].fill != plain.nodes[i].fill || tinted.nodes[i].stroke != plain.nodes[i].stroke {
			t.Errorf("node %d, not tinted: %v; want its branch's %v", i, tinted.nodes[i].fill, plain.nodes[i].fill)
		}
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

// A legend's outline is the map's: PaintMindShape draws each shape as a laid-out node of it is
// drawn, and a shape it does not know as the default's rounded box.
func TestPaintMindShapeIsTheMapsOutline(t *testing.T) {
	box := gui.Rect{X: 2, Y: 3, W: 40, H: 16}
	fill, stroke := color.NRGBA{R: 1, A: 0xff}, color.NRGBA{G: 2, A: 0xff}
	ops := func(s pm.MindShape) []gui.PaintCall {
		rc := gui.NewRecordingCanvas(gui.Size{W: 64, H: 32}, gui.Size{W: 8, H: 16})
		PaintMindShape(rc, s, box, fill, stroke, 1.2, 14)
		return rc.Calls
	}
	kinds := func(cs []gui.PaintCall) (out []string) {
		for _, c := range cs {
			out = append(out, c.Op)
		}
		return out
	}
	for _, c := range []struct {
		s      pm.MindShape
		want   []string
		radius float32 // a rounded box's corner; -1 for none
	}{
		{pm.MindSquare, []string{"FillRRect", "StrokeRRect"}, 0},
		{pm.MindDefault, []string{"FillRRect", "StrokeRRect"}, 4}, // min(H/4, 8)
		{pm.MindRounded, []string{"FillRRect", "StrokeRRect"}, 8}, // a stadium: H/2
		{pm.MindCloud, []string{"FillRRect", "StrokeRRect"}, 8},
		{pm.MindCircle, []string{"FillEllipse", "StrokePath"}, -1},
		{pm.MindBang, []string{"FillEllipse", "StrokePath", "StrokePath"}, -1},
		{pm.MindHexagon, []string{"FillPath", "StrokePath"}, -1},
		{pm.MindShape(200), []string{"FillRRect", "StrokeRRect"}, 4},
	} {
		got := ops(c.s)
		if k := kinds(got); !slices.Equal(k, c.want) {
			t.Errorf("shape %v: %v; want %v", c.s, k, c.want)
			continue
		}
		if c.radius >= 0 && (got[0].Radius != c.radius || got[0].Rect != box) {
			t.Errorf("shape %v: a box %v radius %v; want %v radius %v", c.s, got[0].Rect, got[0].Radius, box, c.radius)
		}
	}
	// the very outline a laid-out node of a shape gets
	l := &Laid{th: Theme{Font: gui.Font{Size: 14}}}
	rc := gui.NewRecordingCanvas(gui.Size{W: 64, H: 32}, gui.Size{W: 8, H: 16})
	l.outline(rc, laidNode{form: formHexagon, box: box, fill: fill, stroke: stroke, width: 1.2})
	if got := ops(pm.MindHexagon); !reflect.DeepEqual(got, rc.Calls) {
		t.Error("PaintMindShape's hexagon is not a laid node's")
	}
}
