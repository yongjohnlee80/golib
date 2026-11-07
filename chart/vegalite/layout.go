package vegalite

import (
	"context"
	"image/color"
	"math"
	"slices"
	"sort"

	pvl "github.com/yongjohnlee80/golib/parse/vegalite"
)

// LAYOUT. A chart is its title over its plot, the y axis left of the plot, the x axis under it,
// and the colour's legend to its right. The plot's size is Vega-Lite's: 200 by 200 for a
// continuous scale, 20 a category for a discrete one, or the spec's width and height; a width of
// "container" fills the width asked. Every length is Vega-Lite's times the unit, the theme's font
// over Vega-Lite's 11-pixel labels.

// builder lays a layered plan out. p is the mark layer being drawn: the base layer's for the axes,
// each layer's in turn for the marks.
type builder struct {
	ctx   context.Context // cancelled: stroke adds nothing more, and Lay answers the error
	l     *pvl.Layered
	p     *pvl.Plan
	th    Theme
	m     Measure
	unit  float32
	lh    float32 // a label's line height
	bold  Font
	items []item
}

// scale maps one position channel's values to pixels along the plot: [from, to] on screen.
type scale struct {
	a        *pvl.AxisPlan
	from, to float32
	ticks    []float64
	step     float64 // the ticks' step: a linear scale's, for its labels
	percent  bool    // a normalized measure: labelled in percent
	band     float32 // a band's width (band scale); the step between points (point scale)
	stepPx   float32 // a category's step
	first    float32 // the first category's start (band) or centre (point)
}

// px is v (a continuous value) on the scale, kept within the plot.
func (s *scale) px(v float64) float32 {
	a := s.a
	t := float32((v - a.Lo) / (a.Hi - a.Lo))
	t = min(max(t, 0), 1)
	return s.from + t*(s.to-s.from)
}

// at is category v's position: its band's start, or its point.
func (s *scale) at(v string) float32 {
	i := slices.Index(s.a.Domain, v)
	if s.from > s.to { // y: the first category on top
		return s.to + s.first + float32(i)*s.stepPx
	}
	return s.from + s.first + float32(i)*s.stepPx
}

// centre is a datum's position on the scale: a category's centre, or a value's.
func (s *scale) centre(v pvl.Pos) float32 {
	if s.a.Discrete {
		if s.a.Band {
			return s.at(v.Str) + s.band/2
		}
		return s.at(v.Str)
	}
	return s.px(v.Num)
}

// label is a tick's text.
func (s *scale) label(v float64) string {
	switch {
	case s.a.Temporal:
		return pvl.FormatTime(v)
	case s.percent:
		return pvl.FormatPercent(v, s.step)
	}
	return pvl.FormatNumber(v, s.step)
}

// tickCount is Vega-Lite's: one a 40 pixels of the axis, at least 2.
func tickCount(length, unit float32) int {
	return max(int(math.Ceil(float64(length/(40*unit)))), 2)
}

// setTicks finds a continuous scale's ticks, niced first when the scale is nice.
func (s *scale) setTicks(length, unit float32) {
	a := s.a
	n := tickCount(length, unit)
	if a.Temporal {
		s.ticks = pvl.TimeTicks(a.Lo, a.Hi, n)
		return
	}
	if a.Nice {
		a.Lo, a.Hi = pvl.NiceDomain(a.Lo, a.Hi, n)
	}
	s.ticks, s.step = pvl.Ticks(a.Lo, a.Hi, n)
}

// setCategories places a discrete scale's categories along length: bands with Vega-Lite's padding
// (0.1 inside, 0.05 out), or points half a step in from each end.
func (s *scale) setCategories(length float32) {
	n := float32(max(len(s.a.Domain), 1))
	if s.a.Band {
		const inner, outer = 0.1, 0.05
		s.stepPx = length / (n - inner + 2*outer)
		s.band = s.stepPx * (1 - inner)
		s.first = s.stepPx * outer
		return
	}
	s.stepPx = length / n
	s.band = s.stepPx
	s.first = s.stepPx / 2
}

