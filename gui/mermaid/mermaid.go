// Package mermaid draws Mermaid diagrams natively on a gui Canvas: parse/mermaid's models, laid
// out by graph/layout, in a theme's colours, as vectors (ADR 1791430025).
//
// Lay runs on a worker: it parses, measures every label with a Measurer the worker holds (never
// the Canvas's shaper, which only the UI loop may use) and lays the diagram out. Its result is a
// Laid: plain geometry, label strings and fonts. Paint draws a Laid on the loop, shaping its labels
// there. Flowcharts are drawn; any other diagram is mermaid.ErrUnsupported, for a caller's
// fallback to render.
package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"

	"gioui.org/f32"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Theme is what a diagram is drawn in: its colours and its labels' font.
type Theme struct {
	Background, Text, Line, NodeFill, NodeStroke, ClusterFill, ClusterStroke color.NRGBA
	Font                                                                     gui.Font
}

// wrapAt is the width a label wraps at, in logical pixels, as Mermaid's does.
const wrapAt = 200

// Lay parses src and lays it out for width (a wider diagram is scaled to fit), measuring with m.
// A diagram golib does not draw is mermaid.ErrUnsupported, and one over a limit
// mermaid.ErrTooLarge (both for a fallback); a malformed one is a *mermaid.SyntaxError.
func Lay(ctx context.Context, src string, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	d, err := pm.Parse(src)
	if err != nil {
		return nil, err
	}
	fc, ok := d.(*pm.FlowchartDiagram)
	if !ok {
		return nil, fmt.Errorf("%w: %v diagrams", pm.ErrUnsupported, d.Kind())
	}
	return layFlowchart(ctx, fc, width, th, m)
}

// Laid is a diagram laid out: shapes, polylines and labels, all plain values. It is safe to share
// between widgets; only Paint, on the loop, keeps anything (the labels it shapes).
type Laid struct {
	size  gui.Size // as drawn, after the fit
	fit   float32  // the scale that fits the diagram to the width asked; 1 when it fits
	th    Theme
	nodes []laidNode
	edges []laidEdge
	group []laidGroup

	mu     sync.Mutex
	shaped map[shapeKey]*gui.TextLayout // the loop's shaping of each label line
}

type label struct {
	lines []string
	font  gui.Font
	box   gui.Rect // the label's lines, centred in it
	color color.NRGBA
}

type laidNode struct {
	shape        pm.Shape
	box          gui.Rect
	fill, stroke color.NRGBA
	width        float32
	label        label
}

type laidEdge struct {
	pts        []gui.Point
	line       pm.LineKind
	head, tail pm.Arrow
	color      color.NRGBA
	label      *label
}

type laidGroup struct {
	box   gui.Rect
	title label
}

type shapeKey struct {
	text string
	font gui.Font
}

// Size is the diagram as Paint draws it.
func (l *Laid) Size() gui.Size { return l.size }

