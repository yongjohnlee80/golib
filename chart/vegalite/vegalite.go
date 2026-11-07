// Package vegalite lays out a subset of Vega-Lite charts, with no drawing backend: Lay reads a spec
// and lays it out as shapes and labels, plain values in paint order, and a [Laid] writes itself as
// SVG ([Laid.SVG]) or hands its marks to a canvas that paints them ([Laid.Marks]; gui/vegalite
// paints them in a window). It imports neither gui nor gio, so a build without the GUI (a
// terminal's, a headless server's) draws a chart for an exported page as the window does.
//
// The subset:
//
//   - marks: bar, line, point (and circle), area, and rule (a line across the plot at each value of
//     its one position channel);
//   - data: inline values, an array of objects;
//   - encodings: x and y (quantitative, nominal, ordinal or temporal fields, a measure aggregated
//     by count, sum, mean, median, min or max, or a rule's datum or aggregate), color (a nominal field, or one
//     colour, its scale's range a list of colours), shape (a point's nominal field, or one of Vega's
//     symbols, its range a list of them) and opacity (one value);
//   - sorting: a discrete axis by its values, a list, the other axis, or another field's aggregate;
//   - layers: each a mark of its own over the spec's data and encoding, on shared scales;
//   - drawing: linear, time, band and point scales with their axes and grid, a legend for the
//     colour and one for the shape, the title and its subtitle, and a bar's or an area's series
//     stacked as Vega-Lite stacks them.
//
// A chart is drawn as the page first shows it, a still: a variable param is its initial value (a
// slider bound to it is the page's, not drawn), a selection is empty, and a condition on it is
// resolved so ("empty": false resolves the other way). A datum may name a variable param
// ({"expr": "budget"}); any other expression is unsupported.
//
// Anything else that changes what a chart says (a transform, a log scale, a binned or time-unit
// field, another channel, a test, a selection with an initial value) is [ErrUnsupported], so the
// caller can fall back, as it does for a Mermaid diagram type golib does not draw. A chart is
// drawn at Vega-Lite's proportions, scaled by the theme's font to Vega-Lite's 11-pixel labels, and
// a chart wider than the width asked is scaled down to it.
package vegalite

import (
	"context"
	"image/color"

	pvl "github.com/yongjohnlee80/golib/parse/vegalite"
)

// The subset and its refusals are parse/vegalite's, which reads and plots a spec: a caller of Lay
// matches them here as before.
var (
	// ErrUnsupported is a spec outside the subset drawn; the error names what.
	ErrUnsupported = pvl.ErrUnsupported
	// ErrTooLarge is a spec over a limit: rows, categories or series.
	ErrTooLarge = pvl.ErrTooLarge
)

// SubsetInfo is the subset Lay draws (parse/vegalite's).
type SubsetInfo = pvl.SubsetInfo

// Subset is the subset Lay draws, each list sorted.
func Subset() SubsetInfo { return pvl.Subset() }

// Theme is what a chart is drawn in: its page, its text (titles and labels), its axes' lines, its
// grid, its labels' font, and its series' colours (nil: Vega-Lite's tableau10).
type Theme struct {
	Background, Text, Axis, Grid color.NRGBA
	Font                         Font
	Palette                      []color.NRGBA
}

// tableau10 is Vega-Lite's categorical scheme, its default for a nominal colour.
var tableau10 = []color.NRGBA{
	{0x4c, 0x78, 0xa8, 0xff}, {0xf5, 0x85, 0x18, 0xff}, {0xe4, 0x57, 0x56, 0xff}, {0x72, 0xb7, 0xb2, 0xff},
	{0x54, 0xa2, 0x4b, 0xff}, {0xee, 0xca, 0x3b, 0xff}, {0xb2, 0x79, 0xa2, 0xff}, {0xff, 0x9d, 0xa6, 0xff},
	{0x9d, 0x75, 0x5d, 0xff}, {0xba, 0xb0, 0xac, 0xff},
}