// textW is s's width in f.
func (b *builder) textW(s string, f Font) float32 {
	if s == "" {
		return 0
	}
	return b.m(s, f)
}

func (b *builder) add(it item) { b.items = append(b.items, it) }

// text adds a label in r.
func (b *builder) text(s string, f Font, r Rect, align int8, up bool, c color.NRGBA) {
	if s != "" {
		b.add(item{kind: itemText, text: s, font: f, rect: r, align: align, up: up, color: c})
	}
}

// line adds a polyline.
func (b *builder) line(c color.NRGBA, width float32, pts ...Point) {
	b.add(item{kind: itemLine, pts: pts, color: c, width: width})
}

// build lays l out for width: the base layer's axes and the first colour layer's legend, over the
// shared scales, and every layer's marks in order.
func build(ctx context.Context, l *pvl.Layered, width float32, th Theme, m Measure) *Laid {
	p := l.Base
	b := &builder{ctx: ctx, l: l, p: p, th: th, m: m, unit: th.Font.Size / 11, lh: th.Font.Size * LineHeight}
	b.bold = th.Font
	b.bold.Bold = true
	u, lh := b.unit, b.lh
	c, sp := p.Layer, l.Spec
	tick, pad := 5*u, 4*u

	xs, ys := &scale{a: &l.Xs}, &scale{a: &l.Ys}
	xs.percent = p.Normalize && p.DimY
	ys.percent = p.Normalize && !p.DimY

	// the legend: its title and entries, to the right of the plot
	var legendW, legendH float32
	var lp *pvl.Plan // the layer the legend is of: the first coloured by a field
	for _, o := range l.Order {
		if q, ok := o.AxisLayer(); ok && q.Layer.Color != nil && q.Layer.Color.Value == nil {
			lp = q
			break
		}
	}
	colorLegend := lp != nil && !lp.Layer.Color.NoAxis && len(l.Series) > 0
	legendTitle := ""
	if colorLegend {
		legendTitle = pvl.AxisTitle(lp.Layer.Color)
		legendW, legendH = b.legendSize(legendTitle, l.Series)
	}
	// the shape's legend, under the colour's
	var sp2 *pvl.Plan // the layer the shape legend is of: the first shaped by a field
	for _, o := range l.Order {
		if q, ok := o.AxisLayer(); ok && q.Layer.Shape != nil && q.Layer.Shape.Shape == "" {
			sp2 = q
			break
		}
	}
	// a shape by the colour's field shares its legend, as Vega-Lite merges them
	shapeLegend := sp2 != nil && !sp2.Layer.Shape.NoAxis && len(l.Shapes) > 0 &&
		!(colorLegend && sp2 == lp && sp2.Layer.Shape.Field == lp.Layer.Color.Field)
	shapeTitle := ""
	var shapeTop float32 // the shape legend's top, below the colour legend's
	if shapeLegend {
		if colorLegend {
			shapeTop = legendH + 2*pad
		}
		shapeTitle = pvl.AxisTitle(sp2.Layer.Shape)
		w, h := b.legendSize(shapeTitle, l.Shapes)
		legendW, legendH = max(legendW, w), shapeTop+h
	}
	legend := colorLegend || shapeLegend

	// the plot's height, then the y axis's labels and their width
	plotH := 200 * u
	switch {
	case sp.Height > 0:
		plotH = sp.Height * u
	case l.Ys.Discrete:
		plotH = 20 * u * float32(max(len(l.Ys.Domain), 1))
	}
	if l.Ys.Discrete {
		ys.setCategories(plotH)
	} else {
		ys.setTicks(plotH, u)
	}
	yAxis := !c.Y.NoAxis
	var yLabelW float32
	yLabels := ys.labels()
	if yAxis {
		for _, s := range yLabels {
			yLabelW = max(yLabelW, b.textW(s, th.Font))
		}
	}
	yTitle := ""
	if yAxis {
		yTitle = b.l.SharedTitle(func(c *pvl.LayerSpec) *pvl.Channel { return c.Y })
	}
	left := pad
	if yAxis {
		left += yLabelW + tick + pad
	}
	if yTitle != "" {
		left += lh + pad
	}

	// the plot's width: Vega-Lite's, the spec's, or what is left of the width asked
	plotW := 200 * u
	switch {
	case sp.ContainerW && width > 0:
		gap := float32(0)
		if legend {
			gap = 4*pad + legendW
		}
		plotW = max(width-left-gap-2*pad, 20*u)
	case sp.Width > 0:
		plotW = sp.Width * u
	case l.Xs.Discrete:
		plotW = 20 * u * float32(max(len(l.Xs.Domain), 1))
	}
	if l.Xs.Discrete {
		xs.setCategories(plotW)
	} else {
		xs.setTicks(plotW, u)
	}
	xAxis := !c.X.NoAxis
	xLabels := xs.labels()
	up := false // a discrete axis's labels upright when any is wider than its category
	var xLabelH, lastHalf float32
	if xAxis {
		xLabelH = lh
		for _, s := range xLabels {
			w := b.textW(s, th.Font)
			if l.Xs.Discrete && w > xs.stepPx-2*u {
				up = true
			}
			xLabelH = max(xLabelH, w)
		}
		if !up {
			xLabelH = lh
		}
		if len(xLabels) > 0 && !l.Xs.Discrete {
			lastHalf = b.textW(xLabels[len(xLabels)-1], th.Font) / 2
		}
	}
	xTitle := ""
	if xAxis {
		xTitle = b.l.SharedTitle(func(c *pvl.LayerSpec) *pvl.Channel { return c.X })
	}

	// the title over it all
	top := lh / 2 // room for the top y label's upper half
	titleFont := b.bold
	titleFont.Size *= 13.0 / 11
	if sp.Title != "" {
		top += titleFont.Size*LineHeight + 2*pad
	}
	if sp.Subtitle != "" {
		top += lh + pad
		if sp.Title == "" {
			top += pad
		}
	}
	plot := Rect{X: left, Y: top, W: plotW, H: plotH}
	xs.from, xs.to = plot.X, plot.X+plot.W
	ys.from, ys.to = plot.Y+plot.H, plot.Y // y grows upward

	bottom := plot.Y + plot.H
	if xAxis {
		bottom += tick + pad + xLabelH
	}
	if xTitle != "" {
		bottom += pad + lh
	}
	right := plot.X + plot.W + max(lastHalf, pad)
	if legend {
		right = plot.X + plot.W + 4*pad + legendW + pad
	}
	size := Size{W: right, H: max(bottom, plot.Y+legendH) + pad}

	subY := pad
	if sp.Title != "" {
		b.text(sp.Title, titleFont, Rect{X: 0, Y: pad, W: size.W, H: titleFont.Size * LineHeight}, 0, false, th.Text)
		subY += titleFont.Size*LineHeight + pad/2
	}
	if sp.Subtitle != "" { // under the title, quieter: Vega-Lite's subtitle
		b.text(sp.Subtitle, th.Font, Rect{X: 0, Y: subY, W: size.W, H: lh}, 0, false, th.Axis)
	}
	b.grid(xs, ys, plot)
	for _, o := range l.Order {
		paint(o, b, xs, ys, plot)
	}
	b.p = p
	if yAxis {
		b.yAxis(ys, yLabels, yLabelW, yTitle, plot)
	}
	if xAxis {
		b.xAxis(xs, xLabels, up, xLabelH, xTitle, plot)
	}
	if colorLegend {
		b.p = lp
		b.legend(legendTitle, Pt(plot.X+plot.W+4*pad, plot.Y))
		b.p = p
	}
	if shapeLegend {
		b.p = sp2
		b.shapeLegend(shapeTitle, Pt(plot.X+plot.W+4*pad, plot.Y+shapeTop))
		b.p = p
	}
	return &Laid{size: size, items: b.items}
}

