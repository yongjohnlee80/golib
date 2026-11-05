package mermaid

import (
	"context"
	"math"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Form is a box's outline. Mermaid's shapes have no exact cell equivalent; each maps to the
// closest corner set.
type Form uint8

const (
	FormText    Form = iota // MindDefault: no outline, the label alone
	FormSquare              // MindSquare: ┌ ┐ └ ┘
	FormRounded             // MindRounded, MindCircle, MindCloud: ╭ ╮ ╰ ╯
	FormHexagon             // MindHexagon: ╱ ╲ corners, the label between < >
	FormBang                // MindBang: heavy ┏ ┓ ┗ ┛
)

// FormOf is the outline a mind map's shape is drawn with.
func FormOf(s pm.MindShape) Form {
	switch s {
	case pm.MindSquare:
		return FormSquare
	case pm.MindRounded, pm.MindCircle, pm.MindCloud:
		return FormRounded
	case pm.MindHexagon:
		return FormHexagon
	case pm.MindBang:
		return FormBang
	}
	return FormText
}

// formSize is a box's size around a label lw cells wide: an outline adds a border and a space
// each side, and a row above and below.
func formSize(f Form, lw int) (w, h int) {
	if f == FormText {
		return max(lw, 1), 1
	}
	return lw + 4, 3
}

// mindmapLevelSep is the gap between a parent and its children: a spine in the middle with room
// for an elbow each side.
const mindmapLevelSep = 6

// Lay lays d out for painting: ErrUnsupported for a kind not in Kinds.
func Lay(ctx context.Context, d pm.Diagram, m Measurer) (*Grid, error) {
	switch d := d.(type) {
	case *pm.MindmapDiagram:
		return LayMindmap(ctx, d, m)
	case *pm.TimelineDiagram:
		return LayTimeline(ctx, d, m)
	case *pm.JourneyDiagram:
		return LayJourney(ctx, d, m)
	case *pm.SequenceDiagram:
		return LaySequence(ctx, d, m)
	case *pm.PieDiagram:
		return LayPie(ctx, d, m)
	}
	return nil, ErrUnsupported
}

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
		w, h := formSize(FormOf(n.Shape), m.StringWidth(n.Label))
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
		r := tui.Rect{X: int(math.Floor(float64(c.X) - float64(w)/2)), Y: int(math.Floor(float64(c.Y) - float64(h)/2)), W: w, H: h}
		g.Boxes = append(g.Boxes, Box{ID: n.ID, Label: n.Label, Rect: r, Form: FormOf(n.Shape)})
	}
	g.normalise()
	for i, n := range d.Nodes {
		if n.Parent >= 0 {
			g.elbow(g.Boxes[n.Parent].Rect, g.Boxes[i].Rect)
		}
	}
	g.measure()
	return g, nil
}

// normalise moves every box so the leftmost and topmost start at 0: flooring can leave a box at
// -1, and painting starts at the grid's origin.
func (g *Grid) normalise() {
	minX, minY := math.MaxInt, math.MaxInt
	for _, b := range g.Boxes {
		minX, minY = min(minX, b.Rect.X), min(minY, b.Rect.Y)
	}
	for i := range g.Boxes {
		g.Boxes[i].Rect.X -= minX
		g.Boxes[i].Rect.Y -= minY
	}
}

// measure sets Size to cover every box and line cell.
func (g *Grid) measure() {
	for _, b := range g.Boxes {
		g.Size.W, g.Size.H = max(g.Size.W, b.Rect.X+b.Rect.W), max(g.Size.H, b.Rect.Y+b.Rect.H)
	}
	for c := range g.lines {
		g.Size.W, g.Size.H = max(g.Size.W, c.x+1), max(g.Size.H, c.y+1)
	}
	for c := range g.marks {
		g.Size.W, g.Size.H = max(g.Size.W, c.x+1), max(g.Size.H, c.y+1)
	}
	for c := range g.fills {
		g.Size.W, g.Size.H = max(g.Size.W, c.x+1), max(g.Size.H, c.y+1)
	}
	for _, t := range g.texts {
		g.Size.W, g.Size.H = max(g.Size.W, t.x+t.w), max(g.Size.H, t.y+1)
	}
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

// corners are a form's ┌ ┐ └ ┘ and its edges' ─ │.
type corners struct{ tl, tr, bl, br, h, v, left, right string }

var formCorners = map[Form]corners{
	FormSquare:  {"┌", "┐", "└", "┘", "─", "│", "│", "│"},
	FormRounded: {"╭", "╮", "╰", "╯", "─", "│", "│", "│"},
	FormHexagon: {"╱", "╲", "╲", "╱", "─", "│", "<", ">"},
	FormBang:    {"┏", "┓", "┗", "┛", "━", "┃", "┃", "┃"},
}

// paintBox draws b's outline and label at the origin; a side its line enters takes a junction
// (┤ ├) where the outline is light.
func paintBox(s tui.Surface, b Box, x0, y0 int, lines map[cell]dirs, box, label style.Style) {
	r := tui.Rect{X: x0 + b.Rect.X, Y: y0 + b.Rect.Y, W: b.Rect.W, H: b.Rect.H}
	if b.Form == FormText {
		tui.DrawText(s, r.X, r.Y, b.Label, label)
		return
	}
	c := formCorners[b.Form]
	right, bottom, mid := r.X+r.W-1, r.Y+r.H-1, r.Y+r.H/2
	s.SetCell(r.X, r.Y, c.tl, box)
	s.SetCell(right, r.Y, c.tr, box)
	s.SetCell(r.X, bottom, c.bl, box)
	s.SetCell(right, bottom, c.br, box)
	for x := r.X + 1; x < right; x++ {
		s.SetCell(x, r.Y, c.h, box)
		s.SetCell(x, bottom, c.h, box)
	}
	for y := r.Y + 1; y < bottom; y++ {
		left, rightSide := c.left, c.right
		if y == mid && b.Form != FormBang && b.Form != FormHexagon {
			if lines[cell{b.Rect.X - 1, b.Rect.Y + b.Rect.H/2}]&east != 0 {
				left = "┤"
			}
			if lines[cell{b.Rect.X + b.Rect.W, b.Rect.Y + b.Rect.H/2}]&west != 0 {
				rightSide = "├"
			}
		}
		s.SetCell(r.X, y, left, box)
		s.SetCell(right, y, rightSide, box)
		s.Fill(tui.Rect{X: r.X + 1, Y: y, W: r.W - 2, H: 1}, " ", label)
	}
	if b.Stem && b.Form != FormBang && b.Form != FormHexagon {
		cx := b.Rect.X + b.Rect.W/2
		if lines[cell{cx, b.Rect.Y - 1}]&south != 0 {
			s.SetCell(x0+cx, r.Y, "┴", box)
		}
		if lines[cell{cx, b.Rect.Y + b.Rect.H}]&north != 0 {
			s.SetCell(x0+cx, bottom, "┬", box)
		}
	}
	tui.DrawText(s, r.X+2, mid, b.Label, label)
}
