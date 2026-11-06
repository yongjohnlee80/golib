package vegalite

import (
	"context"
	"fmt"
	"math"
	"slices"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

// THE BRAILLE TIER — line, point and area charts plotted in dots inside the frame's plot: a line
// joins a series' consecutive points, an area fills each dot column from its line down to its
// baseline (V0: the axis's zero, or the stack's lower edge), a point is its dot (a filled one 2×2),
// and a rule is a whole-cell ─ or │ drawn over the dots. Each series takes its colour (Theme): a
// cell wears the last series that set a dot in it.

// brailleMarks are the marks this tier draws.
func brailleMark(k pv.MarkKind) bool {
	switch k {
	case pv.MarkLine, pv.MarkPoint, pv.MarkArea, pv.MarkRule:
		return true
	}
	return false
}

// layBraille is the Braille tier (Lay tries it after the bars): ch, every layer of it a line, point,
// area or rule, in a room cells wide. It refuses with pv.ErrUnsupported a layer of another mark (a
// bar: the bar tier's) and a room too narrow for a plot (the frame's), so Lay tries the next tier.
func layBraille(ctx context.Context, ch *pv.Chart, m Measurer, room int) (*Drawing, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, l := range ch.Layers {
		if !brailleMark(l.Mark) {
			return nil, fmt.Errorf("%w: a %v layer is not the Braille tier's", pv.ErrUnsupported, l.Mark)
		}
	}
	f, ok := layFrame(ch, room, 0, m)
	if !ok {
		return nil, fmt.Errorf("%w: a room of %d cells leaves no plot", pv.ErrUnsupported, room)
	}
	f.drawBraille(ch.Layers)
	return f.d, nil
}

// drawBraille plots layers into the frame's plot, in paint order.
func (f *frame) drawBraille(layers []pv.Layer) {
	c := newCanvas(f.plot.W, f.plot.H)
	for _, l := range layers {
		switch l.Mark {
		case pv.MarkArea:
			f.areas(c, l)
		case pv.MarkLine:
			f.lines(c, l)
			if l.Point {
				f.points(c, l)
			}
		case pv.MarkPoint:
			f.points(c, l)
		case pv.MarkRule:
			f.rules(c, l)
		}
	}
	for y := range c.h {
		for x := range c.w {
			if r, s := c.at(x, y); r != ' ' {
				f.mark(Cell{X: x, Y: y, Glyph: r, FG: s, BG: -1})
			}
		}
	}
}

// seriesOf is a datum's series index in the chart's colour domain: 0 for a chart of one series.
func (f *frame) seriesOf(d pv.Datum, l pv.Layer) int {
	if l.Color != nil || len(f.ch.Series) == 0 {
		return 0
	}
	if i := slices.Index(f.ch.Series, d.Series); i >= 0 {
		return i
	}
	return 0
}

// dot is a datum's place in dots: x from its position on the x axis, y from its measure's upper
// end (V1) on the y axis; a horizontal layer's measure is on x.
func (f *frame) dot(d pv.Datum, l pv.Layer, v float64) (x, y int) {
	if l.Horizontal {
		return f.measureDotX(v), f.dimDotY(d.Y)
	}
	return f.dimDotX(d.X), f.measureDotY(v)
}

// dimDotX is a position's dot column: a category's band middle, or a value's place on the axis.
func (f *frame) dimDotX(p pv.Pos) int {
	w := float64(2 * f.plot.W)
	if f.ch.X.Discrete {
		return int(math.Floor(f.band(f.ch.X, p.Str, w)))
	}
	return int(math.Round(frac(p.Num, f.ch.X.Lo, f.ch.X.Hi) * (w - 1)))
}

// dimDotY is a position's dot row on the y axis (a horizontal layer's dimension).
func (f *frame) dimDotY(p pv.Pos) int {
	h := float64(4 * f.plot.H)
	if f.ch.Y.Discrete {
		return int(math.Floor(f.band(f.ch.Y, p.Str, h)))
	}
	return int(math.Round((1 - frac(p.Num, f.ch.Y.Lo, f.ch.Y.Hi)) * (h - 1)))
}

// measureDotY is a measure's dot row: the axis's high end at the top.
func (f *frame) measureDotY(v float64) int {
	return int(math.Round((1 - frac(v, f.ch.Y.Lo, f.ch.Y.Hi)) * float64(4*f.plot.H-1)))
}

// measureDotX is a measure's dot column (a horizontal layer's).
func (f *frame) measureDotX(v float64) int {
	return int(math.Round(frac(v, f.ch.X.Lo, f.ch.X.Hi) * float64(2*f.plot.W-1)))
}

// bySeries is a layer's data split by series, each in the order the chart gives it (a line's: x
// order), the series in the colour domain's order.
func (f *frame) bySeries(l pv.Layer) [][]pv.Datum {
	var order []string
	group := map[string][]pv.Datum{}
	for _, d := range l.Data {
		if _, ok := group[d.Series]; !ok {
			order = append(order, d.Series)
		}
		group[d.Series] = append(group[d.Series], d)
	}
	slices.SortStableFunc(order, func(a, b string) int {
		return slices.Index(f.ch.Series, a) - slices.Index(f.ch.Series, b)
	})
	out := make([][]pv.Datum, 0, len(order))
	for _, s := range order {
		out = append(out, group[s])
	}
	return out
}

// lines joins each series' consecutive points.
func (f *frame) lines(c *canvas, l pv.Layer) {
	for _, ds := range f.bySeries(l) {
		for i := range ds {
			s := f.seriesOf(ds[i], l)
			x1, y1 := f.dot(ds[i], l, ds[i].V1)
			if i == 0 {
				c.set(x1, y1, s) // a series of one point still shows
				continue
			}
			x0, y0 := f.dot(ds[i-1], l, ds[i-1].V1)
			c.line(x0, y0, x1, y1, s)
		}
	}
}

// areas fills each series from its line to its baseline, column by column, both edges
// interpolated between consecutive points: a stacked area fills from its lower edge (V0).
func (f *frame) areas(c *canvas, l pv.Layer) {
	for _, ds := range f.bySeries(l) {
		for i := range ds {
			s := f.seriesOf(ds[i], l)
			xa, ta := f.dot(ds[i], l, ds[i].V1)
			_, ba := f.dot(ds[i], l, ds[i].V0)
			if i == 0 {
				c.fillDown(xa, ta, ba, s)
				continue
			}
			xp, tp := f.dot(ds[i-1], l, ds[i-1].V1)
			_, bp := f.dot(ds[i-1], l, ds[i-1].V0)
			if xa == xp {
				c.fillDown(xa, min(ta, tp), max(ba, bp), s)
				continue
			}
			step := sign(xa - xp)
			for x := xp; x != xa+step; x += step {
				t := float64(x-xp) / float64(xa-xp)
				top := int(math.Round(float64(tp) + t*float64(ta-tp)))
				base := int(math.Round(float64(bp) + t*float64(ba-bp)))
				c.fillDown(x, top, base, s)
			}
		}
	}
}

// points sets each datum's dot: a filled point 2×2.
func (f *frame) points(c *canvas, l pv.Layer) {
	for _, d := range l.Data {
		s := f.seriesOf(d, l)
		x, y := f.dot(d, l, d.V1)
		c.set(x, y, s)
		if l.Filled {
			c.set(x+1, y, s)
			c.set(x, y+1, s)
			c.set(x+1, y+1, s)
		}
	}
}

// rules draws each of a rule layer's values as a whole-cell line across the plot: │ at an x value,
// ─ at a y value.
func (f *frame) rules(c *canvas, l pv.Layer) {
	if l.Rule == nil {
		return
	}
	for _, v := range l.Rule.Values {
		if l.Rule.OnX {
			x := f.dimDotX(v) / 2
			for y := range c.h {
				c.rule(x, y, '│', 0)
			}
			continue
		}
		y := f.ruleRow(v)
		for x := range c.w {
			c.rule(x, y, '─', 0)
		}
	}
}

// ruleRow is a y value's row for a rule: its category's band middle, or its value's place.
func (f *frame) ruleRow(v pv.Pos) int {
	if f.ch.Y.Discrete {
		return int(math.Floor(f.band(f.ch.Y, v.Str, float64(f.plot.H))))
	}
	return int(math.Round((1 - frac(v.Num, f.ch.Y.Lo, f.ch.Y.Hi)) * float64(f.plot.H-1)))
}