// labels are a scale's tick labels: its categories, or its ticks' text.
func (s *scale) labels() []string {
	if s.a.Discrete {
		return s.a.Domain
	}
	out := make([]string, len(s.ticks))
	for i, v := range s.ticks {
		out[i] = s.label(v)
	}
	return out
}

// grid draws each axis's gridlines across the plot, under the marks.
func (b *builder) grid(xs, ys *scale, plot Rect) {
	c := b.p.Layer
	if pvl.GridOn(c.X, xs.a) {
		for _, v := range xs.ticks {
			x := xs.px(v)
			b.line(b.th.Grid, b.unit, Pt(x, plot.Y), Pt(x, plot.Y+plot.H))
		}
	}
	if pvl.GridOn(c.Y, ys.a) {
		for _, v := range ys.ticks {
			y := ys.px(v)
			b.line(b.th.Grid, b.unit, Pt(plot.X, y), Pt(plot.X+plot.W, y))
		}
	}
}

// parity is which of labels (laid along an axis from lo to hi, each [lo, hi), in order) are
// drawn: all, or, while two drawn neighbours overlap, every other one, as Vega-Lite's default
// labelOverlap ("parity") thins them. Ticks and gridlines stay.
func parity(spans [][2]float32, gap float32) []bool {
	shown := make([]bool, len(spans))
	for i := range shown {
		shown[i] = true
	}
	for every := 1; every < len(spans); every *= 2 {
		overlap := false
		last := -1
		for i := range spans {
			if !shown[i] {
				continue
			}
			if last >= 0 && spans[i][0] < spans[last][1]+gap {
				overlap = true
				break
			}
			last = i
		}
		if !overlap {
			break
		}
		for i := range shown {
			shown[i] = i%(every*2) == 0
		}
	}
	return shown
}