func layFlowchart(ctx context.Context, fc *pm.FlowchartDiagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	f := th.Font
	small := f
	small.Size *= 0.9
	l := &Laid{th: th, fit: 1}
	index := map[string]int{}
	in := layout.Input{Dir: dirOf(fc.Dir), NodeSep: f.Size * 2.6, RankSep: f.Size * 3, GroupPad: f.Size}
	for i, n := range fc.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index[n.ID] = i
		lines, ts := m.Wrap(n.Label, f, wrapAt)
		w, h := nodeSize(n.Shape, ts, f.Size)
		in.Nodes = append(in.Nodes, layout.Size{W: w, H: h})
		fill, stroke, text, sw := styled(n.Style, th)
		l.nodes = append(l.nodes, laidNode{shape: n.Shape, fill: fill, stroke: stroke, width: sw,
			label: label{lines: lines, font: f, color: text, box: gui.Rect{W: ts.W, H: ts.H}}})
	}
	for _, e := range fc.Edges {
		le := layout.Edge{From: index[e.From], To: index[e.To], MinLen: max(e.MinLen, 1)}
		var lb *label
		if e.Label != "" {
			lines, ts := m.Wrap(e.Label, small, wrapAt)
			pad := small.Size * 0.3
			le.Label = layout.Size{W: ts.W + 2*pad, H: ts.H + 2*pad}
			lb = &label{lines: lines, font: small, color: th.Text, box: gui.Rect{W: ts.W, H: ts.H}}
		}
		in.Edges = append(in.Edges, le)
		l.edges = append(l.edges, laidEdge{line: e.Line, head: e.Head, tail: e.Tail, color: th.Line, label: lb})
	}
	// a subgraph's box holds its own nodes and every nested subgraph's
	parent := map[string]string{}
	for _, s := range fc.Subgraphs {
		parent[s.ID] = s.Parent
	}
	for _, s := range fc.Subgraphs {
		var members []int
		for i, n := range fc.Nodes {
			for g := n.Subgraph; g != ""; g = parent[g] {
				if g == s.ID {
					members = append(members, i)
					break
				}
			}
		}
		if len(members) == 0 {
			continue
		}
		in.Groups = append(in.Groups, members)
		title := s.Title
		if title == "" {
			title = s.ID
		}
		lines, ts := m.Wrap(title, small, wrapAt)
		l.group = append(l.group, laidGroup{title: label{lines: lines, font: small, color: th.Text, box: gui.Rect{W: ts.W, H: ts.H}}})
	}
	res, err := layout.Layered(ctx, in, layout.Limits{})
	if err != nil {
		if errors.Is(err, layout.ErrTooLarge) {
			return nil, fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
		}
		return nil, err
	}
	pad := f.Size * 0.5
	for i := range l.nodes {
		n := &l.nodes[i]
		sz, c := in.Nodes[i], res.Nodes[i]
		n.box = gui.Rect{X: c.X - sz.W/2 + pad, Y: c.Y - sz.H/2 + pad, W: sz.W, H: sz.H}
		n.label.box.X, n.label.box.Y = n.box.X+(n.box.W-n.label.box.W)/2, n.box.Y+(n.box.H-n.label.box.H)/2
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
	}
	for i := range l.group {
		g := &l.group[i]
		r := res.Groups[i]
		g.box = gui.Rect{X: r.X + pad, Y: r.Y + pad, W: r.W, H: r.H}
		g.title.box.X, g.title.box.Y = g.box.X+small.Size*0.5, g.box.Y+small.Size*0.3
	}
	l.size = gui.Size{W: res.Size.W + 2*pad, H: res.Size.H + 2*pad}
	if width > 0 && l.size.W > width {
		l.fit = width / l.size.W
		l.size = gui.Size{W: width, H: l.size.H * l.fit}
	}
	return l, nil
}

func dirOf(d pm.Dir) layout.Dir {
	switch d {
	case pm.BT:
		return layout.BT
	case pm.LR:
		return layout.LR
	case pm.RL:
		return layout.RL
	}
	return layout.TB
}

// nodeSize is a node's box for its label's size, by its shape: room for the shape's slants and
// curves around the text, as Mermaid gives it.
func nodeSize(s pm.Shape, text gui.Size, em float32) (w, h float32) {
	w, h = text.W+em*1.8, text.H+em*1.2
	switch s {
	case pm.Round:
		w += em * 0.6
	case pm.Stadium:
		w += h * 0.5
	case pm.Subroutine:
		w += em * 1.2
	case pm.Cylinder:
		h += em * 0.9
	case pm.Circle, pm.DoubleCircle:
		d := max(w, h)
		w, h = d, d
		if s == pm.DoubleCircle {
			w, h = w+em*0.8, h+em*0.8
		}
	case pm.Diamond:
		w, h = w+h*0.9, h+w*0.45
	case pm.Hexagon, pm.Asymmetric:
		w += h * 0.5
	case pm.Parallelogram, pm.ParallelogramAlt, pm.Trapezoid, pm.TrapezoidAlt:
		w += h * 0.7
	}
	return w, h
}

// styled is a node's colours: the theme's, with its style's fill, stroke, color and stroke-width
// where they are colours and lengths it can read.
func styled(st pm.Style, th Theme) (fill, stroke, text color.NRGBA, width float32) {
	fill, stroke, text, width = th.NodeFill, th.NodeStroke, th.Text, 1.2
	for _, p := range st {
		switch p.Name {
		case "fill":
			if c, ok := cssColor(p.Value); ok {
				fill = c
			}
		case "stroke":
			if c, ok := cssColor(p.Value); ok {
				stroke = c
			}
		case "color":
			if c, ok := cssColor(p.Value); ok {
				text = c
			}
		case "stroke-width":
			if v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(p.Value), "px"), 32); err == nil && v > 0 {
				width = float32(min(v, 8))
			}
		}
	}
	return fill, stroke, text, width
}

