package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// A block diagram lays out on its grid in the window as in cells: a row's blocks share a top, a
// column's share a left edge and width, a spanning block ends where its last column does, a group's
// frame holds its own blocks, and a link runs from its source's outline to its target's, its label
// at its middle.
func TestABlockDiagramIsLaidOut(t *testing.T) {
	l, err := lay(t, "block-beta\n  columns 3\n  a b:2\n  c:2 d\n  block:g:3\n    x y\n  end\n  a -- \"go\" --> d\n", 2000)
	if err != nil {
		t.Fatal(err)
	}
	box := map[string]gui.Rect{}
	for _, n := range l.nodes {
		box[n.id] = n.box
	}
	a, b, c, d, x, y := box["a"], box["b"], box["c"], box["d"], box["x"], box["y"]
	if a.Y != b.Y || c.Y != d.Y || c.Y <= a.Y+a.H {
		t.Errorf("rows: a %+v b %+v, c %+v d %+v", a, b, c, d)
	}
	if c.X != a.X || b.X+b.W != d.X+d.W || b.W <= d.W {
		t.Errorf("spans: a %+v b %+v c %+v d %+v", a, b, c, d)
	}
	if len(l.group) != 1 {
		t.Fatalf("%d groups; want 1", len(l.group))
	}
	f := l.group[0].box
	for id, r := range map[string]gui.Rect{"x": x, "y": y} {
		if r.X <= f.X || r.Y <= f.Y || r.X+r.W >= f.X+f.W || r.Y+r.H >= f.Y+f.H {
			t.Errorf("%s %+v outside its group %+v", id, r, f)
		}
	}
	if f.Y <= c.Y+c.H || f.X != a.X || f.X+f.W != d.X+d.W {
		t.Errorf("the group %+v isn't the third row, three columns wide", f)
	}
	if len(l.edges) != 1 {
		t.Fatalf("%d edges; want 1", len(l.edges))
	}
	e := l.edges[0]
	onOutline := func(p gui.Point, r gui.Rect) bool {
		near := func(v, w float32) bool { return v-w < 0.01 && w-v < 0.01 }
		in := p.X >= r.X-0.01 && p.X <= r.X+r.W+0.01 && p.Y >= r.Y-0.01 && p.Y <= r.Y+r.H+0.01
		return in && (near(p.X, r.X) || near(p.X, r.X+r.W) || near(p.Y, r.Y) || near(p.Y, r.Y+r.H))
	}
	if len(e.pts) != 2 || !onOutline(e.pts[0], a) || !onOutline(e.pts[1], d) {
		t.Errorf("the link %v doesn't run from a's outline %+v to d's %+v", e.pts, a, d)
	}
	if e.head != endArrow || e.label == nil || e.label.lines[0] != "go" {
		t.Fatalf("the link's head %v, label %+v", e.head, e.label)
	}
	mid := gui.Pt((e.pts[0].X+e.pts[1].X)/2, (e.pts[0].Y+e.pts[1].Y)/2)
	if lc := gui.Pt(e.label.box.X+e.label.box.W/2, e.label.box.Y+e.label.box.H/2); lc != mid {
		t.Errorf("the label's centre %v isn't the link's middle %v", lc, mid)
	}
}
