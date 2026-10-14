// Package vegalite draws a subset of Vega-Lite charts natively on a [gui.Canvas], as gui/mermaid
// draws Mermaid diagrams: Lay reads a spec and lays it out, off the loop, and the [Laid] it gives
// paints on the loop.
//
// The subset:
//
//   - marks: bar, line, point (and circle), area, and rule (a line across the plot at each value of
//     its one position channel);
//   - data: inline values, an array of objects;
//   - encodings: x and y (quantitative, nominal, ordinal or temporal fields, a measure aggregated
//     by count, sum, mean, median, min or max, or a rule's datum or aggregate), color (a nominal field, or one
//     colour) and opacity (one value);
//   - layers: each a mark of its own over the spec's data and encoding, on shared scales;
//   - drawing: linear, time, band and point scales with their axes and grid, a legend for the
//     colour, the title, and a bar's or an area's series stacked as Vega-Lite stacks them.
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
	"math"
	"sync"

	"gioui.org/f32"

	"github.com/yongjohnlee80/golib/gui"
)

// Theme is what a chart is drawn in: its page, its text (titles and labels), its axes' lines, its
// grid, its labels' font, and its series' colours (nil: Vega-Lite's tableau10).
type Theme struct {
	Background, Text, Axis, Grid color.NRGBA
	Font                         gui.Font
	Palette                      []color.NRGBA
}

// tableau10 is Vega-Lite's categorical scheme, its default for a nominal colour.
var tableau10 = []color.NRGBA{
	{0x4c, 0x78, 0xa8, 0xff}, {0xf5, 0x85, 0x18, 0xff}, {0xe4, 0x57, 0x56, 0xff}, {0x72, 0xb7, 0xb2, 0xff},
	{0x54, 0xa2, 0x4b, 0xff}, {0xee, 0xca, 0x3b, 0xff}, {0xb2, 0x79, 0xa2, 0xff}, {0xff, 0x9d, 0xa6, 0xff},
	{0x9d, 0x75, 0x5d, 0xff}, {0xba, 0xb0, 0xac, 0xff},
}

// Lay reads src, a Vega-Lite spec, and lays it out for width (a wider chart is scaled to fit),
// measuring with m. A spec outside the subset is ErrUnsupported and one over a limit ErrTooLarge
// (both for a fallback); malformed JSON is the decoder's error.
func Lay(ctx context.Context, src string, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	sp, err := parse(src)
	if err != nil {
		return nil, err
	}
	lp, err := newLayered(sp)
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
	l := build(lp, width, th, m)
	l.fitTo(width)
	return l, nil
}

// Laid is a chart laid out: shapes and labels, plain values, in the order they paint, and the
// draw calls they batch into. It is safe to share between widgets; only Paint, on the loop, keeps
// anything (the labels it shapes).
type Laid struct {
	size  gui.Size // as drawn, after the fit
	fit   float32  // the scale that fits the chart to the width asked; 1 when it fits
	items []item
	ops   []paintOp

	mu     sync.Mutex
	shaped map[shapeKey]*gui.TextLayout
}

type shapeKey struct {
	text string
	font gui.Font
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
	rect  gui.Rect
	pts   []gui.Point
	color color.NRGBA
	width float32
	text  string
	font  gui.Font
	align int8
	up    bool
}

// Size is the chart as Paint draws it.
func (l *Laid) Size() gui.Size { return l.size }

// fitTo sets the fit that scales a chart wider than width down to it.
func (l *Laid) fitTo(width float32) {
	l.fit = 1
	if width > 0 && l.size.W > width {
		l.fit = width / l.size.W
		l.size = gui.Size{W: width, H: l.size.H * l.fit}
	}
}

// paintOp is one draw call: a path filled or stroked (every shape of a run of one colour, and
// width, in it), or a label.
type paintOp struct {
	stroke bool
	path   *gui.Path // nil: the label
	color  color.NRGBA
	width  float32
	label  item
}