// yAxis draws the y axis: its line and ticks, its labels right-aligned to the ticks (thinned where
// they would overlap), and its title, upright.
func (b *builder) yAxis(ys *scale, labels []string, labelW float32, title string, plot Rect) {
	th, u, lh := b.th, b.unit, b.lh
	tick, pad := 5*u, 4*u
	at := make([]float32, len(labels))
	spans := make([][2]float32, len(labels))
	for i, s := range labels {
		if ys.a.Discrete {
			at[i] = ys.centre(pvl.Pos{Str: s})
		} else {
			at[i] = ys.px(ys.ticks[i])
		}
		spans[i] = [2]float32{-at[i] - lh/2, -at[i] + lh/2} // bottom to top: the first tick lowest
	}
	if ys.a.Discrete { // a category's labels run top down
		for i := range spans {
			spans[i] = [2]float32{at[i] - lh/2, at[i] + lh/2}
		}
	}
	b.line(th.Axis, u, Pt(plot.X, plot.Y), Pt(plot.X, plot.Y+plot.H))
	for _, y := range at {
		b.line(th.Axis, u, Pt(plot.X-tick, y), Pt(plot.X, y))
	}
	shown := parity(spans, 0)
	for i, s := range labels {
		if shown[i] {
			b.text(s, th.Font, Rect{X: plot.X - tick - pad - labelW, Y: at[i] - lh/2, W: labelW, H: lh}, 1, false, th.Text)
		}
	}
	if title != "" {
		w := b.textW(title, b.bold)
		x := plot.X - tick - pad - labelW - pad - lh
		b.text(title, b.bold, Rect{X: x, Y: plot.Y + plot.H/2 - w/2, W: lh, H: w}, 0, true, th.Text)
	}
}

