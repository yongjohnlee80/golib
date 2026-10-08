// Package mermaid draws Mermaid diagrams natively on a gui Canvas: parse/mermaid's models, laid
// out by graph/layout or by a diagram's own geometry, in a theme's colours, as vectors (ADR
// 1791430025).
//
// Lay runs on a worker: it parses, measures every label with a Measurer the worker holds (never
// the Canvas's shaper, which only the UI loop may use) and lays the diagram out. Its result is a
// Laid: plain geometry, label strings and fonts. Paint draws a Laid on the loop, shaping its labels
// there. A diagram type with no layer here is mermaid.ErrUnsupported, for a caller's fallback to
// render.
//
// Each diagram type is a layer: a function from its model to a Laid, registered in layers by the
// file that draws it. A Laid is the same few primitives for every type: boxes (a form, a title,
// compartments), edges (a polyline, its ends, its labels), groups (a frame, a title, dividers)
// and free text.
package mermaid

import (
	"context"
	"fmt"
	"image/color"
	"sync"

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

// layer lays one diagram type out. d is that type's model.
type layer func(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error)

// layers are the diagram types drawn, by kind; each type's file registers its own.
var layers = map[pm.Kind]layer{}

// Lay parses src and lays it out for width (a wider diagram is scaled to fit), measuring with m.
// A diagram golib does not draw is mermaid.ErrUnsupported, and one over a limit
// mermaid.ErrTooLarge (both for a fallback); a malformed one is a *mermaid.SyntaxError.
func Lay(ctx context.Context, src string, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	d, err := pm.Parse(src)
	if err != nil {
		return nil, err
	}
	lay := layers[d.Kind()]
	if lay == nil {
		return nil, fmt.Errorf("%w: %v diagrams", pm.ErrUnsupported, d.Kind())
	}
	l, err := lay(ctx, d, width, th, m)
	if err != nil {
		return nil, err
	}
	l.fitTo(width)
	return l, nil
}

// Laid is a diagram laid out: boxes, polylines and labels, all plain values. It is safe to share
// between widgets; only Paint, on the loop, keeps anything (the labels it shapes).
//
// Paint draws in this order: groups, edges (under), their labels, boxes, edges drawn over the
// boxes, their labels, the groups' titles and dividers' labels, then free texts.
type Laid struct {
	size  gui.Size // as drawn, after the fit
	fit   float32  // the scale that fits the diagram to the width asked; 1 when it fits
	th    Theme
	nodes []laidNode
	edges []laidEdge
	group []laidGroup
	texts []label

	mu     sync.Mutex
	shaped map[shapeKey]*gui.TextLayout // the loop's shaping of each label line
}

// Size is the diagram as Paint draws it.
func (l *Laid) Size() gui.Size { return l.size }

// fitTo sets the fit that scales a diagram wider than width down to it. A layer sets size, its
// extent unscaled.
func (l *Laid) fitTo(width float32) {
	l.fit = 1
	if width > 0 && l.size.W > width {
		l.fit = width / l.size.W
		l.size = gui.Size{W: width, H: l.size.H * l.fit}
	}
}

// label is lines of text in one font and colour, placed in box: each line centred in it, or from
// its left when left is set.
type label struct {
	lines []string
	font  gui.Font
	box   gui.Rect
	color color.NRGBA
	left  bool
}

// newLabel measures s in f, wrapped at wrap (0: only at its line breaks), and is its label, placed
// at the origin.
func newLabel(m gui.Measurer, s string, f gui.Font, wrap float32, c color.NRGBA) label {
	if wrap <= 0 {
		wrap = 1 << 20
	}
	lines, sz := m.Wrap(s, f, wrap)
	return label{lines: lines, font: f, color: c, box: gui.Rect{W: sz.W, H: sz.H}}
}

// at places a label's box with its top left at x, y.
func (lb label) at(x, y float32) label {
	lb.box.X, lb.box.Y = x, y
	return lb
}

// centredIn places a label's box at the centre of r.
func (lb label) centredIn(r gui.Rect) label {
	return lb.at(r.X+(r.W-lb.box.W)/2, r.Y+(r.H-lb.box.H)/2)
}

// form is a box's outline: a flowchart shape, or one of the other diagrams'.
type form uint8

const (
	formRect form = iota
	formRound
	formStadium
	formSubroutine
	formCylinder
	formCircle
	formAsymmetric
	formDiamond
	formHexagon
	formParallelogram
	formParallelogramAlt
	formTrapezoid
	formTrapezoidAlt
	formDoubleCircle
	formActor // a stick figure, its label under it: a sequence diagram's actor
	formNote  // a sheet with its top right corner folded
	formBar   // a filled bar: a state diagram's fork or join
	formStart // a filled dot: a state diagram's start
	formEnd   // a ring round a filled dot: its end
	formNone  // no outline: the label alone
	formFace  // a face for a score (mood): smiling, neutral or frowning; a journey task's
)

// laidNode is a box: its form, filled and outlined, its label centred in it, and compartments
// under a divider each (a class's members, an entity's attributes).
type laidNode struct {
	form         form
	box          gui.Rect
	fill, stroke color.NRGBA
	width        float32
	dashed       bool
	label        label
	parts        []part
	mood         int // formFace: 1 to 5, a journey's score; 4 and 5 smile, 3 is level, 1 and 2 frown
}

// part is a divider across a box or group at y (none when y is 0), and the labels under it.
type part struct {
	y      float32
	labels []label
	dashed bool
}

// end is what an edge ends in.
type end uint8

const (
	endNone        end = iota
	endArrow           // a filled arrowhead
	endOpen            // an open arrowhead: two strokes
	endHalf            // one barb: a sequence diagram's async message
	endDot             // a filled dot
	endCross           // an x
	endTriangle        // a hollow triangle: inheritance, realisation
	endDiamond         // a filled diamond: composition
	endDiamondOpen     // a hollow diamond: aggregation
	endOne             // ||: exactly one
	endZeroOne         // o|: zero or one
	endMany            // }|: one or more
	endZeroMany        // }o: zero or more
)

// laidEdge is a polyline through pts, its ends, and its labels: one at its middle, and one near
// each end (a multiplicity, a cardinality).
type laidEdge struct {
	pts                  []gui.Point
	line                 pm.LineKind
	head, tail           end // head at the last point, tail at the first
	color                color.NRGBA
	label                *label
	headLabel, tailLabel *label
	over                 bool        // drawn over the boxes: a message across an activation
	plain                bool        // its middle label on nothing: it sits beside the line, not on it
	ground               color.NRGBA // what its middle label sits on; zero: the page
}

// laidGroup is a frame round other things: a box, its title, and dividers across it with their
// labels (an alt's else, a composite state's regions).
type laidGroup struct {
	box          gui.Rect
	title        label
	fill, stroke color.NRGBA // zero: the theme's cluster colours
	bare         bool        // no fill, the frame alone
	dashed       bool
	tab          bool // the title in a tab at the top left, as a sequence diagram's loop and alt
	parts        []part
}

type shapeKey struct {
	text string
	font gui.Font
}
