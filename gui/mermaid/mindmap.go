package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image/color"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Mindmap] = layMindmap }

// mindmapForm is each mind map shape's form. golib has no starburst or cloud outline: a bang takes
// the double circle (an emphasis, as the burst is) and a cloud the stadium (the softest outline).
var mindmapForm = [...]form{
	pm.MindDefault: formRound, pm.MindSquare: formRect, pm.MindRounded: formStadium,
	pm.MindCircle: formCircle, pm.MindBang: formDoubleCircle, pm.MindCloud: formStadium, pm.MindHexagon: formHexagon,
}

// layMindmap lays a mind map out as a tree growing both ways from its root, at the centre: the
// root's branches split right and left, each branch in a tint of its own.
func layMindmap(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	md := d.(*pm.MindmapDiagram)
	f := th.Font
	em := f.Size
	l := &Laid{th: th}
	in := layout.TreeInput{Dir: layout.LR, BothWays: true, NodeSep: em * 0.8, LevelSep: em * 3}
	branch := mindmapBranches(md)
	tints := mindmapTints(th)
	for i, n := range md.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nf := f
		fill, stroke := th.NodeFill, th.NodeStroke
		if n.Parent < 0 { // the root: larger, bold, in the strongest fill
			nf.Size, nf.Bold = em*1.15, true
			fill = mix(th.NodeFill, th.NodeStroke, 0.3)
		} else {
			tint := tints[branch[i]%len(tints)]
			fill, stroke = mix(th.NodeFill, tint, 0.22), mix(th.NodeStroke, tint, 0.35)
		}
		lb := newLabel(m, n.Label, nf, wrapAt, th.Text)
		fm := mindmapForm[n.Shape]
		w, h := mindmapSize(fm, lb.box.W, lb.box.H, em)
		in.Nodes = append(in.Nodes, layout.Size{W: w, H: h})
		in.Parent = append(in.Parent, n.Parent)
		l.nodes = append(l.nodes, laidNode{form: fm, box: gui.Rect{W: w, H: h}, fill: fill, stroke: stroke, width: 1.2, label: lb})
	}
	res, err := layout.Tree(ctx, in, layout.Limits{})
	if err != nil {
		if errors.Is(err, layout.ErrTooLarge) {
			return nil, fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
		}
		return nil, err
	}
	pad := em * 0.5
	for i := range l.nodes {
		n := &l.nodes[i]
		c := res.Nodes[i]
		n.box.X, n.box.Y = c.X-n.box.W/2+pad, c.Y-n.box.H/2+pad
		n.label = n.label.centredIn(n.box)
	}
	for i, pts := range res.Edges {
		if pts == nil {
			continue
		}
		a, b := gui.Pt(pts[0].X+pad, pts[0].Y+pad), gui.Pt(pts[1].X+pad, pts[1].Y+pad)
		mid := (a.X + b.X) / 2
		// an elbow, its corners rounded: out of the parent level, across, into the child level
		e := laidEdge{pts: []gui.Point{a, {X: mid, Y: a.Y}, {X: mid, Y: b.Y}, b}, line: pm.Solid, color: l.nodes[i].stroke}
		if md.Nodes[i].Parent == 0 {
			e.line = pm.Thick // a branch from the root
		}
		l.edges = append(l.edges, e)
	}
	l.size = gui.Size{W: res.Size.W + 2*pad, H: res.Size.H + 2*pad}
	return l, nil
}

// mindmapBranches is each node's branch: the index of the root's child it hangs from (the root's
// own is 0).
func mindmapBranches(md *pm.MindmapDiagram) []int {
	out := make([]int, len(md.Nodes))
	kids := 0
	for i, n := range md.Nodes {
		switch {
		case n.Parent < 0:
		case md.Nodes[n.Parent].Parent < 0: // a child of the root starts a branch
			out[i] = kids
			kids++
		default: // a parent comes before its children, so its branch is known
			out[i] = out[n.Parent]
		}
	}
	return out
}

// mindmapTints are the colours branches take in turn, the theme's own.
func mindmapTints(th Theme) []color.NRGBA {
	return []color.NRGBA{th.NodeStroke, th.Line, th.ClusterStroke, th.Text, mix(th.NodeStroke, th.Background, 0.4)}
}

// mindmapSize is a node's box for its label's size, by its form: room round the text, a circle as
// wide as it is tall.
func mindmapSize(fm form, w, h, em float32) (float32, float32) {
	w, h = w+em*1.6, h+em
	switch fm {
	case formStadium:
		w += h * 0.5
	case formHexagon:
		w += h * 0.5
	case formCircle, formDoubleCircle:
		d := max(w, h)
		w, h = d, d
		if fm == formDoubleCircle {
			w, h = w+em*0.8, h+em*0.8
		}
	}
	return w, h
}
