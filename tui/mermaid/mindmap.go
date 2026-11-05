package mermaid

import (
	"context"
	"math"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

func init() { register(pm.Mindmap, layAs(LayMindmap), nil) }

// mindmapLevelSep is the gap between a parent and its children: a spine in the middle with room
// for an elbow each side.
const mindmapLevelSep = 6

// LayMindmap lays d out as a two-sided tree, as gui/mermaid's mind map: the root in the middle,
// its children split left and right, each level further out, and a parent joined to its children
// by elbows sharing one spine. Every box sits on whole cells, with at least one blank cell between
// any two. layout.ErrTooLarge comes back as it is, for the host's "too large" notice.
func LayMindmap(ctx context.Context, d *pm.MindmapDiagram, m Measurer) (*Grid, error) {
	g := newGrid()
	if len(d.Nodes) == 0 {
		return g, nil
	}
	in := layout.TreeInput{Dir: layout.LR, BothWays: true, NodeSep: 1, LevelSep: mindmapLevelSep}
	sizes := make([][2]int, len(d.Nodes))
	for i, n := range d.Nodes {
		w, h := boxSize(FormOf(n.Shape), n.Label, m)
		sizes[i] = [2]int{w, h}
		in.Nodes = append(in.Nodes, layout.Size{W: float32(w), H: float32(h)})
		in.Parent = append(in.Parent, n.Parent)
	}
	res, err := layout.Tree(ctx, in, layout.Limits{})
	if err != nil {
		return nil, err
	}
	for i, n := range d.Nodes {
		w, h := sizes[i][0], sizes[i][1]
		c := res.Nodes[i]
		x, y := int(math.Floor(float64(c.X)-float64(w)/2)), int(math.Floor(float64(c.Y)-float64(h)/2))
		g.box(Box{ID: n.ID, Label: n.Label, Form: FormOf(n.Shape), Ports: PortSides}, x, y, m)
	}
	for i, n := range d.Nodes {
		if n.Parent >= 0 {
			g.elbow(g.Boxes[n.Parent].Rect, g.Boxes[i].Rect)
		}
	}
	return g.finish(m)
}

// elbow lines a parent's side to a child's side: out of the parent's middle row, along a spine
// halfway across the gap between them, and into the child's middle row. A child to the right
// leaves the parent's right side; one to the left, its left. Children of one parent on one side
// share the spine, so their elbows meet in ┬ ├ ┴ junctions.
func (g *Grid) elbow(parent, child tui.Rect) {
	py, cy := parent.Y+parent.H/2, child.Y+child.H/2
	if child.X >= parent.X+parent.W { // right of the parent
		from, to := parent.X+parent.W, child.X-1
		spine := from + (to-from)/2
		g.run(cell{from, py}, cell{spine, py})
		g.run(cell{spine, py}, cell{spine, cy})
		g.run(cell{spine, cy}, cell{to, cy})
		g.lines[cell{from, py}] |= west // into the parent's side
		g.lines[cell{to, cy}] |= east   // into the child's side
		return
	}
	from, to := parent.X-1, child.X+child.W
	spine := to + (from-to)/2
	g.run(cell{from, py}, cell{spine, py})
	g.run(cell{spine, py}, cell{spine, cy})
	g.run(cell{spine, cy}, cell{to, cy})
	g.lines[cell{from, py}] |= east
	g.lines[cell{to, cy}] |= west
}
