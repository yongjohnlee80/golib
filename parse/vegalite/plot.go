package vegalite

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/parse/csscolor"
)

// THE PLOT: a spec read (Parse) and plotted (NewLayered) as a renderer reads it, every layer's data
// on its channels after aggregate, stack and sort, on the shared scales. gui/vegalite lays this out
// in a window; tui/vegalite in cells.

// Plot reads src, a Vega-Lite spec, and plots it. A spec outside the subset is ErrUnsupported
// (wrapped with what), one over a limit ErrTooLarge; malformed JSON is the decoder's error.
func Plot(ctx context.Context, src string) (*Chart, error) {
	sp, err := Parse(src)
	if err != nil {
		return nil, err
	}
	l, err := NewLayered(sp)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return l.Chart(), nil
}

// Chart is a spec as plotted.
type Chart struct {
	Title, Subtitle string
	Width, Height   float32 // in Vega-Lite's pixels; 0: the default
	ContainerWidth  bool    // width: "container": the room the chart is given
	X, Y            Axis    // shared by every layer
	// Series is the colour's domain, in order: a series is one colour in every layer; nil: one
	// series. SeriesColors is the colour scale's range from the spec, in Series' order, cycling;
	// nil: the renderer's palette.
	Series       []string
	SeriesColors []csscolor.Color
	// Shapes is the shape field's domain, in order; nil: none. ShapeRange is the shape scale's
	// range, Vega's symbol names in Shapes' order, cycling; nil: Vega's.
	Shapes      []string
	ShapeRange  []string
	ColorLegend Legend  // the first layer coloured by a field's; Off with no such layer
	ShapeLegend Legend  // the first layer shaped by a field's; Off with none, or when it is the colour's
	Layers      []Layer // in paint order
}

// Legend is a colour's or a shape's legend.
type Legend struct {
	Title string
	Off   bool // none: legend: null, or nothing to show
}

// Axis is a position channel's scale and guide, shared by every layer.
type Axis struct {
	Title    string // resolved: the channel's own, else Vega-Lite's ("Sum of x"), layers' joined
	Off      bool   // axis: null
	Grid     bool   // resolved: a continuous axis has one by default
	Discrete bool   // a band or a point scale
	Band     bool   // a band scale: a bar's categories
	Domain   []string
	// Lo and Hi are a continuous axis's extent: the data's, from zero for a measure that starts
	// there, or the spec's domain. Nice: the renderer rounds them to its ticks (NiceDomain).
	Lo, Hi   float64
	Nice     bool
	Temporal bool // Lo, Hi and every Pos.Num on the axis are Unix milliseconds
	Percent  bool // a normalized stack: 0 to 1, labelled as percents (FormatPercent)
}

// Layer is one mark layer over the chart's scales, or a rule (Rule).
type Layer struct {
	Mark        MarkKind
	Point       bool            // a line's points drawn too
	Filled      bool            // a point filled, else a ring
	Color       *csscolor.Color // every mark this colour; nil: by series
	Opacity     float32         // the opacity given outright; 0: the mark's default (Faded: 0.7, else 1)
	Faded       bool            // its default opacity is 0.7: points one per row, bars or areas overlaid
	StrokeWidth float32         // 0: the default
	Size        float32         // a point's area, in square pixels; 0: the default
	Dash        []float32       // strokeDash: on and off lengths; nil: solid
	Shape       string          // shape {"value"}: one symbol for every point; "": by Datum.Shape
	Horizontal  bool            // the dimension is y: a horizontal bar
	Stacked     bool
	Normalized  bool // stacked to 1 a category (Axis.Percent on the measure's axis)
	// Data is the layer's marks, in the dimension's order (its axis's Domain on a discrete one, Num
	// ascending on a continuous one: x, or y for a horizontal bar), each one's series in Series'
	// order: so a line's or an area's points are in x order within each series. A datum outside a
	// discrete axis's Domain, or of no series of Series, is left out.
	Data []Datum
	Rule *Rule // MarkRule only, and then Data is nil
}

// Rule is a rule layer: a line across the plot at each value on one axis.
type Rule struct {
	OnX    bool // vertical lines at x values; else horizontal at y values
	Values []Pos
}

// Chart is l as a renderer reads it.
func (l *Layered) Chart() *Chart {
	sp, base := l.Spec, l.Base
	c := &Chart{Title: sp.Title, Subtitle: sp.Subtitle, Width: sp.Width, Height: sp.Height, ContainerWidth: sp.ContainerW,
		Series: l.Series, SeriesColors: l.Colors, Shapes: l.Shapes, ShapeRange: l.Symbols}
	c.X = l.axis(&l.Xs, base.Layer.X, func(s *LayerSpec) *Channel { return s.X }, base.Normalize && base.DimY)
	c.Y = l.axis(&l.Ys, base.Layer.Y, func(s *LayerSpec) *Channel { return s.Y }, base.Normalize && !base.DimY)
	c.ColorLegend, c.ShapeLegend = Legend{Off: true}, Legend{Off: true}
	var colorOf, shapeOf *Plan
	for _, o := range l.Order {
		if q, ok := o.AxisLayer(); ok && colorOf == nil && q.Layer.Color != nil && q.Layer.Color.Value == nil {
			colorOf = q
		}
		if q, ok := o.AxisLayer(); ok && shapeOf == nil && q.Layer.Shape != nil && q.Layer.Shape.Shape == "" {
			shapeOf = q
		}
	}
	if colorOf != nil && len(l.Series) > 0 {
		c.ColorLegend = Legend{Title: AxisTitle(colorOf.Layer.Color), Off: colorOf.Layer.Color.NoAxis}
	}
	if shapeOf != nil && len(l.Shapes) > 0 { // a shape by the colour's field shares its legend
		shared := !c.ColorLegend.Off && shapeOf == colorOf && shapeOf.Layer.Shape.Field == colorOf.Layer.Color.Field
		c.ShapeLegend = Legend{Title: AxisTitle(shapeOf.Layer.Shape), Off: shapeOf.Layer.Shape.NoAxis || shared}
	}
	for _, o := range l.Order {
		switch o := o.(type) {
		case *Plan:
			c.Layers = append(c.Layers, l.layer(o, c))
		case *RulePlan:
			ly := markOf(o.Layer)
			ly.Rule = &Rule{OnX: o.OnX, Values: slices.Clone(o.Vals)}
			c.Layers = append(c.Layers, ly)
		}
	}
	return c
}

