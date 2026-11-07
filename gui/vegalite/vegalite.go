// Package vegalite draws a subset of Vega-Lite charts natively on a [gui.Canvas], as gui/mermaid
// draws Mermaid diagrams: Lay reads a spec and lays it out, off the loop, and the [Laid] it gives
// paints on the loop. The layout is chart/vegalite's, which needs no drawing backend (a build
// without the GUI writes the same chart as SVG with it); this package measures its labels with
// the window's shaper and paints its marks. Its package doc lists the subset drawn: anything
// outside it is [ErrUnsupported], so the caller can fall back.
package vegalite

import (
	"context"
	"image/color"
	"math"

	"github.com/yongjohnlee80/gio/f32"

	cv "github.com/yongjohnlee80/golib/chart/vegalite"
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/picture"
)

// The subset and its refusals are chart/vegalite's (parse/vegalite's): a caller of Lay matches
// them here as before.
var (
	// ErrUnsupported is a spec outside the subset drawn; the error names what.
	ErrUnsupported = cv.ErrUnsupported
	// ErrTooLarge is a spec over a limit: rows, categories or series.
	ErrTooLarge = cv.ErrTooLarge
)

// SubsetInfo is the subset Lay draws.
type SubsetInfo = cv.SubsetInfo

// Subset is the subset Lay draws, each list sorted.
func Subset() SubsetInfo { return cv.Subset() }

// Theme is what a chart is drawn in: its page, its text (titles and labels), its axes' lines, its
// grid, its labels' font, and its series' colours (nil: Vega-Lite's tableau10).
type Theme struct {
	Background, Text, Axis, Grid color.NRGBA
	Font                         gui.Font
	Palette                      []color.NRGBA
}

// Lay reads src, a Vega-Lite spec, and lays it out for width (a wider chart is scaled to fit),
// measuring with m. A spec outside the subset is ErrUnsupported and one over a limit ErrTooLarge
// (both for a fallback); malformed JSON is the decoder's error.
func Lay(ctx context.Context, src string, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	measure := func(s string, f cv.Font) float32 { return m.Measure(s, gui.Font(f)).Width() }
	c, err := cv.Lay(ctx, src, width, cv.Theme{
		Background: th.Background, Text: th.Text, Axis: th.Axis, Grid: th.Grid,
		Font: cv.Font(th.Font), Palette: th.Palette,
	}, measure)
	if err != nil {
		return nil, err
	}
	return &Laid{chart: c, ops: batch(c.Marks())}, nil
}

// Laid is a chart laid out (chart/vegalite's) and the draw calls its marks batch into. It is safe
// to share between widgets; only Paint, on the loop, keeps anything (the labels it shapes).
type Laid struct {
	chart  *cv.Laid
	ops    []paintOp
	labels picture.Labels
}

// Size is the chart as Paint draws it.
func (l *Laid) Size() gui.Size { return gui.Size(l.chart.Size()) }

// SVG is the chart as a standalone <svg> element, written without a canvas (chart/vegalite's).
func (l *Laid) SVG() []byte { return l.chart.SVG() }

// paintOp is one draw call: a path filled or stroked (every shape of a run of one colour, and
// width, in it), or a label.
type paintOp struct {
	stroke bool
	path   *gui.Path // nil: the label
	color  color.NRGBA
	width  float32
	label  cv.Mark
}

// batch makes marks' draw calls: a run of shapes filled, or stroked, in one colour (and width) is
// one path, so a thousand points cost one call, not a thousand. Order is kept: only neighbours
// join.
func batch(marks []cv.Mark) []paintOp {
	var ops []paintOp
	for _, it := range marks {
		if it.Kind == cv.MarkText {
			ops = append(ops, paintOp{label: it})
			continue
		}
		stroke := it.Kind == cv.MarkLine || it.Kind == cv.MarkRing
		var op *paintOp
		if n := len(ops); n > 0 && ops[n-1].path != nil && ops[n-1].stroke == stroke && ops[n-1].color == it.Color && ops[n-1].width == it.Width {
			op = &ops[n-1]
		} else {
			ops = append(ops, paintOp{stroke: stroke, path: new(gui.Path), color: it.Color, width: it.Width})
			op = &ops[len(ops)-1]
		}
		switch it.Kind {
		case cv.MarkRect:
			r := it.Rect
			op.path.Polyline([]gui.Point{{X: r.X, Y: r.Y}, {X: r.X + r.W, Y: r.Y}, {X: r.X + r.W, Y: r.Y + r.H}, {X: r.X, Y: r.Y + r.H}}, true)
		case cv.MarkFill:
			op.path.Polyline(points(it.Points), true)
		case cv.MarkLine:
			op.path.Polyline(points(it.Points), false)
		case cv.MarkDot, cv.MarkRing:
			op.path.Ellipse(gui.Rect(it.Rect))
		}
	}
	return ops
}

// points are a mark's points as gui's.
func points(pts []cv.Point) []gui.Point {
	out := make([]gui.Point, len(pts))
	for i, p := range pts {
		out[i] = gui.Point(p)
	}
	return out
}

// Paint draws the chart on c with its top left at (0, 0).
func (l *Laid) Paint(c gui.Canvas) {
	defer picture.Push(c, l.chart.Fit())()
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
func (l *Laid) text(c gui.Canvas, it cv.Mark) {
	t := l.labels.Shape(c, it.Text, gui.Font(it.Font))
	r := it.Rect
	if it.Up {
		at := gui.Pt(r.X+r.W/2, r.Y+r.H) // the box's bottom middle: where the text starts, upright
		c.PushTransform(f32.Affine2D{}.Rotate(f32.Pt(at.X, at.Y), -math.Pi/2))
		c.DrawText(t, gui.Pt(at.X, at.Y-t.Height/2), gui.Solid(it.Color))
		c.Pop()
		return
	}
	x := r.X
	switch it.Align {
	case 0:
		x += (r.W - t.Width) / 2
	case 1:
		x += r.W - t.Width
	}
	c.DrawText(t, gui.Pt(x, r.Y+(r.H-t.Height)/2), gui.Solid(it.Color))
}