// cssColor reads #rgb, #rgba, #rrggbb and #rrggbbaa; anything else is not read.
func cssColor(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return color.NRGBA{}, false
	}
	h := s[1:]
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, r := range h {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		h = b.String()
	}
	if len(h) == 6 {
		h += "ff"
	}
	if len(h) != 8 {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}, true
}

// Paint draws the diagram on c, its top left at c's origin. On the loop: it shapes each label line
// with c's shaper the first time it is drawn.
func (l *Laid) Paint(c gui.Canvas) {
	if l.fit != 1 {
		c.PushTransform(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(l.fit, l.fit)))
		defer c.Pop()
	}
	th := l.th
	for _, g := range l.group {
		c.FillRRect(g.box, 4, gui.Solid(th.ClusterFill))
		c.StrokeRRect(g.box, 4, 1, gui.Solid(th.ClusterStroke))
		l.text(c, g.title, false)
	}
	for _, e := range l.edges {
		l.edge(c, e)
	}
	for _, e := range l.edges {
		if e.label != nil {
			pad := e.label.font.Size * 0.3
			b := e.label.box
			c.FillRRect(gui.Rect{X: b.X - pad, Y: b.Y - pad, W: b.W + 2*pad, H: b.H + 2*pad}, 3, gui.Solid(th.Background))
			l.text(c, *e.label, true)
		}
	}
	for _, n := range l.nodes {
		l.node(c, n)
		l.text(c, n.label, true)
	}
}

