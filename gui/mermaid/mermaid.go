// Package mermaid renders Mermaid diagram specifications natively into vector graphics on a [gui.Canvas].
//
// Pipeline Architecture:
//
//	Worker Goroutine (Background):
//	[ Mermaid Source DSL ] ---> [ parse/mermaid.Parse ] ---> [ Model AST ]
//	                                                             |
//	                                                             v
//	[ Text Measurer Pool ] --------------------------------> [ layer Layout ]
//	                                                             |
//	                                                             v
//	                                                       [ *Laid Geometry ]
//	                                                       (Nodes, Edges, Groups)
//
//	UI Loop Goroutine (Foreground):
//	[ *Laid Geometry ] --------> [ Laid.Paint(Canvas) ] ----> GPU Vectors
//	                             (Shapes text via Canvas)
//
// Lay runs asynchronously on worker goroutines: it parses DSL text, measures labels via a pooled
// [gui.Measurer] (completely off the main UI loop), and computes node/edge coordinates. The resulting
// [Laid] structure encapsulates resolution-independent geometry ready for instant rasterization.
package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image/color"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/picture"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Theme is what a diagram is drawn in: its colours and its labels' font.
type Theme struct {
	Background, Text, Line, NodeFill, NodeStroke, ClusterFill, ClusterStroke color.NRGBA
	Font                                                                     gui.Font
	// Tint is a mind map node's own colour, by its ID, in place of its branch's: a caller that
	// colours nodes by what they are (a document's type). nil, or false for a node, keeps the
	// branch's tint. Other diagrams ignore it.
	Tint func(id string) (color.NRGBA, bool)
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
	return layParsed(ctx, d, width, th, m)
}

// ErrInvalidModel is a diagram model that breaks an invariant its parser keeps, so its layer can't
// lay it out.
var ErrInvalidModel = errors.New("mermaid: the diagram model breaks its parser's invariants")

// maxModelNodes is the most nodes a modelled mind map may have: the tree layout's own bound, checked
// before any label is measured.
const maxModelNodes = 1000

// LayDiagram lays out d, a mind map already modelled in parse/mermaid's own types, for width: Lay
// without the parse. A caller that builds the model sets each node's ID, label and shape itself,
// so it needs no Mermaid escaping, and every node keeps its own ID whatever its shape.
//
// The model is checked whole before anything is laid out, as the parser checks a source: any
// other model (nil, a nil *MindmapDiagram, another diagram type), a shape it does not define, or
// a broken tree (a root other than node 0, a parent at or after its child) is ErrInvalidModel; more
// than 1000 nodes, or a label longer than mermaid.MaxLabel, is mermaid.ErrTooLarge. An empty mind
// map lays out empty, as Lay("mindmap") does. Other diagrams are laid out from their source, by
// Lay.
func LayDiagram(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	if err := validModel(d); err != nil {
		return nil, err
	}
	return layParsed(ctx, d, width, th, m)
}