// xAxis draws the x axis: its line and ticks, its labels (upright, reading up to the tick, when a
// category's are too wide; thinned where they would overlap), and its title.
func (b *builder) xAxis(xs *scale, labels []string, up bool, labelH float32, title string, plot Rect) {
	th, u, lh := b.th, b.unit, b.lh
	tick, pad := 5*u, 4*u
	y0 := plot.Y + plot.H
	at := make([]float32, len(labels))
	widths := make([]float32, len(labels))
	spans := make([][2]float32, len(labels))
	for i, s := range labels {
		if xs.a.Discrete {
			at[i] = xs.centre(pvl.Pos{Str: s})
		} else {
			at[i] = xs.px(xs.ticks[i])
		}
		widths[i] = b.textW(s, th.Font)
		half := widths[i] / 2
		if up {
			half = lh / 2
		}
		spans[i] = [2]float32{at[i] - half, at[i] + half}
	}
	b.line(th.Axis, u, Pt(plot.X, y0), Pt(plot.X+plot.W, y0))
	for _, x := range at {
		b.line(th.Axis, u, Pt(x, y0), Pt(x, y0+tick))
	}
	shown := parity(spans, 2*u)
	for i, s := range labels {
		switch {
		case !shown[i]:
		case up:
			b.text(s, th.Font, Rect{X: at[i] - lh/2, Y: y0 + tick + pad, W: lh, H: widths[i]}, 0, true, th.Text)
		default:
			b.text(s, th.Font, Rect{X: at[i] - widths[i]/2, Y: y0 + tick + pad, W: widths[i], H: lh}, 0, false, th.Text)
		}
	}
	if title != "" {
		b.text(title, b.bold, Rect{X: plot.X, Y: y0 + tick + pad + labelH + pad, W: plot.W, H: lh}, 0, false, th.Text)
	}
}

// colorOf is a series' colour: the mark's, the colour channel's value, or the palette's.
func (b *builder) colorOf(series string) color.NRGBA {
	if col, ok := b.p.Layer.FixedColor(); ok {
		return color.NRGBA(col)
	}
	i := max(slices.Index(b.l.Series, series), 0) // the shared colour domain: a series is one colour in every layer
	palette := b.th.Palette
	if b.l.Colors != nil { // the scale's range, cycling as Vega's ordinal scale does
		palette = make([]color.NRGBA, len(b.l.Colors))
		for i, c := range b.l.Colors {
			palette[i] = color.NRGBA(c)
		}
	}
	return palette[i%len(palette)]
}

// opacity is a mark's: the opacity channel's, the mark's, or Vega-Lite's default (0.7 where the
// kind fades, Plan.Faded: for points, and for bars or areas overlaid by series).
func (b *builder) opacity() float32 {
	p := b.p
	if a, ok := p.Layer.FixedOpacity(); ok {
		return a
	}
	if p.Faded() {
		return 0.7
	}
	return 1
}

// fade is c at opacity a, over the page.
func fade(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(float32(c.A)*a + 0.5)
	return c
}

// radius is a point's: half the side of its bounding box, whose area is its size (Vega-Lite's
// default 30 square pixels), as Vega sizes its symbols.
func (b *builder) radius() float32 {
	area := float32(30)
	if b.p.Layer.Mark.Size > 0 {
		area = b.p.Layer.Mark.Size
	}
	return float32(math.Sqrt(float64(area))) / 2 * b.unit
}

// symbolOf is a point's symbol: the shape channel's one, or its field's value's (the scale's range,
// or Vega's symbols, in the shared domain's order, cycling), or a circle.
func (b *builder) symbolOf(shape string) string {
	sh := b.p.Layer.Shape
	switch {
	case sh == nil:
		return "circle"
	case sh.Shape != "":
		return sh.Shape
	}
	syms := pvl.DefaultShapes
	if b.l.Symbols != nil {
		syms = b.l.Symbols
	}
	return syms[max(slices.Index(b.l.Shapes, shape), 0)%len(syms)]
}

