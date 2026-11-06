package vegalite

import (
	"math"
	"slices"
)

// MARKS — what each mark kind checks and scales, defined once and registered in marks: adding a
// mark is one definition and its registration, not an edit to every switch over the kinds.
// markTypes names the Vega-Lite types that draw each kind. How a kind is drawn is the drawing
// package's (gui/vegalite's, tui/vegalite's).

// markDef is how a mark kind is read and scaled.
type markDef struct {
	// layer reads a chart of the kind into its own layer; nil reads it as a mark layer (newPlan).
	layer func(c *LayerSpec) (LayerPlan, error)
	// check refuses what the kind cannot draw, after newPlan's checks every mark layer takes;
	// nil: nothing more.
	check  func(p *Plan) error
	band   bool // a discrete axis is a band its marks fill
	stacks bool // split into series, the measure piles by default (Vega-Lite's stack)
	shapes bool // a shape channel picks the marks' symbols
	// faded reports whether the layer's default opacity is 0.7 rather than 1; nil: never.
	faded func(p *Plan) bool
}

// marks are the mark kinds' definitions, each registered by its own init below.
var marks = map[MarkKind]markDef{}

func init() {
	marks[MarkBar] = markDef{check: checkBar, band: true, stacks: true, faded: overlaid}
}

func init() {
	marks[MarkLine] = markDef{check: checkLine}
}

func init() {
	marks[MarkArea] = markDef{check: checkLine, stacks: true, faded: overlaid}
}

func init() {
	marks[MarkPoint] = markDef{shapes: true, faded: unaggregated}
}

func init() {
	marks[MarkRule] = markDef{layer: func(c *LayerSpec) (LayerPlan, error) {
		r, err := NewRule(c)
		if err != nil {
			return nil, err // not the typed nil *rulePlan
		}
		return r, nil
	}}
}

// LayerPlan is one of a layered spec's layers, in paint order: a mark layer (*Plan) or a rule
// (*RulePlan).
type LayerPlan interface {
	// AxisLayer is the layer as a mark layer, whose channels the axes' titles and the legends
	// read; false for a rule.
	AxisLayer() (*Plan, bool)
	// Widen widens the shared axes by the layer's own values, after every mark layer's scales
	// are merged (newLayered).
	Widen(l *Layered) error
}

func (p *Plan) AxisLayer() (*Plan, bool) { return p, true }

// Widen does nothing: a mark layer's scales are merged before any layer widens them.
func (p *Plan) Widen(*Layered) error { return nil }

func (r *RulePlan) AxisLayer() (*Plan, bool) { return nil, false }

// Widen adds the rule's values to the axis it is on: categories after the mark layers', or the
// extent, unless the spec gives the domain.
func (r *RulePlan) Widen(l *Layered) error {
	a, name := &l.Ys, "y"
	if r.OnX {
		a, name = &l.Xs, "x"
	}
	if r.Channel.Type.Discrete() != a.Discrete || (r.Channel.Type == TypeTemporal) != a.Temporal {
		return Unsupported("a rule of %v on a %s axis of another type", r.Channel.Type, name)
	}
	for _, v := range r.Vals {
		switch {
		case a.Discrete:
			if !slices.Contains(a.Domain, v.Str) {
				a.Domain = append(a.Domain, v.Str)
			}
		case len(a.Channel.Domain) == 0: // an explicit domain stays as the spec says
			a.Lo, a.Hi = math.Min(a.Lo, v.Num), math.Max(a.Hi, v.Num)
		}
	}
	return nil
}

// checkBar takes a bar of a discrete dimension and a quantitative measure, either way round.
func checkBar(p *Plan) error {
	x, y := p.Layer.X, p.Layer.Y
	switch {
	case x.Type.Discrete() && y.Type == TypeQuantitative:
	case y.Type.Discrete() && x.Type == TypeQuantitative:
		p.DimY = true
	default:
		return Unsupported("a bar of %v x and %v y", x.Type, y.Type)
	}
	return nil
}

// checkLine takes a line or an area of a quantitative y over an x it does not aggregate.
func checkLine(p *Plan) error {
	c := p.Layer
	if c.Y.Type != TypeQuantitative {
		return Unsupported("a %s with a %v y", c.Mark.Kind, c.Y.Type)
	}
	if c.X.Aggregate != AggNone {
		return Unsupported("a %s with x aggregated", c.Mark.Kind)
	}
	return nil
}

// overlaid is a bar's or an area's fade: series drawn over each other, not piled.
func overlaid(p *Plan) bool { return !p.Stacked && len(p.Series) > 1 }

// unaggregated is a point's fade: one per row, not a summary.
func unaggregated(p *Plan) bool { return p.Value().Aggregate == AggNone }

// Faded reports whether the layer's default opacity is 0.7 rather than 1: points one per row,
// and bars or areas overlaid by series.
func (p *Plan) Faded() bool {
	f := marks[p.Layer.Mark.Kind].faded
	return f != nil && f(p)
}