// Lay reads src, a Vega-Lite spec, and lays it out for width (a wider chart is scaled to fit),
// measuring its labels with measure ([ApproxMeasure] when nil). A spec outside the subset is
// ErrUnsupported and one over a limit ErrTooLarge (both for a fallback); malformed JSON is the
// decoder's error.
func Lay(ctx context.Context, src string, width float32, th Theme, measure Measure) (*Laid, error) {
	sp, err := pvl.Parse(src)
	if err != nil {
		return nil, err
	}
	lp, err := pvl.NewLayered(sp)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if th.Font.Size <= 0 {
		th.Font.Size = 11
	}
	if len(th.Palette) == 0 {
		th.Palette = tableau10
	}
	if measure == nil {
		measure = ApproxMeasure
	}
	l := build(ctx, lp, width, th, measure)
	if err := ctx.Err(); err != nil { // abandoned mid-layout: what was laid is not the chart
		return nil, err
	}
	l.fitTo(width)
	return l, nil
}

// Laid is a chart laid out: shapes and labels, plain values, in the order they paint. It holds
// nothing else, so it is safe to share between goroutines.
type Laid struct {
	size  Size    // as drawn, after the fit
	fit   float32 // the scale that fits the chart to the width asked; 1 when it fits
	items []item
}

// itemKind is what an item draws.
type itemKind uint8

const (
	itemRect itemKind = iota // a filled rectangle: a bar, a swatch
	itemFill                 // a filled polygon: an area
	itemLine                 // a stroked polyline: a line, an axis, a tick, a gridline
	itemDot                  // a filled circle in rect
	itemRing                 // a stroked circle in rect
	itemText                 // a label
)

// item is one thing drawn. A label is drawn in rect (measured there), at its left, centre or
// right (align -1, 0, 1); an upright label (up) fills rect turned a quarter left, reading upward
// from its bottom.
type item struct {
	kind  itemKind
	rect  Rect
	pts   []Point
	color color.NRGBA
	width float32
	text  string
	font  Font
	align int8
	up    bool
}

// Size is the chart as drawn: after the fit, when it was scaled down to the width asked.
func (l *Laid) Size() Size { return l.size }

// Fit is the scale that fits the chart to the width Lay was asked for: 1 when it fits, under 1
// when it was scaled down. Every mark is in the chart's own units; a painter scales them by Fit.
func (l *Laid) Fit() float32 { return l.fit }

// fitTo sets the fit that scales a chart wider than width down to it.
func (l *Laid) fitTo(width float32) {
	l.fit = 1
	if width > 0 && l.size.W > width {
		l.fit = width / l.size.W
		l.size = Size{W: width, H: l.size.H * l.fit}
	}
}

// MarkKind is what a [Mark] draws.
type MarkKind uint8

const (
	MarkRect MarkKind = MarkKind(itemRect) // Rect filled: a bar, a swatch
	MarkFill MarkKind = MarkKind(itemFill) // the polygon through Points filled: an area
	MarkLine MarkKind = MarkKind(itemLine) // the polyline through Points stroked Width wide
	MarkDot  MarkKind = MarkKind(itemDot)  // the circle in Rect filled
	MarkRing MarkKind = MarkKind(itemRing) // the circle in Rect stroked Width wide
	MarkText MarkKind = MarkKind(itemText) // Text in Font, placed in Rect by Align and Up
)

// Mark is one thing a chart draws, in the chart's own units (before the fit). A label (MarkText)
// is measured for Rect: it sits at Rect's left, centre or right (Align -1, 0, 1), centred
// vertically; an upright one (Up) fills Rect turned a quarter left, reading upward from the
// middle of Rect's bottom.
type Mark struct {
	Kind   MarkKind
	Rect   Rect
	Points []Point
	Color  color.NRGBA
	Width  float32
	Text   string
	Font   Font
	Align  int8
	Up     bool
}

// Marks are the chart's marks in the order they paint. Points are the chart's own: a painter
// reads them and never changes them.
func (l *Laid) Marks() []Mark {
	out := make([]Mark, len(l.items))
	for i, it := range l.items {
		out[i] = Mark{Kind: MarkKind(it.kind), Rect: it.rect, Points: it.pts, Color: it.color, Width: it.width,
			Text: it.text, Font: it.font, Align: it.align, Up: it.up}
	}
	return out
}