// batch makes items' draw calls: a run of shapes filled, or stroked, in one colour (and width) is
// one path, so a thousand points cost one call, not a thousand. Order is kept: only neighbours
// join.
func batch(items []item) []paintOp {
	var ops []paintOp
	for _, it := range items {
		if it.kind == itemText {
			ops = append(ops, paintOp{label: it})
			continue
		}
		stroke := it.kind == itemLine || it.kind == itemRing
		var op *paintOp
		if n := len(ops); n > 0 && ops[n-1].path != nil && ops[n-1].stroke == stroke && ops[n-1].color == it.color && ops[n-1].width == it.width {
			op = &ops[n-1]
		} else {
			ops = append(ops, paintOp{stroke: stroke, path: new(gui.Path), color: it.color, width: it.width})
			op = &ops[len(ops)-1]
		}
		switch it.kind {
		case itemRect:
			r := it.rect
			addPoly(op.path, []gui.Point{{X: r.X, Y: r.Y}, {X: r.X + r.W, Y: r.Y}, {X: r.X + r.W, Y: r.Y + r.H}, {X: r.X, Y: r.Y + r.H}}, true)
		case itemFill:
			addPoly(op.path, it.pts, true)
		case itemLine:
			addPoly(op.path, it.pts, false)
		case itemDot, itemRing:
			addEllipse(op.path, it.rect)
		}
	}
	return ops
}

// Paint draws the chart on c with its top left at (0, 0).
func (l *Laid) Paint(c gui.Canvas) {
	if l.fit != 1 {
		c.PushTransform(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(l.fit, l.fit)))
		defer c.Pop()
	}
	for _, op := range l.ops {
		switch {
		case op.path == nil:
			l.text(c, op.label)
		case op.stroke:
			c.StrokePath(op.path, op.width, gui.Solid(op.color))
		default:
			c.FillPath(op.path, gui.Solid(op.color))
		}
	}
}

// text draws a label: shaped once, then placed in its box, or turned upright about its corner.
func (l *Laid) text(c gui.Canvas, it item) {
	t := l.shape(c, it.text, it.font)
	r := it.rect
	if it.up {
		at := gui.Pt(r.X+r.W/2, r.Y+r.H) // the box's bottom middle: where the text starts, upright
		c.PushTransform(f32.Affine2D{}.Rotate(f32.Pt(at.X, at.Y), -math.Pi/2))
		c.DrawText(t, gui.Pt(at.X, at.Y-t.Height/2), gui.Solid(it.color))
		c.Pop()
		return
	}
	x := r.X
	switch it.align {
	case 0:
		x += (r.W - t.Width) / 2
	case 1:
		x += r.W - t.Width
	}
	c.DrawText(t, gui.Pt(x, r.Y+(r.H-t.Height)/2), gui.Solid(it.color))
}

// shape is s in f, shaped on the loop the first time it is drawn.
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

// addPoly adds the polyline through pts to p, as a subpath of its own, closed when close is set.
func addPoly(p *gui.Path, pts []gui.Point, close bool) {
	for i, pt := range pts {
		if i == 0 {
			p.MoveTo(pt)
		} else {
			p.LineTo(pt)
		}
	}
	if close && len(pts) > 0 {
		p.Close()
	}
}

// addEllipse adds the ellipse inscribed in r to p, as four cubic arcs.
func addEllipse(p *gui.Path, r gui.Rect) {
	const k = 0.5523 // a quarter circle's control distance, over its radius
	cx, cy, rx, ry := r.X+r.W/2, r.Y+r.H/2, r.W/2, r.H/2
	p.MoveTo(gui.Pt(cx+rx, cy)).
		CubeTo(gui.Pt(cx+rx, cy+ry*k), gui.Pt(cx+rx*k, cy+ry), gui.Pt(cx, cy+ry)).
		CubeTo(gui.Pt(cx-rx*k, cy+ry), gui.Pt(cx-rx, cy+ry*k), gui.Pt(cx-rx, cy)).
		CubeTo(gui.Pt(cx-rx, cy-ry*k), gui.Pt(cx-rx*k, cy-ry), gui.Pt(cx, cy-ry)).
		CubeTo(gui.Pt(cx+rx*k, cy-ry), gui.Pt(cx+rx, cy-ry*k), gui.Pt(cx+rx, cy)).Close()
}
