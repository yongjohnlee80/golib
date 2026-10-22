package mermaid

import (
	"image/color"
	"math"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/picture"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Mermaid Vector Rasterization Pipeline:
//
//	[ *Laid Vector Geometry Model ]
//	               |
//	               +---> 1. Scale Affine Transform (Fit scaling if diagram > width)
//	               +---> 2. Background Group Frames (Filled rounded rects, tabs, dashes)
//	               +---> 3. Under-Layer Edges & Polylines (Dashed or solid links)
//	               +---> 4. Shape Nodes (Forms: Rect, Stadium, Diamond, Cylinder, etc.)
//	               +---> 5. Over-Layer Edges (Connectors routing above nodes)
//	               +---> 6. Group Titles & Section Dividers
//	               +---> 7. Free Text Annotations & Notes
//	               |
//	               v
//	[ gui.Canvas GPU Vector Operations ]
//
// Paint renders the laid-out diagram geometry onto canvas c with its origin at (0, 0).
// Text labels are dynamically shaped on the first frame using the canvas text shaper.
func (l *Laid) Paint(c gui.Canvas) {
	defer picture.Push(c, l.fit)()
	for _, g := range l.group {
		l.frame(c, g)
	}
	l.edgesAt(c, false)
	for _, n := range l.nodes {
		l.node(c, n)
	}
	l.edgesAt(c, true)
	for _, g := range l.group { // over what the frames hold, so a box never hides a frame's title
		if !g.tab && !g.bare && len(g.title.lines) > 0 { // on its group's fill: an edge crossing it never runs through it
			l.ground(c, g.title, or(g.fill, l.th.ClusterFill))
		}
		l.text(c, g.title)
		for _, p := range g.parts {
			for _, lb := range p.labels {
				if p.y > 0 { // on the page: a lifeline or a box under a section's label never runs through it
					l.ground(c, lb, l.th.Background)
				}
				l.text(c, lb)
			}
		}
	}
	for _, t := range l.texts {
		l.text(c, t)
	}
}

// edgesAt draws the edges drawn over the boxes, or under them, then their labels.
func (l *Laid) edgesAt(c gui.Canvas, over bool) {
	for _, e := range l.edges {
		if e.over == over {
			l.edge(c, e)
		}
	}
	for _, e := range l.edges {
		if e.over != over {
			continue
		}
		for _, lb := range []*label{e.label, e.headLabel, e.tailLabel} {
			if lb == nil {
				continue
			}
			if lb == e.label && !e.plain { // the middle label sits on the page, over the line
				l.ground(c, *lb, or(e.ground, l.th.Background))
			}
			l.text(c, *lb)
		}
	}
}

// labelPad is how far a label's ground reaches past its text: what ground draws, and the room
// edgeLabelRoom keeps for it in the layout.
func labelPad(f gui.Font) float32 { return f.Size * 0.3 }

// edgeLabelRoom is the room an edge's label takes in the layout: its text and its ground's pad.
func edgeLabelRoom(lb label) layout.Size {
	pad := labelPad(lb.font)
	return layout.Size{W: lb.box.W + 2*pad, H: lb.box.H + 2*pad}
}

// placeEdges routes each edge along res's polyline for it and centres its label on res's label
// point, moved by margin: a layered layout's coordinates, inside the diagram's margin.
func placeEdges(edges []laidEdge, res layout.Result, margin float32) {
	for i := range edges {
		e := &edges[i]
		for _, p := range res.Edges[i] {
			e.pts = append(e.pts, gui.Pt(p.X+margin, p.Y+margin))
		}
		if e.label != nil {
			c := res.Labels[i]
			*e.label = e.label.at(c.X+margin-e.label.box.W/2, c.Y+margin-e.label.box.H/2)
		}
	}
}

// placeGroups places each group on res's rectangle for it, moved by margin, its title inset at its
// top left by the title font's size.
func placeGroups(groups []laidGroup, res layout.Result, margin float32, title gui.Font) {
	for i := range groups {
		g := &groups[i]
		r := res.Groups[i]
		g.box = gui.Rect{X: r.X + margin, Y: r.Y + margin, W: r.W, H: r.H}
		g.title = g.title.at(g.box.X+title.Size*0.5, g.box.Y+title.Size*0.3)
	}
}

// ground fills col behind a label, a little past it (labelPad).
func (l *Laid) ground(c gui.Canvas, lb label, col color.NRGBA) {
	pad := labelPad(lb.font)
	b := lb.box
	c.FillRRect(gui.Rect{X: b.X - pad, Y: b.Y - pad, W: b.W + 2*pad, H: b.H + 2*pad}, 3, gui.Solid(col))
}

// frame draws a group: its box, its title's tab, and its dividers. Its title and its dividers'
// labels are drawn later, over what it holds.
func (l *Laid) frame(c gui.Canvas, g laidGroup) {
	th := l.th
	fill, stroke := or(g.fill, th.ClusterFill), or(g.stroke, th.ClusterStroke)
	radius := float32(4)
	if g.tab {
		radius = 0
	}
	if !g.bare && fill.A > 0 {
		c.FillRRect(g.box, radius, gui.Solid(fill))
	}
	if g.dashed {
		b := g.box
		dashes(c, []gui.Point{{X: b.X, Y: b.Y}, {X: b.X + b.W, Y: b.Y}, {X: b.X + b.W, Y: b.Y + b.H}, {X: b.X, Y: b.Y + b.H}, {X: b.X, Y: b.Y}},
			1, gui.Solid(stroke), th.Font.Size*0.3)
	} else {
		c.StrokeRRect(g.box, radius, 1, gui.Solid(stroke))
	}
	if g.tab && len(g.title.lines) > 0 {
		// the tab: the title's box, its bottom right corner cut
		t := g.title.box
		pad := g.title.font.Size * 0.4
		x0, y0, x1, y1 := g.box.X, g.box.Y, t.X+t.W+pad, t.Y+t.H+pad*0.6
		cut := (y1 - y0) / 3
		p := new(gui.Path).MoveTo(gui.Pt(x0, y0)).LineTo(gui.Pt(x1+cut, y0)).LineTo(gui.Pt(x1+cut, y1-cut)).
			LineTo(gui.Pt(x1, y1)).LineTo(gui.Pt(x0, y1)).Close()
		c.FillPath(p, gui.Solid(gui.Mix(stroke, th.Background, 0.75)))
		c.StrokePath(p, 1, gui.Solid(stroke))
	}
	l.dividers(c, g.box, g.parts, stroke)
}

// parts draws dividers across r at each part's y, and the part's labels.
func (l *Laid) parts(c gui.Canvas, r gui.Rect, ps []part, stroke color.NRGBA) {
	l.dividers(c, r, ps, stroke)
	for _, p := range ps {
		for _, lb := range p.labels {
			l.text(c, lb)
		}
	}
}

// dividers draws the lines across r at each part's y.
func (l *Laid) dividers(c gui.Canvas, r gui.Rect, ps []part, stroke color.NRGBA) {
	for _, p := range ps {
		if p.y > 0 {
			pts := []gui.Point{{X: r.X, Y: p.y}, {X: r.X + r.W, Y: p.y}}
			if p.dashed {
				dashes(c, pts, 1, gui.Solid(stroke), l.th.Font.Size*0.3)
			} else {
				c.StrokePath(new(gui.Path).MoveTo(pts[0]).LineTo(pts[1]), 1, gui.Solid(stroke))
			}
		}
	}
}

// or returns color c if non-zero, otherwise default color d.
func or(c, d color.NRGBA) color.NRGBA {
	if c.A == 0 {
		return d
	}
	return c
}

// text draws a label's lines, each centred in its box or from its left.
func (l *Laid) text(c gui.Canvas, lb label) {
	y := lb.box.Y
	lh := lb.font.Size * gui.LineHeight
	for _, line := range lb.lines {
		t := l.labels.Shape(c, line, lb.font)
		x := lb.box.X
		if !lb.left {
			x += (lb.box.W - t.Width) / 2
		}
		c.DrawText(t, gui.Pt(x, y+(lh-t.Height)/2), gui.Solid(lb.color))
		y += lh
	}
}

// node draws a box: its form filled and outlined, its label, and its compartments.
func (l *Laid) node(c gui.Canvas, n laidNode) {
	paintForm(c, n, l.th.Font.Size)
	l.text(c, n.label)
	l.parts(c, n.box, n.parts, n.stroke)
}

// paintForm draws a box's form, at em, the font size the diagram is laid out in: a dashed
// outline's dashes and a form's insets scale with it. It reads nothing of a Laid, so a mind map's
// key draws the same forms (PaintMindShape).
func paintForm(c gui.Drawer, n laidNode, em float32) {
	b := n.box
	fill, stroke := gui.Solid(n.fill), gui.Solid(n.stroke)
	x0, y0, x1, y1 := b.X, b.Y, b.X+b.W, b.Y+b.H
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	poly := func(pts ...gui.Point) {
		p := new(gui.Path).Polyline(pts, true)
		c.FillPath(p, fill)
		if n.dashed {
			dashes(c, append(pts, pts[0]), n.width, stroke, em*0.3)
			return
		}
		c.StrokePath(p, n.width, stroke)
	}
	rrect := func(r float32) {
		c.FillRRect(b, r, fill)
		if n.dashed {
			dashes(c, []gui.Point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}, {X: x0, Y: y0}}, n.width, stroke, em*0.3)
			return
		}
		c.StrokeRRect(b, r, n.width, stroke)
	}
	switch n.form {
	case formNone:
	case formRound:
		rrect(min(b.H/4, 8))
	case formStadium:
		rrect(b.H / 2)
	case formCircle, formDoubleCircle:
		c.FillEllipse(b, fill)
		c.StrokePath(ellipse(b), n.width, stroke)
		if n.form == formDoubleCircle {
			in := em * 0.35
			c.StrokePath(ellipse(gui.Rect{X: b.X + in, Y: b.Y + in, W: b.W - 2*in, H: b.H - 2*in}), n.width, stroke)
		}
	case formDiamond:
		poly(gui.Pt(cx, y0), gui.Pt(x1, cy), gui.Pt(cx, y1), gui.Pt(x0, cy))
	case formHexagon:
		s := b.H / 4
		poly(gui.Pt(x0+s, y0), gui.Pt(x1-s, y0), gui.Pt(x1, cy), gui.Pt(x1-s, y1), gui.Pt(x0+s, y1), gui.Pt(x0, cy))
	case formAsymmetric:
		s := b.H / 3
		poly(gui.Pt(x0, y0), gui.Pt(x1, y0), gui.Pt(x1, y1), gui.Pt(x0, y1), gui.Pt(x0+s, cy))
	case formParallelogram:
		s := b.H / 3
		poly(gui.Pt(x0+s, y0), gui.Pt(x1, y0), gui.Pt(x1-s, y1), gui.Pt(x0, y1))
	case formParallelogramAlt:
		s := b.H / 3
		poly(gui.Pt(x0, y0), gui.Pt(x1-s, y0), gui.Pt(x1, y1), gui.Pt(x0+s, y1))
	case formTrapezoid:
		s := b.H / 3
		poly(gui.Pt(x0+s, y0), gui.Pt(x1-s, y0), gui.Pt(x1, y1), gui.Pt(x0, y1))
	case formTrapezoidAlt:
		s := b.H / 3
		poly(gui.Pt(x0, y0), gui.Pt(x1, y0), gui.Pt(x1-s, y1), gui.Pt(x0+s, y1))
	case formCylinder:
		ry := min(b.H/8, b.W/6)
		c.FillRect(gui.Rect{X: x0, Y: y0 + ry, W: b.W, H: b.H - 2*ry}, fill)
		c.FillEllipse(gui.Rect{X: x0, Y: y1 - 2*ry, W: b.W, H: 2 * ry}, fill)
		c.FillEllipse(gui.Rect{X: x0, Y: y0, W: b.W, H: 2 * ry}, fill)
		c.StrokePath(ellipse(gui.Rect{X: x0, Y: y0, W: b.W, H: 2 * ry}), n.width, stroke)
		side := new(gui.Path).MoveTo(gui.Pt(x0, y0+ry)).LineTo(gui.Pt(x0, y1-ry))
		side.MoveTo(gui.Pt(x1, y0+ry)).LineTo(gui.Pt(x1, y1-ry))
		c.StrokePath(side, n.width, stroke)
		c.StrokePath(halfEllipse(gui.Rect{X: x0, Y: y1 - 2*ry, W: b.W, H: 2 * ry}), n.width, stroke)
	case formSubroutine:
		rrect(0)
		in := em * 0.5
		bars := new(gui.Path).MoveTo(gui.Pt(x0+in, y0)).LineTo(gui.Pt(x0+in, y1))
		bars.MoveTo(gui.Pt(x1-in, y0)).LineTo(gui.Pt(x1-in, y1))
		c.StrokePath(bars, n.width, stroke)
	case formNote:
		f := min(em*0.6, b.H/3, b.W/3)
		poly(gui.Pt(x0, y0), gui.Pt(x1-f, y0), gui.Pt(x1, y0+f), gui.Pt(x1, y1), gui.Pt(x0, y1))
		fold := new(gui.Path).MoveTo(gui.Pt(x1-f, y0)).LineTo(gui.Pt(x1-f, y0+f)).LineTo(gui.Pt(x1, y0+f))
		c.StrokePath(fold, n.width, stroke)
	case formBar:
		c.FillRRect(b, min(b.W, b.H)/2, stroke)
	case formStart:
		c.FillEllipse(b, stroke)
	case formEnd:
		c.StrokePath(ellipse(b), n.width, stroke)
		in := min(b.W, b.H) * 0.25
		c.FillEllipse(gui.Rect{X: b.X + in, Y: b.Y + in, W: b.W - 2*in, H: b.H - 2*in}, stroke)
	case formActor:
		// the figure above the label, which the layer places under it
		h := min(b.H-n.label.box.H, em*2.6)
		r := h * 0.16
		head := gui.Rect{X: cx - r, Y: y0, W: 2 * r, H: 2 * r}
		c.FillEllipse(head, fill)
		c.StrokePath(ellipse(head), n.width, stroke)
		neck, hip, foot := y0+2*r, y0+h*0.62, y0+h
		arm := h * 0.3
		body := new(gui.Path).MoveTo(gui.Pt(cx, neck)).LineTo(gui.Pt(cx, hip))
		body.MoveTo(gui.Pt(cx-arm, neck+h*0.12)).LineTo(gui.Pt(cx+arm, neck+h*0.12))
		body.MoveTo(gui.Pt(cx-arm*0.8, foot)).LineTo(gui.Pt(cx, hip)).LineTo(gui.Pt(cx+arm*0.8, foot))
		c.StrokePath(body, n.width, stroke)
	case formFace:
		c.FillEllipse(b, fill)
		c.StrokePath(ellipse(b), n.width, stroke)
		r := min(b.W, b.H) / 2
		eye := r * 0.12
		for _, ex := range []float32{cx - r*0.35, cx + r*0.35} {
			c.FillEllipse(gui.Rect{X: ex - eye, Y: cy - r*0.3 - eye, W: 2 * eye, H: 2 * eye}, stroke)
		}
		// the mouth: its middle below its corners for a smile, above them for a frown
		bend := map[bool]float32{true: r * 0.25, false: -r * 0.2}[n.mood >= 4]
		if n.mood == 3 {
			bend = 0
		}
		y := cy + r*0.35
		if n.mood <= 2 {
			y = cy + r*0.5
		}
		mouth := new(gui.Path).MoveTo(gui.Pt(cx-r*0.4, y)).QuadTo(gui.Pt(cx, y+bend), gui.Pt(cx+r*0.4, y))
		c.StrokePath(mouth, n.width, stroke)
	default: // formRect
		rrect(0)
	}
}