// symbol adds a point drawn as one of Vega's symbols, r half its bounding box's side, filled or
// outlined. A circle is the dot's.
func (b *builder) symbol(at Point, name string, r float32, c color.NRGBA, filled bool) {
	pts := symbolPoints(name, r)
	if pts == nil {
		b.dot(at, r, c, filled)
		return
	}
	for i := range pts {
		pts[i].X += at.X
		pts[i].Y += at.Y
	}
	if filled {
		b.add(item{kind: itemFill, pts: pts, color: c})
		return
	}
	b.add(item{kind: itemLine, pts: append(pts, pts[0]), color: c, width: 2 * b.unit})
}

// symbolPoints is a Vega symbol's outline about the origin, r half its bounding box's side, as
// vega-scenegraph draws it; nil for a circle.
func symbolPoints(name string, r float32) []Point {
	h := float32(math.Sqrt(3)/2) * r // a triangle's half height
	s := r / 2.5                     // a cross's half arm width
	var pts []Point
	switch name {
	case "square":
		pts = []Point{{X: -r, Y: -r}, {X: r, Y: -r}, {X: r, Y: r}, {X: -r, Y: r}}
	case "diamond":
		pts = []Point{{X: -r, Y: 0}, {X: 0, Y: -r}, {X: r, Y: 0}, {X: 0, Y: r}}
	case "triangle-up":
		pts = []Point{{X: 0, Y: -h}, {X: -r, Y: h}, {X: r, Y: h}}
	case "triangle-down":
		pts = []Point{{X: 0, Y: h}, {X: -r, Y: -h}, {X: r, Y: -h}}
	case "triangle-right":
		pts = []Point{{X: h, Y: 0}, {X: -h, Y: -r}, {X: -h, Y: r}}
	case "triangle-left":
		pts = []Point{{X: -h, Y: 0}, {X: h, Y: -r}, {X: h, Y: r}}
	case "cross":
		pts = []Point{{X: -r, Y: -s}, {X: -r, Y: s}, {X: -s, Y: s}, {X: -s, Y: r}, {X: s, Y: r}, {X: s, Y: s},
			{X: r, Y: s}, {X: r, Y: -s}, {X: s, Y: -s}, {X: s, Y: -r}, {X: -s, Y: -r}, {X: -s, Y: -s}}
	}
	return clockwise(pts)
}

// clockwise is pts wound clockwise on the screen, reversed if need be: a run of one colour is one
// path, filled by the non-zero rule, so where two symbols wound opposite ways overlapped, they
// would cancel to a hole.
func clockwise(pts []Point) []Point {
	var area float32
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		area += p.X*q.Y - q.X*p.Y
	}
	if area < 0 {
		slices.Reverse(pts)
	}
	return pts
}

// shapeLegend draws the shape's legend with its top left at at: its title, then each shape's
// symbol, in the axes' colour, and its label.
func (b *builder) shapeLegend(title string, at Point) {
	b.legendEntries(title, b.l.Shapes, at, func(s string, x, cy, sw float32) {
		b.symbol(Pt(x+sw/2, cy), b.symbolOf(s), sw/2.5, b.th.Axis, b.p.Layer.Mark.Filled)
	})
}

// legendSize is the size of a legend: its title (bold) over its entries, each a swatch and a
// label, one line apiece.
func (b *builder) legendSize(title string, entries []string) (w, h float32) {
	lh, pad := b.lh, 4*b.unit
	w = b.textW(title, b.bold)
	for _, s := range entries {
		w = max(w, lh+pad+b.textW(s, b.th.Font))
	}
	h = float32(len(entries)) * lh
	if title != "" {
		h += lh + pad
	}
	return w, h
}

// legendEntries draws a legend with its top left at at, in legendSize's box: its title, then each
// entry's swatch (swatch draws it at x, centred on cy, sw across) and its label.
func (b *builder) legendEntries(title string, entries []string, at Point, swatch func(s string, x, cy, sw float32)) {
	th, lh := b.th, b.lh
	pad := 4 * b.unit
	y := at.Y
	if title != "" {
		b.text(title, b.bold, Rect{X: at.X, Y: y, W: b.textW(title, b.bold), H: lh}, -1, false, th.Text)
		y += lh + pad
	}
	sw := th.Font.Size * 0.8
	for _, s := range entries {
		swatch(s, at.X, y+lh/2, sw)
		b.text(s, th.Font, Rect{X: at.X + lh + pad, Y: y, W: b.textW(s, th.Font), H: lh}, -1, false, th.Text)
		y += lh
	}
}

