package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.ER] = layER }

// cardEnd is each cardinality's crow's-foot end.
var cardEnd = [...]end{pm.ExactlyOne: endOne, pm.ZeroOrOne: endZeroOne, pm.OneOrMore: endMany, pm.ZeroOrMore: endZeroMany}

// layER lays an ER diagram out as a graph: each entity a box, its name over a table of its
// attributes, each relationship an edge with a crow's foot at either end.
func layER(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	er := d.(*pm.ERDiagram)
	f := th.Font
	title := f
	title.Bold = true
	small := f
	small.Size *= 0.9
	pad := f.Size * 0.6
	l := &Laid{th: th}
	index := map[string]int{}
	in := layout.Input{Dir: dirOf(er.Dir), NodeSep: f.Size * 3, RankSep: f.Size * 4}
	for i, e := range er.Entities {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index[e.ID] = i
		fill, stroke, text, sw := styled(e.Style, th)
		n := laidNode{form: formRect, fill: fill, stroke: stroke, width: sw, label: newLabel(m, e.Label, title, wrapAt, text)}
		n.parts, n.box.W, n.box.H = attributeTable(m, e.Attributes, small, text, pad)
		n.box.W = max(n.box.W, n.label.box.W+2*pad, f.Size*6) // a bare name still reads as a box
		n.box.H += n.label.box.H + 2*pad
		in.Nodes = append(in.Nodes, layout.Size{W: n.box.W, H: n.box.H})
		l.nodes = append(l.nodes, n)
	}
	for _, r := range er.Relationships {
		le := layout.Edge{From: index[r.From], To: index[r.To], MinLen: 1}
		e := laidEdge{line: pm.Solid, head: cardEnd[r.ToCard], tail: cardEnd[r.FromCard], color: th.Line}
		if !r.Identifying {
			e.line = pm.Dotted
		}
		if r.Label != "" {
			lb := newLabel(m, r.Label, small, wrapAt, th.Text)
			p := small.Size * 0.3
			le.Label = layout.Size{W: lb.box.W + 2*p, H: lb.box.H + 2*p}
			e.label = &lb
		}
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
	margin := f.Size * 0.5
	for i := range l.nodes {
		n := &l.nodes[i]
		c := res.Nodes[i]
		x, y := c.X-n.box.W/2+margin, c.Y-n.box.H/2+margin
		n.box.X, n.box.Y = x, y
		titleH := n.label.box.H + 2*pad
		n.label = n.label.centredIn(gui.Rect{X: x, Y: y, W: n.box.W, H: titleH})
		// the table was laid at the origin: move it under the title, a divider between them
		for j := range n.parts {
			p := &n.parts[j]
			if j == 0 {
				p.y = y + titleH
			}
			for k := range p.labels {
				p.labels[k].box.X += x
				p.labels[k].box.Y += y + titleH
			}
		}
	}
	for i := range l.edges {
		e := &l.edges[i]
		for _, p := range res.Edges[i] {
			e.pts = append(e.pts, gui.Pt(p.X+margin, p.Y+margin))
		}
		if e.label != nil {
			c := res.Labels[i]
			*e.label = e.label.at(c.X+margin-e.label.box.W/2, c.Y+margin-e.label.box.H/2)
		}
	}
	l.size = gui.Size{W: res.Size.W + 2*margin, H: res.Size.H + 2*margin}
	return l, nil
}

// attributeTable lays an entity's attributes out as a table, its top left at the origin: a part
// per row, the row's type, name, keys and comment each in its own column, left aligned, so every
// row's cells start at the same x. A column no row fills takes no room. It answers the table's
// parts and its size; none and zero for no attributes.
func attributeTable(m gui.Measurer, attrs []pm.Attribute, f gui.Font, text color.NRGBA, pad float32) ([]part, float32, float32) {
	if len(attrs) == 0 {
		return nil, 0, 0
	}
	const cols = 4
	cells := make([][cols]label, len(attrs))
	var widths [cols]float32
	var filled [cols]bool
	for i, a := range attrs {
		for c, s := range [cols]string{a.Type, a.Name, strings.Join(a.Keys, ", "), a.Comment} {
			wrap := float32(0)
			if c == 3 {
				wrap = wrapAt // a comment may be long; the rest are words
			}
			lb := newLabel(m, s, f, wrap, text)
			lb.left = true
			cells[i][c] = lb
			if s != "" {
				filled[c] = true
				widths[c] = max(widths[c], lb.box.W)
			}
		}
	}
	gap := f.Size
	var x [cols]float32
	at := pad
	for c := range cols {
		x[c] = at
		if filled[c] {
			at += widths[c] + gap
		}
	}
	w := at - gap + pad
	parts := make([]part, len(attrs))
	y := pad * 0.5
	for i := range attrs {
		h := f.Size * 1.2
		for c := range cols {
			h = max(h, cells[i][c].box.H)
		}
		for c := range cols {
			if filled[c] && len(cells[i][c].lines) > 0 && cells[i][c].lines[0] != "" {
				parts[i].labels = append(parts[i].labels, cells[i][c].at(x[c], y))
			}
		}
		y += h + pad*0.4
	}
	return parts, w, y + pad*0.5
}