// ellipse is the outline of the ellipse in r (gui.Path.Ellipse).
func ellipse(r gui.Rect) *gui.Path { return new(gui.Path).Ellipse(r) }

// halfEllipse is the lower half of the ellipse in r: a cylinder's base.
func halfEllipse(r gui.Rect) *gui.Path {
	const k = gui.Kappa
	cx, cy, rx, ry := r.X+r.W/2, r.Y+r.H/2, r.W/2, r.H/2
	return new(gui.Path).MoveTo(gui.Pt(cx+rx, cy)).
		CubeTo(gui.Pt(cx+rx, cy+ry*k), gui.Pt(cx+rx*k, cy+ry), gui.Pt(cx, cy+ry)).
		CubeTo(gui.Pt(cx-rx*k, cy+ry), gui.Pt(cx-rx, cy+ry*k), gui.Pt(cx-rx, cy))
}

// endLength is how far an end reaches back along its edge from the tip: the line stops there, so
// a hollow end is not drawn through.
func endLength(e end, size float32) float32 { return ends[e].reach * size }

// edge draws an edge's line through its points, its corners rounded, and its ends.
func (l *Laid) edge(c gui.Canvas, e laidEdge) {
	if e.line == pm.Invisible || len(e.pts) < 2 {
		return
	}
	width := float32(1.3)
	if e.line == pm.Thick {
		width = 2.8
	}
	brush := gui.Solid(e.color)
	size := l.th.Font.Size * 0.7
	pts := e.pts
	n := len(pts)
	if hl, tl := endLength(e.head, size), endLength(e.tail, size); hl > 0 || tl > 0 {
		pts = append([]gui.Point(nil), pts...)
		pts[n-1] = back(pts[n-1], pts[n-2], hl)
		pts[0] = back(pts[0], pts[1], tl)
	}
	if e.line == pm.Dotted {
		dashes(c, pts, width, brush, l.th.Font.Size*0.3)
	} else {
		c.StrokePath(rounded(pts, l.th.Font.Size*0.6), width, brush)
	}
	l.end(c, e.head, e.pts[n-1], e.pts[n-2], size, width, brush)
	l.end(c, e.tail, e.pts[0], e.pts[1], size, width, brush)
}