// dot adds a point: a filled circle, or a ring.
func (b *builder) dot(at Point, r float32, c color.NRGBA, filled bool) {
	box := Rect{X: at.X - r, Y: at.Y - r, W: 2 * r, H: 2 * r}
	if filled {
		b.add(item{kind: itemDot, rect: box, color: c})
		return
	}
	b.add(item{kind: itemRing, rect: box, color: c, width: 2 * b.unit})
}

// series is one series' data, in x's order.
type series struct {
	name string
	data []pvl.Datum
}

// seriesData splits the data by series, in the colour's order, each sorted by x: a line is drawn
// through its points from left to right, as Vega-Lite sorts it.
func (b *builder) seriesData(xs *scale) []series {
	p := b.p
	names := p.Series
	if names == nil {
		names = []string{""}
	}
	out := make([]series, len(names))
	idx := map[string]int{}
	for i, n := range names {
		out[i].name = n
		idx[n] = i
	}
	for _, d := range p.Data {
		if i, ok := idx[d.Series]; ok {
			out[i].data = append(out[i].data, d)
		}
	}
	for i := range out {
		sort.SliceStable(out[i].data, func(a, c int) bool {
			return xs.centre(out[i].data[a].X) < xs.centre(out[i].data[c].X)
		})
	}
	return out
}

// legend draws the colour's legend with its top left at at: its title, then a swatch and a label
// a series. A line's swatch is a stroke, a point's a circle, a bar's or an area's a square.
func (b *builder) legend(title string, at Point) {
	b.legendEntries(title, b.l.Series, at, func(s string, x, cy, sw float32) {
		col := b.colorOf(s)
		if swatch := marks[b.p.Layer.Mark.Kind].swatch; swatch != nil {
			swatch(b, s, x, cy, sw, col)
			return
		}
		b.add(item{kind: itemRect, rect: Rect{X: x, Y: cy - sw/2, W: sw, H: sw}, color: col})
	})
}

// rule draws a rule layer: a line across the plot at each of its values, in its colour (the
// mark's, the colour channel's value, or Vega-Lite's default, the palette's first), width
// (Vega-Lite's 1 by default) and dashes.
func (b *builder) rule(r *pvl.RulePlan, xs, ys *scale, plot Rect) {
	c := r.Layer
	fixed, ok := c.FixedColor()
	col := color.NRGBA(fixed)
	if !ok {
		col = b.th.Palette[0]
	}
	alpha, ok := c.FixedOpacity()
	if !ok {
		alpha = 1
	}
	width := c.StrokeWidth(1) * b.unit
	for _, v := range r.Vals {
		if r.OnX {
			x := xs.centre(v)
			b.stroke(fade(col, alpha), width, c.Mark.Dash, Pt(x, plot.Y), Pt(x, plot.Y+plot.H))
		} else {
			y := ys.centre(v)
			b.stroke(fade(col, alpha), width, c.Mark.Dash, Pt(plot.X, y), Pt(plot.X+plot.W, y))
		}
	}
}

// stroke adds a polyline, dashed by dash (Vega-Lite's strokeDash, on and off lengths in its
// pixels) when it is set.
func (b *builder) stroke(c color.NRGBA, width float32, dash []float32, pts ...Point) {
	if b.ctx.Err() != nil { // abandoned: no more cutting
		return
	}
	if len(dash) == 0 {
		b.line(c, width, pts...)
		return
	}
	pattern := make([]float32, len(dash))
	for i, d := range dash {
		pattern[i] = d * b.unit
	}
	for _, run := range Dash(pts, pattern) {
		b.line(c, width, run...)
	}
}
