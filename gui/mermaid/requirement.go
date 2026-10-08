package mermaid

import (
	"context"
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Requirement] = layRequirement }

// requirementFields are a node's fields as its box lists them, labelled as Mermaid labels them;
// the ones it does not give are left out.
func requirementFields(n pm.RequirementNode) []string {
	var out []string
	add := func(name, v string) {
		if v != "" {
			out = append(out, name+": "+v)
		}
	}
	if n.Type == pm.ReqElement {
		add("Type", n.ElemType)
		add("Doc Ref", n.DocRef)
		return out
	}
	add("ID", n.ID)
	add("Text", n.Text)
	if n.Risk != 0 {
		add("Risk", n.Risk.String())
	}
	if n.Verify != 0 {
		add("Verification", n.Verify.String())
	}
	return out
}

// layRequirement lays a requirement diagram out in layers: each requirement and element a box (its
// «type» and name, then its fields under a divider), each relationship an edge named «kind». A
// container holds what it contains by a solid line from a dot at the container; every other
// relationship is a dotted line with an open arrow at its destination.
func layRequirement(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	rd := d.(*pm.RequirementDiagram)
	f := th.Font
	small := f
	small.Size *= 0.85
	bold := f
	bold.Bold = true
	em := f.Size
	padX, padY := em*0.7, em*0.4
	l := &Laid{th: th}
	in := layout.Input{Dir: dirOf(rd.Dir), NodeSep: em * 3, RankSep: em * 3.5}
	index := map[string]int{}
	for i, n := range rd.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index[n.Name] = i
		fill, stroke, text, sw := styled(n.Style, th)
		// the box at the origin: the type, the name, then the fields under a divider
		head := newLabel(m, "«"+n.Type.Stereotype()+"»", small, wrapAt, text).at(0, padY)
		name := newLabel(m, n.Name, bold, wrapAt, text).at(0, head.box.Y+head.box.H)
		w := max(head.box.W, name.box.W) + 2*padX
		y := name.box.Y + name.box.H + padY
		node := laidNode{form: formRect, fill: fill, stroke: stroke, width: sw, label: name,
			parts: []part{{labels: []label{head}}}}
		if fields := requirementFields(n); len(fields) > 0 {
			p := part{y: y}
			y += padY
			for _, s := range fields {
				lb := newLabel(m, s, f, wrapAt, text).at(padX, y)
				lb.left = true
				p.labels = append(p.labels, lb)
				y += lb.box.H
				w = max(w, lb.box.W+2*padX)
			}
			y += padY
			node.parts = append(node.parts, p)
		}
		w = max(w, em*7) // a box with a short name still reads as a box
		// the title lines centre in the box's width, known only now
		node.parts[0].labels[0].box.X = (w - head.box.W) / 2
		node.label.box.X = (w - name.box.W) / 2
		node.box = gui.Rect{W: w, H: y}
		in.Nodes = append(in.Nodes, layout.Size{W: w, H: y})
		l.nodes = append(l.nodes, node)
	}
	for _, r := range rd.Relations {
		le := layout.Edge{From: index[r.Src], To: index[r.Dst], MinLen: 1}
		e := laidEdge{line: pm.Dotted, head: endOpen, color: th.Line}
		if r.Kind == pm.ReqContains {
			e.line, e.head, e.tail = pm.Solid, endNone, endDot
		}
		lb := newLabel(m, "«"+r.Kind.String()+"»", small, wrapAt, th.Text)
		pad := small.Size * 0.3
		le.Label = layout.Size{W: lb.box.W + 2*pad, H: lb.box.H + 2*pad}
		e.label = &lb
		in.Edges = append(in.Edges, le)
		l.edges = append(l.edges, e)
	}
	res, err := layout.Layered(ctx, in, layout.Limits{})
	if err != nil {
		if errors.Is(err, layout.ErrTooLarge) {
			return nil, fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
		}
		return nil, err
	}
	margin := em * 0.5
	for i := range l.nodes {
		n := &l.nodes[i]
		c := res.Nodes[i]
		x, y := c.X-n.box.W/2+margin, c.Y-n.box.H/2+margin
		n.box.X, n.box.Y = x, y
		n.label = n.label.at(n.label.box.X+x, n.label.box.Y+y)
		for j := range n.parts {
			p := &n.parts[j]
			if p.y > 0 {
				p.y += y
			}
			for k := range p.labels {
				p.labels[k] = p.labels[k].at(p.labels[k].box.X+x, p.labels[k].box.Y+y)
			}
		}
	}
	for i := range l.edges {
		e := &l.edges[i]
		for _, p := range res.Edges[i] {
			e.pts = append(e.pts, gui.Pt(p.X+margin, p.Y+margin))
		}
		c := res.Labels[i]
		*e.label = e.label.at(c.X+margin-e.label.box.W/2, c.Y+margin-e.label.box.H/2)
	}
	l.size = gui.Size{W: res.Size.W + 2*margin, H: res.Size.H + 2*margin}
	return l, nil
}
