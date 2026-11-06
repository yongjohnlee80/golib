package mermaid

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Quadrant is the quadrant chart kind's value, declared here and registered in init (kind.go).
const Quadrant Kind = 12

func init() {
	register(Quadrant, "quadrant", []string{"quadrantChart"}, (*parser).quadrant)
}

// QuadrantDiagram is a parsed quadrant chart: a square split in four by its axes' midpoints, each
// quadrant labelled, and points placed by two numbers from 0 to 1.
type QuadrantDiagram struct {
	Title string
	// XAxis runs left to right, YAxis bottom to top: each its low end's label and its high end's,
	// "" when the chart gives none.
	XAxis, YAxis QuadrantAxis
	// Quadrants are quadrant-1 to quadrant-4's labels, as Mermaid numbers them: [0] top right, [1]
	// top left, [2] bottom left, [3] bottom right; "" when the chart gives none.
	Quadrants [4]string
	Points    []QuadrantPoint
}

func (*QuadrantDiagram) Kind() Kind { return Quadrant }

// QuadrantAxis is an axis's two labels: the low end's and the high end's.
type QuadrantAxis struct {
	Low, High string
}

// QuadrantPoint is a point: its label, and where it lies, X from the left and Y from the bottom,
// each from 0 to 1.
type QuadrantPoint struct {
	Label string
	X, Y  float64
	Span  [2]int // its statement
}

// quadrantStyles are the keys a point's style or a classDef may set. They are read, so a chart
// that uses them parses, and drawn by neither renderer: a point is drawn in the diagram's colours.
var quadrantStyles = map[string]bool{"radius": true, "color": true, "stroke-color": true, "stroke-width": true}

// quadrantParser is the parse of one quadrant chart. It holds the parser rather than embedding it,
// so the parser's own statement stays the flowchart's.
type quadrantParser struct {
	p *parser
	d *QuadrantDiagram
}

// quadrant parses a quadrant chart's body: rest is its header line after the keyword, which takes
// nothing. Each line is a title, an axis, a quadrant's label, a classDef, an accessibility line, or
// one point.
func (p *parser) quadrant(rest string, restOff int) (Diagram, error) {
	t := &quadrantParser{p: p, d: &QuadrantDiagram{}}
	if s := strings.TrimSpace(rest); s != "" {
		bad, _ := firstWord(s)
		p.fail(restOff+strings.Index(rest, s), "unexpected %q after quadrantChart", bad)
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		t.statement(st)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	return p.done(t.d)
}

// statement is one line.
func (t *quadrantParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "title":
		if s, err := t.p.label(rest, restOff); err == nil {
			t.d.Title = s
		}
		return
	case "x-axis":
		t.axis(&t.d.XAxis, rest, restOff)
		return
	case "y-axis":
		t.axis(&t.d.YAxis, rest, restOff)
		return
	case "quadrant-1", "quadrant-2", "quadrant-3", "quadrant-4":
		if s, err := t.p.label(rest, restOff); err == nil {
			t.d.Quadrants[w[len(w)-1]-'1'] = s
		}
		return
	case "classDef":
		name, styles := firstWord(rest)
		if name == "" {
			t.p.fail(restOff, "a classDef wants a name")
			return
		}
		t.styles(styles, st.off+len(st.text)-len(styles))
		return
	case "accTitle:", "accDescr:":
		return // accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if strings.HasPrefix(rest, ":") {
			return
		}
		t.p.unsupport(st.off, w+" block")
		return
	}
	t.point(st)
}

// axis reads `low --> high`, or `low` alone, into a.
func (t *quadrantParser) axis(a *QuadrantAxis, rest string, off int) {
	low, high, two := strings.Cut(rest, "-->")
	l, err := t.p.label(low, off)
	if err != nil {
		return
	}
	a.Low, a.High = l, ""
	if two {
		if h, err := t.p.label(high, off+len(low)+len("-->")); err == nil {
			a.High = h
		}
	}
}

// point reads `label: [x, y]`, a class after the label (`label:::name`) and styles after the
// bracket (`radius: 10, color: #ff0000`) allowed. x and y are finite numbers from 0 to 1.
func (t *quadrantParser) point(st stmt) {
	open := strings.LastIndexByte(st.text, '[')
	colon := strings.LastIndexByte(st.text[:max(open, 0)], ':')
	if open < 0 || colon < 0 {
		bad, _ := firstWord(st.text)
		t.p.fail(st.off, "unexpected %q: a quadrant chart's line is a title, an axis, a quadrant or a point (label: [x, y])", bad)
		return
	}
	name := strings.TrimSpace(st.text[:colon])
	if i := strings.Index(name, ":::"); i >= 0 {
		name = strings.TrimSpace(name[:i]) // its class: styles neither renderer draws
	}
	if name == "" {
		t.p.fail(st.off, "a point wants a label before its colon")
		return
	}
	label, err := t.p.label(name, st.off)
	if err != nil {
		return
	}
	if between := strings.TrimSpace(st.text[colon+1 : open]); between != "" {
		t.p.fail(st.off+colon+1, "unexpected %q between a point's colon and its [", between)
		return
	}
	end := strings.IndexByte(st.text[open:], ']')
	if end < 0 {
		t.p.fail(st.off+open, "a point's [x, y] is not closed")
		return
	}
	coords := st.text[open+1 : open+end]
	xs, ys, two := strings.Cut(coords, ",")
	if !two {
		t.p.fail(st.off+open, "a point wants [x, y]: two numbers")
		return
	}
	x, okx := t.coord("x", xs, st.off+open+1)
	y, oky := t.coord("y", ys, st.off+open+1+len(xs)+1)
	if !okx || !oky {
		return
	}
	after := st.text[open+end+1:]
	t.styles(after, st.off+open+end+1)
	t.d.Points = append(t.d.Points, QuadrantPoint{Label: label, X: x, Y: y, Span: [2]int{st.off, st.off + len(st.text)}})
}

// coord reads one of a point's numbers: a finite number from 0 to 1.
func (t *quadrantParser) coord(axis, s string, off int) (float64, bool) {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil:
		t.p.fail(off, "a point's %s %q is not a number", axis, s)
		return 0, false
	case math.IsNaN(v) || math.IsInf(v, 0):
		t.p.fail(off, "a point's %s %q is not finite", axis, s)
		return 0, false
	case v < 0 || v > 1:
		t.p.fail(off, "a point's %s %s is outside 0 to 1", axis, s)
		return 0, false
	}
	return v, true
}

// styles reads a comma-separated list of `key: value`, each key one a point may set
// (quadrantStyles); "" is none. They are read and not kept: neither renderer draws them.
func (t *quadrantParser) styles(s string, off int) {
	if strings.TrimSpace(s) == "" {
		return
	}
	for part := range strings.SplitSeq(s, ",") {
		k, v, ok := strings.Cut(part, ":")
		k = strings.TrimSpace(k)
		if !ok || !quadrantStyles[k] || strings.TrimSpace(v) == "" {
			t.p.fail(off, "unexpected %q: a point's style is radius, color, stroke-color or stroke-width, each key: value", strings.TrimSpace(part))
			return
		}
		off += len(part) + 1
	}
}