// axis is a shared scale with its guide, read off the base layer's channel (base) and every mark
// layer's (of): its title, joined as Vega-Lite merges them.
func (l *Layered) axis(a *AxisPlan, base *Channel, of func(*LayerSpec) *Channel, percent bool) Axis {
	return Axis{Title: l.SharedTitle(of), Off: base.NoAxis, Grid: GridOn(base, a), Discrete: a.Discrete, Band: a.Band,
		Domain: slices.Clone(a.Domain), Lo: a.Lo, Hi: a.Hi, Nice: a.Nice, Temporal: a.Temporal, Percent: percent}
}

// markOf is a layer's mark as its spec gives it, before its data.
func markOf(s *LayerSpec) Layer {
	m := s.Mark
	ly := Layer{Mark: m.Kind, Point: m.Point, Filled: m.Filled, Size: m.Size, Dash: slices.Clone(m.Dash), StrokeWidth: m.StrokeWidth}
	if col, ok := s.FixedColor(); ok {
		ly.Color = &col
	}
	if a, ok := s.FixedOpacity(); ok {
		ly.Opacity = a
	}
	if s.Shape != nil {
		ly.Shape = s.Shape.Shape
	}
	return ly
}

// layer is a mark layer as plotted: its data in the dimension's order, then Series' order.
func (l *Layered) layer(p *Plan, c *Chart) Layer {
	ly := markOf(p.Layer)
	ly.Faded, ly.Horizontal, ly.Stacked, ly.Normalized = p.Faded(), p.DimY, p.Stacked, p.Normalize
	dimAxis, dim := c.X, func(d Datum) Pos { return d.X }
	if p.DimY {
		dimAxis, dim = c.Y, func(d Datum) Pos { return d.Y }
	}
	series := map[string]int{"": 0}
	for i, s := range c.Series {
		series[s] = i
	}
	at := func(d Datum) (float64, bool) {
		if !dimAxis.Discrete {
			return dim(d).Num, true
		}
		i := slices.Index(dimAxis.Domain, dim(d).Str)
		return float64(i), i >= 0
	}
	for _, d := range p.Data {
		_, inDomain := at(d)
		_, inSeries := series[d.Series]
		if inDomain && inSeries {
			ly.Data = append(ly.Data, d)
		}
	}
	sort.SliceStable(ly.Data, func(i, j int) bool {
		a, _ := at(ly.Data[i])
		b, _ := at(ly.Data[j])
		if a != b {
			return a < b
		}
		return series[ly.Data[i].Series] < series[ly.Data[j].Series]
	})
	return ly
}

// AxisTitle is a channel's title: its own, or its field's, as Vega-Lite words an aggregate's
// ("Sum of x"); "" for title: null.
func AxisTitle(ch *Channel) string {
	switch {
	case ch.NoTitle:
		return ""
	case ch.Title != nil:
		return *ch.Title
	case ch.Aggregate == AggCount:
		return "Count of Records"
	case ch.Aggregate != AggNone:
		return map[Aggregation]string{AggSum: "Sum", AggMean: "Mean", AggMedian: "Median", AggMin: "Min", AggMax: "Max"}[ch.Aggregate] + " of " + ch.Field
	}
	return ch.Field
}

// SharedTitle is a shared axis's title: the base layer's own, or, as Vega-Lite merges them, every
// mark layer's default title on it, each once, joined ("n, t"). of picks the axis's channel.
func (l *Layered) SharedTitle(of func(*LayerSpec) *Channel) string {
	base := of(l.Base.Layer)
	if base.Title != nil || base.NoTitle {
		return AxisTitle(base)
	}
	var titles []string
	for _, o := range l.Order {
		if q, ok := o.AxisLayer(); ok {
			if t := AxisTitle(of(q.Layer)); t != "" && !slices.Contains(titles, t) {
				titles = append(titles, t)
			}
		}
	}
	return strings.Join(titles, ", ")
}

// GridOn is whether a channel's axis draws its grid: a continuous one's by default.
func GridOn(ch *Channel, a *AxisPlan) bool {
	if ch.NoAxis {
		return false
	}
	if ch.Grid != nil {
		return *ch.Grid
	}
	return !a.Discrete
}
