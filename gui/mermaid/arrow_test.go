package mermaid

import (
	"context"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// A block arrow is laid out in a window as an arrow polygon in the node's colours, its tip on the
// side it points to (both for x and y), its label inside; a mind map node with an icon is laid out
// with its text. In sepia and in dark.
func TestABlockArrowIsAnArrowInAWindow(t *testing.T) {
	m, release := gui.AcquireMeasurer(1)
	defer release()
	dark := testTheme
	dark.Background, dark.Text, dark.NodeFill, dark.NodeStroke = color.NRGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff},
		color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}, color.NRGBA{R: 0x30, G: 0x30, B: 0x38, A: 0xff}, color.NRGBA{R: 0x87, G: 0xaf, B: 0xd7, A: 0xff}
	want := map[string]form{"go": formArrowRight, "l": formArrowLeft, "x": formArrowX, "y": formArrowY, "dn": formArrowDown, "up": formArrowUp, "a": formRect}
	for _, th := range []struct {
		name string
		th   Theme
	}{{"sepia", testTheme}, {"dark", dark}} {
		l, err := Lay(context.Background(), "block-beta\n  columns 3\n  a go<[\"next\"]>(right) b\n  l<[\"back\"]>(left) x<[\"wide\"]>(x) y<[\"tall\"]>(y)\n  dn<[\"down\"]>(down) up<[\"up\"]>(up)\n", 4000, th.th, m)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range l.nodes {
			f, ok := want[n.id]
			if !ok {
				continue
			}
			if n.form != f || n.fill != th.th.NodeFill || n.stroke != th.th.NodeStroke {
				t.Errorf("%s %s: form %d, fill %v, stroke %v; want form %d in the node's colours", th.name, n.id, n.form, n.fill, n.stroke, f)
			}
			if f == formRect {
				continue
			}
			pts := arrowPoly(f, n.box)
			b := n.box
			tip := map[form][]gui.Point{formArrowRight: {gui.Pt(b.X+b.W, b.Y+b.H/2)}, formArrowLeft: {gui.Pt(b.X, b.Y+b.H/2)},
				formArrowDown: {gui.Pt(b.X+b.W/2, b.Y+b.H)}, formArrowUp: {gui.Pt(b.X+b.W/2, b.Y)},
				formArrowX: {gui.Pt(b.X+b.W, b.Y+b.H/2), gui.Pt(b.X, b.Y+b.H/2)}, formArrowY: {gui.Pt(b.X+b.W/2, b.Y+b.H), gui.Pt(b.X+b.W/2, b.Y)}}[f]
			for _, want := range tip {
				found := false
				for _, p := range pts {
					found = found || (p == want)
				}
				if !found {
					t.Errorf("%s %s: no tip at %v in %v", th.name, n.id, want, pts)
				}
			}
			for _, p := range pts {
				if p.X < b.X || p.X > b.X+b.W || p.Y < b.Y || p.Y > b.Y+b.H {
					t.Errorf("%s %s: the arrow leaves its box at %v", th.name, n.id, p)
				}
			}
		}
		if _, err := Lay(context.Background(), "mindmap\n  root((Plan))\n    ::icon(fa fa-book)\n    Read", 4000, th.th, m); err != nil {
			t.Errorf("%s: a mind map with an icon: %v; want it drawn", th.name, err)
		}
	}
}

// insidePoly reports whether p is inside the polygon pts (ray casting).
func insidePoly(p gui.Point, pts []gui.Point) bool {
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		a, b := pts[i], pts[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// A block arrow's label lies inside its arrow, not only its box: a long label on an arrow pointing
// up, down or both ways along a column, a label of three lines on one pointing right, left or both
// ways along a row, and a narrow label of four lines on one pointing down a column a wide block
// widens (its heads a third of its length, not half its breadth), each whole inside the polygon
// drawn. Laid out at its width and fitted into a narrow one.
func TestABlockArrowsLabelIsInsideItsArrow(t *testing.T) {
	src := "block-beta\n  columns 3\n" +
		"  w[\"a block wide enough to widen its column well past its arrow\"] space space\n" +
		"  up<[\"a long label on an arrow pointing up\"]>(up) dn<[\"a long label on an arrow pointing down\"]>(down) y<[\"a long label on an arrow both ways\"]>(y)\n" +
		"  r<[\"first line<br/>second line<br/>third\"]>(right) l<[\"first line<br/>second line<br/>third\"]>(left) x<[\"first line<br/>second line<br/>third\"]>(x)\n" +
		"  v<[\"a<br/>b<br/>c<br/>d\"]>(down) space space\n"
	for _, width := range []float32{4000, 300} {
		l, err := lay(t, src, width)
		if err != nil {
			t.Fatal(err)
		}
		if width == 300 && l.fit >= 1 {
			t.Fatalf("at 300 the diagram is not fitted: fit %v", l.fit)
		}
		arrows := 0
		for _, n := range l.nodes {
			if n.form < formArrowRight {
				continue
			}
			arrows++
			pts, lb := arrowPoly(n.form, n.box), n.label.box
			if len(n.label.lines) < 1 || lb.W <= 0 || lb.H <= 0 {
				t.Fatalf("%s: no label laid %+v", n.id, n.label)
			}
			for i := 0; i <= 6; i++ {
				for j := 0; j <= 6; j++ {
					p := gui.Pt(lb.X+lb.W*float32(i)/6, lb.Y+lb.H*float32(j)/6)
					if !insidePoly(p, pts) {
						t.Errorf("width %v, %s: the label %+v leaves its arrow %v at %v", width, n.id, lb, pts, p)
						i, j = 7, 7
					}
				}
			}
		}
		if arrows != 7 {
			t.Fatalf("width %v: %d arrows laid, want 7", width, arrows)
		}
	}
}