// end draws an edge's end with its tip at tip, the edge coming from from.
func (l *Laid) end(c gui.Canvas, kind end, tip, from gui.Point, size, width float32, b gui.Brush) {
	u, ok := unit(from, tip)
	if !ok || ends[kind].draw == nil {
		return
	}
	ends[kind].draw(endMark{c: c, kind: kind, tip: tip, u: u, size: size, w: size * 0.45, width: width, b: b, bg: gui.Solid(l.th.Background)})
}

// endMark is an end being drawn: its tip, the edge's direction toward it (u), the end's size and
// half breadth (w), the line's width and brush, and the background a hollow end is filled with.
type endMark struct {
	c              gui.Canvas
	kind           end
	tip, u         gui.Point
	size, w, width float32
	b, bg          gui.Brush
}

// at is the point along the edge back from the tip, and across it (along the normal).
func (m endMark) at(along, across float32) gui.Point {
	nx, ny := -m.u.Y, m.u.X
	return gui.Pt(m.tip.X-m.u.X*along+nx*across, m.tip.Y-m.u.Y*along+ny*across)
}

// endSpec is an end kind: how far it reaches back from the tip, in end sizes (endLength), and how
// it draws. endNone has neither.
type endSpec struct {
	reach float32
	draw  func(m endMark)
}