// text draws a label's lines, each centred in its box (or from its left, for a title).
func (l *Laid) text(c gui.Canvas, lb label, centre bool) {
	y := lb.box.Y
	lh := lb.font.Size * 1.2
	for _, line := range lb.lines {
		t := l.shape(c, line, lb.font)
		x := lb.box.X
		if centre {
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

// node draws a node's shape, filled and outlined.
func (l *Laid) node(c gui.Canvas, n laidNode) {
	b := n.box
	fill, stroke := gui.Solid(n.fill), gui.Solid(n.stroke)
	x0, y0, x1, y1 := b.X, b.Y, b.X+b.W, b.Y+b.H
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	poly := func(pts ...gui.Point) {
		p := new(gui.Path).MoveTo(pts[0])
		for _, q := range pts[1:] {
			p.LineTo(q)
		}
		p.Close()
		c.FillPath(p, fill)
		c.StrokePath(p, n.width, stroke)
	}
	switch n.shape {
	case pm.Round:
		c.FillRRect(b, min(b.H/4, 8), fill)
		c.StrokeRRect(b, min(b.H/4, 8), n.width, stroke)
	case pm.Stadium:
		c.FillRRect(b, b.H/2, fill)
		c.StrokeRRect(b, b.H/2, n.width, stroke)
	case pm.Circle, pm.DoubleCircle:
		c.FillEllipse(b, fill)
		ring := ellipse(b)
		c.StrokePath(ring, n.width, stroke)
		if n.shape == pm.DoubleCircle {
			in := l.th.Font.Size * 0.35
			c.StrokePath(ellipse(gui.Rect{X: b.X + in, Y: b.Y + in, W: b.W - 2*in, H: b.H - 2*in}), n.width, stroke)
		}
	case pm.Diamond:
		poly(gui.Pt(cx, y0), gui.Pt(x1, cy), gui.Pt(cx, y1), gui.Pt(x0, cy))
	case pm.Hexagon:
		s := b.H / 4
		poly(gui.Pt(x0+s, y0), gui.Pt(x1-s, y0), gui.Pt(x1, cy), gui.Pt(x1-s, y1), gui.Pt(x0+s, y1), gui.Pt(x0, cy))
	case pm.Asymmetric:
		s := b.H / 3
		poly(gui.Pt(x0, y0), gui.Pt(x1, y0), gui.Pt(x1, y1), gui.Pt(x0, y1), gui.Pt(x0+s, cy))
	case pm.Parallelogram:
		s := b.H / 3
		poly(gui.Pt(x0+s, y0), gui.Pt(x1, y0), gui.Pt(x1-s, y1), gui.Pt(x0, y1))
	case pm.ParallelogramAlt:
		s := b.H / 3
		poly(gui.Pt(x0, y0), gui.Pt(x1-s, y0), gui.Pt(x1, y1), gui.Pt(x0+s, y1))
	case pm.Trapezoid:
		s := b.H / 3
		poly(gui.Pt(x0+s, y0), gui.Pt(x1-s, y0), gui.Pt(x1, y1), gui.Pt(x0, y1))
	case pm.TrapezoidAlt:
		s := b.H / 3
		poly(gui.Pt(x0, y0), gui.Pt(x1, y0), gui.Pt(x1-s, y1), gui.Pt(x0+s, y1))
	case pm.Cylinder:
		ry := min(b.H/8, b.W/6)
		body := gui.Rect{X: x0, Y: y0 + ry, W: b.W, H: b.H - 2*ry}
		c.FillRect(body, fill)
		c.FillEllipse(gui.Rect{X: x0, Y: y1 - 2*ry, W: b.W, H: 2 * ry}, fill)
		c.FillEllipse(gui.Rect{X: x0, Y: y0, W: b.W, H: 2 * ry}, fill)
		c.StrokePath(ellipse(gui.Rect{X: x0, Y: y0, W: b.W, H: 2 * ry}), n.width, stroke)
		side := new(gui.Path).MoveTo(gui.Pt(x0, y0+ry)).LineTo(gui.Pt(x0, y1-ry))
		side.MoveTo(gui.Pt(x1, y0+ry)).LineTo(gui.Pt(x1, y1-ry))
		c.StrokePath(side, n.width, stroke)
		c.StrokePath(halfEllipse(gui.Rect{X: x0, Y: y1 - 2*ry, W: b.W, H: 2 * ry}), n.width, stroke)
	case pm.Subroutine:
		c.FillRect(b, fill)
		c.StrokeRRect(b, 0, n.width, stroke)
		in := l.th.Font.Size * 0.5
		bars := new(gui.Path).MoveTo(gui.Pt(x0+in, y0)).LineTo(gui.Pt(x0+in, y1))
		bars.MoveTo(gui.Pt(x1-in, y0)).LineTo(gui.Pt(x1-in, y1))
		c.StrokePath(bars, n.width, stroke)
	default: // Rect
		c.FillRect(b, fill)
		c.StrokeRRect(b, 0, n.width, stroke)
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
	pts := e.pts
	head := l.th.Font.Size * 0.7
	// the line stops short of an arrowhead, which is drawn up to the node
	if e.head == pm.Head || e.tail == pm.Head {
		pts = append([]gui.Point(nil), pts...)
		if e.head == pm.Head {
			pts[len(pts)-1] = back(pts[len(pts)-1], pts[len(pts)-2], head*0.8)
		}
		if e.tail == pm.Head {
			pts[0] = back(pts[0], pts[1], head*0.8)
		}
	}
	if e.line == pm.Dotted {
		dashes(c, pts, width, brush, l.th.Font.Size*0.3)
	} else {
		c.StrokePath(rounded(pts, l.th.Font.Size*0.6), width, brush)
	}
	end := func(kind pm.Arrow, tip, from gui.Point) {
		switch kind {
		case pm.Head:
			arrow(c, tip, from, head, brush)
		case pm.Dot:
			r := head * 0.35
			o := back(tip, from, r)
			c.FillEllipse(gui.Rect{X: o.X - r, Y: o.Y - r, W: 2 * r, H: 2 * r}, brush)
		case pm.Cross:
			r := head * 0.35
			o := back(tip, from, r*1.4)
			x := new(gui.Path).MoveTo(gui.Pt(o.X-r, o.Y-r)).LineTo(gui.Pt(o.X+r, o.Y+r))
			x.MoveTo(gui.Pt(o.X-r, o.Y+r)).LineTo(gui.Pt(o.X+r, o.Y-r))
			c.StrokePath(x, width, brush)
		}
	}
	n := len(e.pts)
	end(e.head, e.pts[n-1], e.pts[n-2])
	end(e.tail, e.pts[0], e.pts[1])
}

// back is the point d short of tip, toward from.
func back(tip, from gui.Point, d float32) gui.Point {
	dx, dy := tip.X-from.X, tip.Y-from.Y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return tip
	}
	return gui.Pt(tip.X-dx/l*d, tip.Y-dy/l*d)
}

// arrow fills an arrowhead with its tip at tip, pointing away from from.
func arrow(c gui.Canvas, tip, from gui.Point, size float32, b gui.Brush) {
	dx, dy := tip.X-from.X, tip.Y-from.Y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return
	}
	ux, uy := dx/l, dy/l
	bx, by := tip.X-ux*size, tip.Y-uy*size
	w := size * 0.45
	p := new(gui.Path).MoveTo(tip).LineTo(gui.Pt(bx-uy*w, by+ux*w)).LineTo(gui.Pt(bx+uy*w, by-ux*w)).Close()
	c.FillPath(p, b)
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
