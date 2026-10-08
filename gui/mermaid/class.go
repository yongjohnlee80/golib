package mermaid

import (
	"context"
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Class] = layClass }

// relEnd is each relation end's end; an association drawn dotted is a dependency, whose head is
// open.
func relEnd(e pm.RelEnd, line pm.LineKind) end {
	switch e {
	case pm.RelInheritance:
		return endTriangle
	case pm.RelComposition:
		return endDiamond
	case pm.RelAggregation:
		return endDiamondOpen
	case pm.RelAssociation:
		if line == pm.Dotted {
			return endOpen
		}
		return endArrow
	}
	return endNone
}

// classBox is a class's box before it is placed: its size, and its labels and dividers relative
// to its top left.
type classBox struct {
	size     gui.Size
	name     label
	heads    []label     // the annotations, above the name
	sections []classSect // the attributes, then the methods
}

// classSect is a compartment: the divider above it, and its members.
type classSect struct {
	y       float32
	members []label
}

// layClass lays a class diagram out in layers: each class a box of three compartments (its
// annotations and name, its attributes, its methods), each note a sheet joined to its class by a
// dotted line, and each namespace a box round its classes.
func layClass(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	cd := d.(*pm.ClassDiagram)
	f := th.Font
	small := f
	small.Size *= 0.85
	bold := f
	bold.Bold = true
	em := f.Size
	padX, padY := em*0.7, em*0.35
	l := &Laid{th: th}
	in := layout.Input{Dir: dirOf(cd.Dir), NodeSep: em * 3, RankSep: em * 3.5, GroupPad: em * 1.6}
	index := map[string]int{}
	boxes := make([]classBox, 0, len(cd.Classes))

	for i, k := range cd.Classes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index[k.ID] = i
		fill, stroke, text, sw := styled(k.Style, th)
		var b classBox
		w := em * 5
		y := padY
		for _, a := range k.Annotations {
			lb := newLabel(m, "«"+a+"»", small, wrapAt, text).at(0, y)
			b.heads = append(b.heads, lb)
			y += lb.box.H
			w = max(w, lb.box.W+2*padX)
		}
		b.name = newLabel(m, k.Label, bold, wrapAt, text).at(0, y)
		y += b.name.box.H + padY
		w = max(w, b.name.box.W+2*padX)
		for _, members := range [][]string{k.Attributes, k.Methods} {
			sect := classSect{y: y}
			y += padY
			for _, s := range members {
				lb := newLabel(m, s, f, 0, text).at(padX, y)
				lb.left = true
				sect.members = append(sect.members, lb)
				y += lb.box.H
				w = max(w, lb.box.W+2*padX)
			}
			y += padY
			b.sections = append(b.sections, sect)
		}
		b.size = gui.Size{W: w, H: y}
		boxes = append(boxes, b)
		in.Nodes = append(in.Nodes, layout.Size{W: b.size.W, H: b.size.H})
		l.nodes = append(l.nodes, laidNode{form: formRect, fill: fill, stroke: stroke, width: sw})
	}

	for _, r := range cd.Relations {
		le := layout.Edge{From: index[r.From], To: index[r.To], MinLen: 1}
		e := laidEdge{line: r.Line, tail: relEnd(r.FromEnd, r.Line), head: relEnd(r.ToEnd, r.Line), color: th.Line}
		if r.Label != "" {
			lb := newLabel(m, r.Label, small, wrapAt, th.Text)
			pad := small.Size * 0.3
			le.Label = layout.Size{W: lb.box.W + 2*pad, H: lb.box.H + 2*pad}
			e.label = &lb
		}
		if r.FromCard != "" {
			lb := newLabel(m, r.FromCard, small, 0, th.Text)
			e.tailLabel = &lb
		}
		if r.ToCard != "" {
			lb := newLabel(m, r.ToCard, small, 0, th.Text)
			e.headLabel = &lb
		}
		in.Edges = append(in.Edges, le)
		l.edges = append(l.edges, e)
	}

	// notes: a sheet each, after the classes, a dotted line to the class it is for
	for _, n := range cd.Notes {
		lb := newLabel(m, n.Text, small, wrapAt, th.Text)
		size := gui.Size{W: lb.box.W + 2*padX + em*0.6, H: lb.box.H + 2*padY}
		boxes = append(boxes, classBox{size: size, name: lb.at((size.W-lb.box.W)/2, padY)})
		in.Nodes = append(in.Nodes, layout.Size{W: size.W, H: size.H})
		l.nodes = append(l.nodes, laidNode{form: formNote, fill: mix(th.NodeFill, th.Background, 0.5), stroke: th.NodeStroke, width: 1})
		if i, ok := index[n.For]; ok {
			in.Edges = append(in.Edges, layout.Edge{From: len(in.Nodes) - 1, To: i, MinLen: 1})
			l.edges = append(l.edges, laidEdge{line: pm.Dotted, color: th.Line})
		}
	}

	for _, ns := range cd.Namespaces {
		var members []int
		for i, k := range cd.Classes {
			if k.Namespace == ns.Name {
				members = append(members, i)
			}
		}
		if len(members) == 0 {
			continue
		}
		in.Groups = append(in.Groups, members)
		lb := newLabel(m, ns.Name, small, wrapAt, th.Text)
		lb.left = true
		l.group = append(l.group, laidGroup{title: lb})
	}

	res, err := layout.Layered(ctx, in, layout.Limits{})
	if err != nil {
		if errors.Is(err, layout.ErrTooLarge) {
			return nil, fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
		}
		return nil, err
	}
	pad := em * 0.5
	for i := range l.nodes {
		n, b, c := &l.nodes[i], boxes[i], res.Nodes[i]
		x, y := c.X-b.size.W/2+pad, c.Y-b.size.H/2+pad
		n.box = gui.Rect{X: x, Y: y, W: b.size.W, H: b.size.H}
		centred := func(lb label) label { return lb.at(x+(b.size.W-lb.box.W)/2, y+lb.box.Y) }
		n.label = centred(b.name)
		var heads part
		for _, h := range b.heads {
			heads.labels = append(heads.labels, centred(h))
		}
		if len(heads.labels) > 0 {
			n.parts = append(n.parts, heads)
		}
		for _, sect := range b.sections {
			p := part{y: y + sect.y}
			for _, mb := range sect.members {
				p.labels = append(p.labels, mb.at(x+mb.box.X, y+mb.box.Y))
			}
			n.parts = append(n.parts, p)
		}
	}
	for i := range l.edges {
		e := &l.edges[i]
		for _, p := range res.Edges[i] {
			e.pts = append(e.pts, gui.Pt(p.X+pad, p.Y+pad))
		}
		if e.label != nil {
			c := res.Labels[i]
			e.label.box.X, e.label.box.Y = c.X+pad-e.label.box.W/2, c.Y+pad-e.label.box.H/2
		}
		if n := len(e.pts); n >= 2 {
			if e.tailLabel != nil {
				*e.tailLabel = cardinalityAt(*e.tailLabel, e.pts[0], e.pts[1], em)
			}
			if e.headLabel != nil {
				*e.headLabel = cardinalityAt(*e.headLabel, e.pts[n-1], e.pts[n-2], em)
			}
		}
	}
	for i := range l.group {
		g := &l.group[i]
		r := res.Groups[i]
		g.box = gui.Rect{X: r.X + pad, Y: r.Y + pad, W: r.W, H: r.H}
		g.title = g.title.at(g.box.X+small.Size*0.5, g.box.Y+small.Size*0.3)
	}
	l.size = gui.Size{W: res.Size.W + 2*pad, H: res.Size.H + 2*pad}
	return l, nil
}

// cardinalityAt places a label by an edge's end at tip, the edge running toward from: a little
// along the edge, to its side, clear of the end's mark.
func cardinalityAt(lb label, tip, from gui.Point, em float32) label {
	u, ok := unit(tip, from)
	if !ok {
		return lb.at(tip.X, tip.Y)
	}
	along, aside := em*1.8, em*0.5+max(lb.box.W, lb.box.H)/2
	cx, cy := tip.X+u.X*along-u.Y*aside, tip.Y+u.Y*along+u.X*aside
	return lb.at(cx-lb.box.W/2, cy-lb.box.H/2)
}