// diamondReach is a diamond's length, in end sizes: the line stops where the diamond starts.
const diamondReach = 1.6

// ends is each end kind's reach and drawing.
var ends = [...]endSpec{
	endNone: {},
	endArrow: {0.8, func(m endMark) {
		m.c.FillPath(new(gui.Path).MoveTo(m.tip).LineTo(m.at(m.size, m.w)).LineTo(m.at(m.size, -m.w)).Close(), m.b)
	}},
	endOpen: {0, func(m endMark) {
		m.c.StrokePath(new(gui.Path).MoveTo(m.at(m.size, m.w)).LineTo(m.tip).LineTo(m.at(m.size, -m.w)), m.width, m.b)
	}},
	endHalf: {0, func(m endMark) {
		m.c.StrokePath(new(gui.Path).MoveTo(m.at(m.size, m.w)).LineTo(m.tip), m.width, m.b)
	}},
	endDot: {0, func(m endMark) {
		r := m.size * 0.35
		o := m.at(r, 0)
		m.c.FillEllipse(gui.Rect{X: o.X - r, Y: o.Y - r, W: 2 * r, H: 2 * r}, m.b)
	}},
	endCross: {0, func(m endMark) {
		r := m.size * 0.35
		o := m.at(r*1.4, 0)
		x := new(gui.Path).MoveTo(gui.Pt(o.X-r, o.Y-r)).LineTo(gui.Pt(o.X+r, o.Y+r))
		x.MoveTo(gui.Pt(o.X-r, o.Y+r)).LineTo(gui.Pt(o.X+r, o.Y-r))
		m.c.StrokePath(x, m.width, m.b)
	}},
	endTriangle: {1, func(m endMark) {
		p := new(gui.Path).MoveTo(m.tip).LineTo(m.at(m.size, m.w*1.2)).LineTo(m.at(m.size, -m.w*1.2)).Close()
		m.c.FillPath(p, m.bg)
		m.c.StrokePath(p, m.width, m.b)
	}},
	endDiamond:     {diamondReach, drawDiamond},
	endDiamondOpen: {diamondReach, drawDiamond},
	endOne:         {0, drawCrowsFoot},
	endZeroOne:     {0, drawCrowsFoot},
	endMany:        {0, drawCrowsFoot},
	endZeroMany:    {0, drawCrowsFoot},
}

