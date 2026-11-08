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