// layParsed lays out d, a model its parser built or validModel accepted.
func layParsed(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	if !(th.Font.Size > 0) { // every length is a multiple of it: nothing lays out to scale without one
		return nil, fmt.Errorf("%w: a font size of %v", pm.ErrUnsupported, th.Font.Size)
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

// validModel checks a caller's mind map for everything the mind map layer relies on that the
// parser keeps by construction: its type, its shapes, its tree, and its size.
func validModel(d pm.Diagram) error {
	md, ok := d.(*pm.MindmapDiagram)
	switch {
	case !ok:
		return fmt.Errorf("%w: %T; LayDiagram takes a *mermaid.MindmapDiagram, and Lay the other diagrams' source", ErrInvalidModel, d)
	case md == nil:
		return fmt.Errorf("%w: a nil mind map", ErrInvalidModel)
	case len(md.Nodes) > maxModelNodes:
		return fmt.Errorf("%w: a mind map of %d nodes, more than %d", pm.ErrTooLarge, len(md.Nodes), maxModelNodes)
	}
	for i, n := range md.Nodes {
		switch {
		case int(n.Shape) >= len(mindmapForm):
			return fmt.Errorf("%w: a mind map's node %d has shape %d, which mind maps do not define", ErrInvalidModel, i, n.Shape)
		case len(n.Label) > pm.MaxLabel:
			return fmt.Errorf("%w: a mind map's node %d has a label of %d bytes, more than %d", pm.ErrTooLarge, i, len(n.Label), pm.MaxLabel)
		case i == 0 && n.Parent != -1:
			return fmt.Errorf("%w: a mind map's node 0 has parent %d; the root comes first", ErrInvalidModel, n.Parent)
		case i > 0 && (n.Parent < 0 || n.Parent >= i):
			return fmt.Errorf("%w: a mind map's node %d has parent %d; each node's parent comes before it, and only node 0 is a root", ErrInvalidModel, i, n.Parent)
		}
	}
	return nil
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

	labels picture.Labels // the loop's shaping of each label line
}

// Size is the diagram as Paint draws it.
func (l *Laid) Size() gui.Size { return l.size }

// NodeAt is the id of the node whose box holds p, in Paint's coordinates (the fit applied), and
// true; false over no node, or over a node its layer kept no id for. Where boxes overlap, the one
// drawn last (on top) answers.
func (l *Laid) NodeAt(p gui.Point) (string, bool) {
	q := gui.Pt(p.X/l.fit, p.Y/l.fit) // Paint scales the unscaled boxes by fit
	for i := len(l.nodes) - 1; i >= 0; i-- {
		n := l.nodes[i]
		if n.id != "" && q.X >= n.box.X && q.X < n.box.X+n.box.W && q.Y >= n.box.Y && q.Y < n.box.Y+n.box.H {
			return n.id, true
		}
	}
	return "", false
}

// fitTo sets the fit that scales a diagram wider than width down to it. A layer sets size, its
// extent unscaled.
func (l *Laid) fitTo(width float32) { l.size, l.fit = picture.FitTo(l.size, width) }

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

// moved is lb placed dx, dy further on.
func (lb label) moved(dx, dy float32) label { return lb.at(lb.box.X+dx, lb.box.Y+dy) }

// moved is p placed dx, dy further on, its labels copied; a part without a divider keeps none.
func (p part) moved(dx, dy float32) part {
	if p.y != 0 {
		p.y += dy
	}
	labels := make([]label, len(p.labels))
	for i, lb := range p.labels {
		labels[i] = lb.moved(dx, dy)
	}
	p.labels = labels
	return p
}

// movedParts is ps placed dx, dy further on, copied; nil stays nil.
func movedParts(ps []part, dx, dy float32) []part {
	if ps == nil {
		return nil
	}
	out := make([]part, len(ps))
	for i, p := range ps {
		out[i] = p.moved(dx, dy)
	}
	return out
}

// moved is n placed dx, dy further on.
func (n laidNode) moved(dx, dy float32) laidNode {
	n.box.X, n.box.Y = n.box.X+dx, n.box.Y+dy
	n.label = n.label.moved(dx, dy)
	n.parts = movedParts(n.parts, dx, dy)
	return n
}

// moved is e placed dx, dy further on: its route copied, and each of its labels.
func (e laidEdge) moved(dx, dy float32) laidEdge {
	pts := make([]gui.Point, len(e.pts))
	for j, p := range e.pts {
		pts[j] = gui.Pt(p.X+dx, p.Y+dy)
	}
	e.pts = pts
	for _, lb := range []**label{&e.label, &e.headLabel, &e.tailLabel} {
		if *lb != nil {
			m := (*lb).moved(dx, dy)
			*lb = &m
		}
	}
	return e
}

// moved is g placed dx, dy further on.
func (g laidGroup) moved(dx, dy float32) laidGroup {
	g.box.X, g.box.Y = g.box.X+dx, g.box.Y+dy
	g.title = g.title.moved(dx, dy)
	g.parts = movedParts(g.parts, dx, dy)
	return g
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
	id           string // the model's node id, where its layer keeps one (mindmap); "" for none
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

// layoutErr is a layout engine's error as Lay reports it: one too large for the engine is the
// parser's ErrTooLarge, which a caller tests for whichever stage refused.
func layoutErr(err error) error {
	if errors.Is(err, layout.ErrTooLarge) {
		return fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
	}
	return err
}
