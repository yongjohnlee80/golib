package vegalite

import (
	"image/color"
	"math"
	"slices"

	"github.com/yongjohnlee80/golib/gui"
)

// MARKS — what each mark kind checks, scales and draws, defined once and registered in marks:
// adding a mark is one definition and its registration, not an edit to every switch over the
// kinds. markTypes names the Vega-Lite types that draw each kind.

// markDef is how a mark kind is read, scaled and drawn.
type markDef struct {
	// layer reads a chart of the kind into its own layer; nil reads it as a mark layer (newPlan).
	layer func(c *chart) (layer, error)
	// check refuses what the kind cannot draw, after newPlan's checks every mark layer takes;
	// nil: nothing more.
	check  func(p *plan) error
	band   bool // a discrete axis is a band its marks fill
	stacks bool // split into series, the measure piles by default (Vega-Lite's stack)
	shapes bool // a shape channel picks the marks' symbols
	// faded reports whether the layer's default opacity is 0.7 rather than 1; nil: never.
	faded func(p *plan) bool
	draw  func(b *builder, xs, ys *scale, alpha float32)
	// swatch draws a legend entry's swatch for series in col, sw wide, at x and centred on cy; nil:
	// a square.
	swatch func(b *builder, series string, x, cy, sw float32, col color.NRGBA)
}

// marks are the mark kinds' definitions, each registered by its own init below.
var marks = map[markKind]markDef{}

func init() {
	marks[markBar] = markDef{check: checkBar, band: true, stacks: true, faded: overlaid, draw: (*builder).bars}
}

func init() {
	marks[markLine] = markDef{check: checkLine, swatch: lineSwatch,
		draw: func(b *builder, xs, ys *scale, alpha float32) { b.lines(xs, ys, alpha, false) }}
}

func init() {
	marks[markArea] = markDef{check: checkLine, stacks: true, faded: overlaid,
		draw: func(b *builder, xs, ys *scale, alpha float32) { b.lines(xs, ys, alpha, true) }}
}

func init() {
	marks[markPoint] = markDef{shapes: true, faded: unaggregated, draw: (*builder).points, swatch: pointSwatch}
}

func init() {
	marks[markRule] = markDef{layer: func(c *chart) (layer, error) {
		r, err := newRule(c)
		if err != nil {
			return nil, err // not the typed nil *rulePlan
		}
		return r, nil
	}}
}

// layer is one of a layered spec's layers, in paint order: a mark layer (*plan) or a rule
// (*rulePlan).
type layer interface {
	// axisLayer is the layer as a mark layer, whose channels the axes' titles and the legends
	// read; false for a rule.
	axisLayer() (*plan, bool)
	// widen widens the shared axes by the layer's own values, after every mark layer's scales
	// are merged (newLayered).
	widen(l *layered) error
	// paint draws the layer on the plot.
	paint(b *builder, xs, ys *scale, plot gui.Rect)
}

func (p *plan) axisLayer() (*plan, bool) { return p, true }

// widen does nothing: a mark layer's scales are merged before any layer widens them.
func (p *plan) widen(*layered) error { return nil }

func (p *plan) paint(b *builder, xs, ys *scale, _ gui.Rect) {
	b.p = p
	marks[p.c.mark.kind].draw(b, xs, ys, b.opacity())
}

func (r *rulePlan) axisLayer() (*plan, bool) { return nil, false }

// widen adds the rule's values to the axis it is on: categories after the mark layers', or the
// extent, unless the spec gives the domain.
func (r *rulePlan) widen(l *layered) error {
	a, name := &l.ys, "y"
	if r.onX {
		a, name = &l.xs, "x"
	}
	if r.ch.typ.discrete() != a.discrete || (r.ch.typ == typeTemporal) != a.temporal {
		return unsupported("a rule of %v on a %s axis of another type", r.ch.typ, name)
	}
	for _, v := range r.vals {
		switch {
		case a.discrete:
			if !slices.Contains(a.domain, v.str) {
				a.domain = append(a.domain, v.str)
			}
		case len(a.ch.domain) == 0: // an explicit domain stays as the spec says
			a.lo, a.hi = math.Min(a.lo, v.num), math.Max(a.hi, v.num)
		}
	}
	return nil
}

func (r *rulePlan) paint(b *builder, xs, ys *scale, plot gui.Rect) { b.rule(r, xs, ys, plot) }