// ends reaches every end kind: a kind added before endKinds fails to build here until the table
// is as long, rather than indexing past it. The table is keyed, so an entry dropped from its middle
// leaves a hole this does not see; TestEveryEndKindDraws does.
var _ = [1]struct{}{}[len(ends)-int(endKinds)]

// drawDiamond is a diamond end: filled for composition, hollow (the background) for aggregation.
func drawDiamond(m endMark) {
	d := m.size * diamondReach
	p := new(gui.Path).MoveTo(m.tip).LineTo(m.at(d/2, m.w)).LineTo(m.at(d, 0)).LineTo(m.at(d/2, -m.w)).Close()
	if m.kind == endDiamond {
		m.c.FillPath(p, m.b)
	} else {
		m.c.FillPath(p, m.bg)
	}
	m.c.StrokePath(p, m.width, m.b)
}

// drawCrowsFoot is a crow's foot end: the inner mark (a bar, or a ring for zero) and the outer (a
// bar, or the foot for many), the outer nearer the box.
func drawCrowsFoot(m endMark) {
	p := new(gui.Path)
	bar := func(along float32) { p.MoveTo(m.at(along, m.w)).LineTo(m.at(along, -m.w)) }
	if m.kind == endMany || m.kind == endZeroMany {
		p.MoveTo(m.at(0, m.w)).LineTo(m.at(m.size, 0)).LineTo(m.at(0, -m.w))
	} else {
		bar(m.size * 0.5)
	}
	if m.kind == endZeroOne || m.kind == endZeroMany {
		r := m.size * 0.3
		o := m.at(m.size*1.1+r, 0)
		ring := gui.Rect{X: o.X - r, Y: o.Y - r, W: 2 * r, H: 2 * r}
		m.c.FillEllipse(ring, m.bg)
		m.c.StrokePath(ellipse(ring), m.width, m.b)
	} else {
		bar(m.size * 1.1)
	}
	m.c.StrokePath(p, m.width, m.b)
}

