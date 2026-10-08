package mermaid

import (
	"image/color"
	"math"

	"gioui.org/f32"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Paint draws the diagram on c, its top left at c's origin. On the loop: it shapes each label line
// with c's shaper the first time it is drawn.
func (l *Laid) Paint(c gui.Canvas) {
	if l.fit != 1 {
		c.PushTransform(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(l.fit, l.fit)))
		defer c.Pop()
	}
	for _, g := range l.group {
		l.frame(c, g)
	}
	l.edgesAt(c, false)
	for _, n := range l.nodes {
		l.node(c, n)
	}
	l.edgesAt(c, true)
	for _, g := range l.group { // over what the frames hold, so a box never hides a frame's title
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

// ground fills col behind a label, a little past it.
func (l *Laid) ground(c gui.Canvas, lb label, col color.NRGBA) {
	pad := lb.font.Size * 0.3
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
		c.FillPath(p, gui.Solid(mix(stroke, th.Background, 0.75)))
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

func or(c, d color.NRGBA) color.NRGBA {
	if c.A == 0 {
		return d
	}
	return c
}

// mix is a weighted between a (t = 0) and b (t = 1).
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: m(a.A, b.A)}
}

// text draws a label's lines, each centred in its box or from its left.
func (l *Laid) text(c gui.Canvas, lb label) {
	y := lb.box.Y
	lh := lb.font.Size * 1.2
	for _, line := range lb.lines {
		t := l.shape(c, line, lb.font)
		x := lb.box.X
		if !lb.left {
			x += (lb.box.W - t.Width) / 2
		}
		c.DrawText(t, gui.Pt(x, y+(lh-t.Height)/2), gui.Solid(lb.color))
		y += lh
	}
}

func (l *Laid) shape(c gui.Canvas, s string, f gui.Font) *gui.TextLayout {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := shapeKey{s, f}
	if t, ok := l.shaped[k]; ok {
		return t
	}
	if l.shaped == nil {
		l.shaped = map[shapeKey]*gui.TextLayout{}
	}
	t := c.Text().Layout(s, f, 0)
	l.shaped[k] = t
	return t
}

// node draws a box: its form filled and outlined, its label, and its compartments.
func (l *Laid) node(c gui.Canvas, n laidNode) {
	l.outline(c, n)
	l.text(c, n.label)
	l.parts(c, n.box, n.parts, n.stroke)
}

// outline draws a box's form.
func (l *Laid) outline(c gui.Canvas, n laidNode) {
	b := n.box
	fill, stroke := gui.Solid(n.fill), gui.Solid(n.stroke)
	x0, y0, x1, y1 := b.X, b.Y, b.X+b.W, b.Y+b.H
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	em := l.th.Font.Size
	poly := func(pts ...gui.Point) {
		p := new(gui.Path).MoveTo(pts[0])
		for _, q := range pts[1:] {
			p.LineTo(q)
		}
		p.Close()
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
	default: // formRect
		rrect(0)
	}
}

// ellipse is the outline of the ellipse in r, as four cubic arcs.
func ellipse(r gui.Rect) *gui.Path {
	const k = 0.5523 // the cubic's handle for a quarter circle
	cx, cy, rx, ry := r.X+r.W/2, r.Y+r.H/2, r.W/2, r.H/2
	return new(gui.Path).MoveTo(gui.Pt(cx+rx, cy)).
		CubeTo(gui.Pt(cx+rx, cy+ry*k), gui.Pt(cx+rx*k, cy+ry), gui.Pt(cx, cy+ry)).
		CubeTo(gui.Pt(cx-rx*k, cy+ry), gui.Pt(cx-rx, cy+ry*k), gui.Pt(cx-rx, cy)).
		CubeTo(gui.Pt(cx-rx, cy-ry*k), gui.Pt(cx-rx*k, cy-ry), gui.Pt(cx, cy-ry)).
		CubeTo(gui.Pt(cx+rx*k, cy-ry), gui.Pt(cx+rx, cy-ry*k), gui.Pt(cx+rx, cy)).Close()
}

// halfEllipse is the lower half of the ellipse in r: a cylinder's base.
func halfEllipse(r gui.Rect) *gui.Path {
	const k = 0.5523
	cx, cy, rx, ry := r.X+r.W/2, r.Y+r.H/2, r.W/2, r.H/2
	return new(gui.Path).MoveTo(gui.Pt(cx+rx, cy)).
		CubeTo(gui.Pt(cx+rx, cy+ry*k), gui.Pt(cx+rx*k, cy+ry), gui.Pt(cx, cy+ry)).
		CubeTo(gui.Pt(cx-rx*k, cy+ry), gui.Pt(cx-rx, cy+ry*k), gui.Pt(cx-rx, cy))
}

// endLength is how far an end reaches back along its edge from the tip: the line stops there, so
// a hollow end is not drawn through.
func endLength(e end, size float32) float32 {
	switch e {
	case endArrow:
		return size * 0.8
	case endTriangle:
		return size
	case endDiamond, endDiamondOpen:
		return size * 1.6
	}
	return 0
}

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
	if !ok {
		return
	}
	nx, ny := -u.Y, u.X // the normal
	at := func(along, across float32) gui.Point {
		return gui.Pt(tip.X-u.X*along+nx*across, tip.Y-u.Y*along+ny*across)
	}
	w := size * 0.45
	switch kind {
	case endArrow:
		c.FillPath(new(gui.Path).MoveTo(tip).LineTo(at(size, w)).LineTo(at(size, -w)).Close(), b)
	case endOpen:
		c.StrokePath(new(gui.Path).MoveTo(at(size, w)).LineTo(tip).LineTo(at(size, -w)), width, b)
	case endHalf:
		c.StrokePath(new(gui.Path).MoveTo(at(size, w)).LineTo(tip), width, b)
	case endDot:
		r := size * 0.35
		o := at(r, 0)
		c.FillEllipse(gui.Rect{X: o.X - r, Y: o.Y - r, W: 2 * r, H: 2 * r}, b)
	case endCross:
		r := size * 0.35
		o := at(r*1.4, 0)
		x := new(gui.Path).MoveTo(gui.Pt(o.X-r, o.Y-r)).LineTo(gui.Pt(o.X+r, o.Y+r))
		x.MoveTo(gui.Pt(o.X-r, o.Y+r)).LineTo(gui.Pt(o.X+r, o.Y-r))
		c.StrokePath(x, width, b)
	case endTriangle:
		p := new(gui.Path).MoveTo(tip).LineTo(at(size, w*1.2)).LineTo(at(size, -w*1.2)).Close()
		c.FillPath(p, gui.Solid(l.th.Background))
		c.StrokePath(p, width, b)
	case endDiamond, endDiamondOpen:
		d := size * 1.6
		p := new(gui.Path).MoveTo(tip).LineTo(at(d/2, w)).LineTo(at(d, 0)).LineTo(at(d/2, -w)).Close()
		if kind == endDiamond {
			c.FillPath(p, b)
		} else {
			c.FillPath(p, gui.Solid(l.th.Background))
		}
		c.StrokePath(p, width, b)
	case endOne, endZeroOne, endMany, endZeroMany:
		// crow's foot: the inner mark (a bar, or a ring for zero) and the outer (a bar, or the foot
		// for many), the outer nearer the box
		p := new(gui.Path)
		bar := func(along float32) { p.MoveTo(at(along, w)).LineTo(at(along, -w)) }
		if kind == endMany || kind == endZeroMany {
			p.MoveTo(at(0, w)).LineTo(at(size, 0)).LineTo(at(0, -w))
		} else {
			bar(size * 0.5)
		}
		if kind == endZeroOne || kind == endZeroMany {
			r := size * 0.3
			o := at(size*1.1+r, 0)
			ring := gui.Rect{X: o.X - r, Y: o.Y - r, W: 2 * r, H: 2 * r}
			c.FillEllipse(ring, gui.Solid(l.th.Background))
			c.StrokePath(ellipse(ring), width, b)
		} else {
			bar(size * 1.1)
		}
		c.StrokePath(p, width, b)
	}
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

func dist(a, b gui.Point) float32 {
	return float32(math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y)))
}

// dashes strokes the polyline as dashes of length dash with gaps as long.
func dashes(c gui.Canvas, pts []gui.Point, width float32, b gui.Brush, dash float32) {
	p := new(gui.Path)
	on, left := true, dash
	for i := 1; i < len(pts); i++ {
		a, z := pts[i-1], pts[i]
		seg := dist(a, z)
		for at := float32(0); at < seg; {
			step := min(left, seg-at)
			from := lerp(a, z, at/seg)
			to := lerp(a, z, (at+step)/seg)
			if on {
				p.MoveTo(from).LineTo(to)
			}
			at += step
			left -= step
			if left <= 0 {
				on, left = !on, dash
			}
		}
	}
	c.StrokePath(p, width, b)
}

func lerp(a, b gui.Point, t float32) gui.Point {
	return gui.Pt(a.X+(b.X-a.X)*t, a.Y+(b.Y-a.Y)*t)
}