// checkBar takes a bar of a discrete dimension and a quantitative measure, either way round.
func checkBar(p *plan) error {
	x, y := p.c.x, p.c.y
	switch {
	case x.typ.discrete() && y.typ == typeQuantitative:
	case y.typ.discrete() && x.typ == typeQuantitative:
		p.dimY = true
	default:
		return unsupported("a bar of %v x and %v y", x.typ, y.typ)
	}
	return nil
}

// checkLine takes a line or an area of a quantitative y over an x it does not aggregate.
func checkLine(p *plan) error {
	c := p.c
	if c.y.typ != typeQuantitative {
		return unsupported("a %s with a %v y", c.mark.kind, c.y.typ)
	}
	if c.x.aggregate != "" {
		return unsupported("a %s with x aggregated", c.mark.kind)
	}
	return nil
}

// overlaid is a bar's or an area's fade: series drawn over each other, not piled.
func overlaid(p *plan) bool { return !p.stacked && len(p.series) > 1 }

// unaggregated is a point's fade: one per row, not a summary.
func unaggregated(p *plan) bool { return p.value().aggregate == "" }

// bars draws a bar per datum: from its pile's base to its top, or from zero (the domain's end
// nearer it) when the bars are not piled.
func (b *builder) bars(xs, ys *scale, alpha float32) {
	p := b.p
	dimS, valS := xs, ys
	if p.dimY {
		dimS, valS = ys, xs
	}
	for _, d := range p.data {
		dv := d.x
		if p.dimY {
			dv = d.y
		}
		at := dimS.at(dv.str)
		v0, v1 := valS.px(d.v0), valS.px(d.v1)
		if !p.stacked {
			v0 = valS.px(math.Min(math.Max(0, valS.a.lo), valS.a.hi)) // from zero, or the domain's end nearer it
		}
		r := gui.Rect{X: at, Y: min(v0, v1), W: dimS.band, H: float32(math.Abs(float64(v1 - v0)))}
		if p.dimY {
			r = gui.Rect{X: min(v0, v1), Y: at, W: float32(math.Abs(float64(v1 - v0))), H: dimS.band}
		}
		b.add(item{kind: itemRect, rect: r, color: fade(b.colorOf(d.series), alpha)})
	}
}

// lines draws a line, or an area down to its pile's base (zero when not piled), per series.
func (b *builder) lines(xs, ys *scale, alpha float32, area bool) {
	p := b.p
	for _, s := range b.seriesData(xs) {
		if len(s.data) == 0 {
			continue
		}
		col := fade(b.colorOf(s.name), alpha)
		top := make([]gui.Point, len(s.data))
		for i, d := range s.data {
			top[i] = gui.Pt(xs.centre(d.x), ys.px(d.v1))
		}
		if area {
			pts := slices.Clone(top)
			for i := len(s.data) - 1; i >= 0; i-- {
				v0 := s.data[i].v0
				if !p.stacked {
					v0 = math.Min(math.Max(0, ys.a.lo), ys.a.hi)
				}
				pts = append(pts, gui.Pt(top[i].X, ys.px(v0)))
			}
			b.add(item{kind: itemFill, pts: pts, color: col})
			continue
		}
		width := p.c.strokeWidth(2) * b.unit
		if len(top) > 1 {
			b.stroke(col, width, p.c.mark.dash, top...)
		}
		if p.c.mark.point || len(top) == 1 {
			for _, pt := range top {
				b.dot(pt, b.radius(), col, true)
			}
		}
	}
}

// points draws a symbol per datum, filled for a circle mark.
func (b *builder) points(xs, ys *scale, alpha float32) {
	p := b.p
	for _, d := range p.data {
		b.symbol(gui.Pt(xs.centre(d.x), ys.centre(d.y)), b.symbolOf(d.shape), b.radius(), fade(b.colorOf(d.series), alpha), p.c.mark.filled)
	}
}

// lineSwatch is a line's legend swatch: a stroke.
func lineSwatch(b *builder, _ string, x, cy, sw float32, col color.NRGBA) {
	b.line(col, 2*b.unit, gui.Pt(x, cy), gui.Pt(x+sw, cy))
}

// pointSwatch is a point's legend swatch: a circle, or the layer's constant shape, or the shape
// it shares the colour's field of.
func pointSwatch(b *builder, series string, x, cy, sw float32, col color.NRGBA) {
	sym := "circle"
	if sh := b.p.c.shape; sh != nil && (sh.shape != "" || sh.field == b.p.c.color.field) {
		sym = b.symbolOf(series)
	}
	b.symbol(gui.Pt(x+sw/2, cy), sym, sw/2.5, col, b.p.c.mark.filled)
}
