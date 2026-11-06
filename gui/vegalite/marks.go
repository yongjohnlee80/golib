package vegalite

import (
	"image/color"
	"math"
	"slices"

	"github.com/yongjohnlee80/golib/gui"
	pvl "github.com/yongjohnlee80/golib/parse/vegalite"
)

// MARKS — how each mark kind is drawn, defined once and registered in marks; what a kind checks
// and scales is parse/vegalite's.

// markDef is how a mark kind is drawn.
type markDef struct {
	draw func(b *builder, xs, ys *scale, alpha float32)
	// swatch draws a legend entry's swatch for series in col, sw wide, at x and centred on cy; nil:
	// a square.
	swatch func(b *builder, series string, x, cy, sw float32, col color.NRGBA)
}

// marks are the mark kinds' drawings, each registered by its own init below. A rule is a layer of
// its own (paint), not a mark layer's drawing.
var marks = map[pvl.MarkKind]markDef{}

func init() { marks[pvl.MarkBar] = markDef{draw: (*builder).bars} }

func init() {
	marks[pvl.MarkLine] = markDef{swatch: lineSwatch,
		draw: func(b *builder, xs, ys *scale, alpha float32) { b.lines(xs, ys, alpha, false) }}
}

func init() {
	marks[pvl.MarkArea] = markDef{draw: func(b *builder, xs, ys *scale, alpha float32) { b.lines(xs, ys, alpha, true) }}
}

func init() { marks[pvl.MarkPoint] = markDef{draw: (*builder).points, swatch: pointSwatch} }

// paint draws layer o on the plot: a mark layer by its kind's drawing, a rule across the plot.
func paint(o pvl.LayerPlan, b *builder, xs, ys *scale, plot gui.Rect) {
	switch o := o.(type) {
	case *pvl.Plan:
		b.p = o
		marks[o.Layer.Mark.Kind].draw(b, xs, ys, b.opacity())
	case *pvl.RulePlan:
		b.rule(o, xs, ys, plot)
	}
}

// bars draws a bar per datum: from its pile's base to its top, or from zero (the domain's end
// nearer it) when the bars are not piled.
func (b *builder) bars(xs, ys *scale, alpha float32) {
	p := b.p
	dimS, valS := xs, ys
	if p.DimY {
		dimS, valS = ys, xs
	}
	for _, d := range p.Data {
		dv := d.X
		if p.DimY {
			dv = d.Y
		}
		at := dimS.at(dv.Str)
		v0, v1 := valS.px(d.V0), valS.px(d.V1)
		if !p.Stacked {
			v0 = valS.px(math.Min(math.Max(0, valS.a.Lo), valS.a.Hi)) // from zero, or the domain's end nearer it
		}
		r := gui.Rect{X: at, Y: min(v0, v1), W: dimS.band, H: float32(math.Abs(float64(v1 - v0)))}
		if p.DimY {
			r = gui.Rect{X: min(v0, v1), Y: at, W: float32(math.Abs(float64(v1 - v0))), H: dimS.band}
		}
		b.add(item{kind: itemRect, rect: r, color: fade(b.colorOf(d.Series), alpha)})
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
			top[i] = gui.Pt(xs.centre(d.X), ys.px(d.V1))
		}
		if area {
			pts := slices.Clone(top)
			for i := len(s.data) - 1; i >= 0; i-- {
				v0 := s.data[i].V0
				if !p.Stacked {
					v0 = math.Min(math.Max(0, ys.a.Lo), ys.a.Hi)
				}
				pts = append(pts, gui.Pt(top[i].X, ys.px(v0)))
			}
			b.add(item{kind: itemFill, pts: pts, color: col})
			continue
		}
		width := p.Layer.StrokeWidth(2) * b.unit
		if len(top) > 1 {
			b.stroke(col, width, p.Layer.Mark.Dash, top...)
		}
		if p.Layer.Mark.Point || len(top) == 1 {
			for _, pt := range top {
				b.dot(pt, b.radius(), col, true)
			}
		}
	}
}

// points draws a symbol per datum, filled for a circle mark.
func (b *builder) points(xs, ys *scale, alpha float32) {
	p := b.p
	for _, d := range p.Data {
		b.symbol(gui.Pt(xs.centre(d.X), ys.centre(d.Y)), b.symbolOf(d.Shape), b.radius(), fade(b.colorOf(d.Series), alpha), p.Layer.Mark.Filled)
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
	if sh := b.p.Layer.Shape; sh != nil && (sh.Shape != "" || sh.Field == b.p.Layer.Color.Field) {
		sym = b.symbolOf(series)
	}
	b.symbol(gui.Pt(x+sw/2, cy), sym, sw/2.5, col, b.p.Layer.Mark.Filled)
}