// unit is the unit vector from a to b.
func unit(a, b gui.Point) (gui.Point, bool) {
	d := dist(a, b)
	if d == 0 {
		return gui.Point{}, false
	}
	return gui.Pt((b.X-a.X)/d, (b.Y-a.Y)/d), true
}

// back is the point d short of tip, toward from.
func back(tip, from gui.Point, d float32) gui.Point {
	u, ok := unit(from, tip)
	if !ok {
		return tip
	}
	return gui.Pt(tip.X-u.X*d, tip.Y-u.Y*d)
}

// rounded is a polyline with each corner rounded by up to r.
func rounded(pts []gui.Point, r float32) *gui.Path {
	p := new(gui.Path).MoveTo(pts[0])
	for i := 1; i < len(pts)-1; i++ {
		a, b, n := pts[i-1], pts[i], pts[i+1]
		d := min(r, dist(a, b)/2, dist(b, n)/2)
		p.LineTo(back(b, a, d))
		p.QuadTo(b, back(b, n, d))
	}
	return p.LineTo(pts[len(pts)-1])
}

// dist calculates the Euclidean distance between points a and b.
func dist(a, b gui.Point) float32 {
	return float32(math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y)))
}

// dashes strokes the polyline as dashes of length dash with gaps as long, as one path (gui.Dash:
// bounded, and solid where a dash would be too fine to see).
func dashes(c gui.Drawer, pts []gui.Point, width float32, b gui.Brush, dash float32) {
	p := new(gui.Path)
	for _, run := range gui.Dash(pts, []float32{dash}) {
		p.MoveTo(run[0])
		for _, q := range run[1:] {
			p.LineTo(q)
		}
	}
	c.StrokePath(p, width, b)
}
