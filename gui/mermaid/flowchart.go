package mermaid

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Flowchart] = layFlowchart }

// shapeForm is each flowchart shape's form.
var shapeForm = [...]form{
	pm.Rect: formRect, pm.Round: formRound, pm.Stadium: formStadium, pm.Subroutine: formSubroutine,
	pm.Cylinder: formCylinder, pm.Circle: formCircle, pm.Asymmetric: formAsymmetric, pm.Diamond: formDiamond,
	pm.Hexagon: formHexagon, pm.Parallelogram: formParallelogram, pm.ParallelogramAlt: formParallelogramAlt,
	pm.Trapezoid: formTrapezoid, pm.TrapezoidAlt: formTrapezoidAlt, pm.DoubleCircle: formDoubleCircle,
}

// arrowEnd is each flowchart arrow's end.
var arrowEnd = [...]end{pm.None: endNone, pm.Head: endArrow, pm.Dot: endDot, pm.Cross: endCross}

func layFlowchart(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	fc := d.(*pm.FlowchartDiagram)
	f := th.Font
	small := f
	small.Size *= 0.9
	l := &Laid{th: th}
	index := map[string]int{}
	in := layout.Input{Dir: dirOf(fc.Dir), NodeSep: f.Size * 2.6, RankSep: f.Size * 3, GroupPad: f.Size}
	for i, n := range fc.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index[n.ID] = i
		lines, ts := m.Wrap(n.Label, f, wrapAt)
		w, h := nodeSize(n.Shape, ts, f.Size)
		in.Nodes = append(in.Nodes, layout.Size{W: w, H: h})
		fill, stroke, text, sw := styled(n.Style, th)
		l.nodes = append(l.nodes, laidNode{form: shapeForm[n.Shape], fill: fill, stroke: stroke, width: sw,
			label: label{lines: lines, font: f, color: text, box: gui.Rect{W: ts.W, H: ts.H}}})
	}
	sub := map[string]string{} // each node's innermost subgraph
	for _, n := range fc.Nodes {
		sub[n.ID] = n.Subgraph
	}
	for _, e := range fc.Edges {
		le := layout.Edge{From: index[e.From], To: index[e.To], MinLen: max(e.MinLen, 1)}
		var lb *label
		if e.Label != "" {
			lines, ts := m.Wrap(e.Label, small, wrapAt)
			pad := small.Size * 0.3
			le.Label = layout.Size{W: ts.W + 2*pad, H: ts.H + 2*pad}
			lb = &label{lines: lines, font: small, color: th.Text, box: gui.Rect{W: ts.W, H: ts.H}}
		}
		in.Edges = append(in.Edges, le)
		de := laidEdge{line: e.Line, head: arrowEnd[e.Head], tail: arrowEnd[e.Tail], color: th.Line, label: lb}
		if g := sub[e.From]; g != "" && g == sub[e.To] { // inside one subgraph: its label sits on the subgraph's fill
			de.ground = th.ClusterFill
		}
		l.edges = append(l.edges, de)
	}
	// a subgraph's box holds its own nodes and every nested subgraph's
	parent := map[string]string{}
	for _, s := range fc.Subgraphs {
		parent[s.ID] = s.Parent
	}
	for _, s := range fc.Subgraphs {
		var members []int
		for i, n := range fc.Nodes {
			for g := n.Subgraph; g != ""; g = parent[g] {
				if g == s.ID {
					members = append(members, i)
					break
				}
			}
		}
		if len(members) == 0 {
			continue
		}
		in.Groups = append(in.Groups, members)
		title := s.Title
		if title == "" {
			title = s.ID
		}
		lines, ts := m.Wrap(title, small, wrapAt)
		l.group = append(l.group, laidGroup{title: label{lines: lines, font: small, color: th.Text, box: gui.Rect{W: ts.W, H: ts.H}, left: true}})
		// the margin round a group's members holds its title above them
		in.GroupPad = max(in.GroupPad, ts.H+small.Size*0.9)
	}
	res, err := layout.Layered(ctx, in, layout.Limits{})
	if err != nil {
		if errors.Is(err, layout.ErrTooLarge) {
			return nil, fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
		}
		return nil, err
	}
	pad := f.Size * 0.5
	for i := range l.nodes {
		n := &l.nodes[i]
		sz, c := in.Nodes[i], res.Nodes[i]
		n.box = gui.Rect{X: c.X - sz.W/2 + pad, Y: c.Y - sz.H/2 + pad, W: sz.W, H: sz.H}
		n.label.box.X, n.label.box.Y = n.box.X+(n.box.W-n.label.box.W)/2, n.box.Y+(n.box.H-n.label.box.H)/2
	}
	for i := range l.edges {
		e := &l.edges[i]
		for _, p := range res.Edges[i] {
			e.pts = append(e.pts, gui.Pt(p.X+pad, p.Y+pad))
		}
		if e.label != nil {
			c := res.Labels[i]
			e.label.box.X, e.label.box.Y = c.X+pad-e.label.box.W/2, c.Y+pad-e.label.box.H/2
		}
	}
	for i := range l.group {
		g := &l.group[i]
		r := res.Groups[i]
		g.box = gui.Rect{X: r.X + pad, Y: r.Y + pad, W: r.W, H: r.H}
		g.title.box.X, g.title.box.Y = g.box.X+small.Size*0.5, g.box.Y+small.Size*0.3
	}
	l.size = gui.Size{W: res.Size.W + 2*pad, H: res.Size.H + 2*pad}
	return l, nil
}

func dirOf(d pm.Dir) layout.Dir {
	switch d {
	case pm.BT:
		return layout.BT
	case pm.LR:
		return layout.LR
	case pm.RL:
		return layout.RL
	}
	return layout.TB
}

// nodeSize is a node's box for its label's size, by its shape: room for the shape's slants and
// curves around the text, as Mermaid gives it.
func nodeSize(s pm.Shape, text gui.Size, em float32) (w, h float32) {
	w, h = text.W+em*1.8, text.H+em*1.2
	switch s {
	case pm.Round:
		w += em * 0.6
	case pm.Stadium:
		w += h * 0.5
	case pm.Subroutine:
		w += em * 1.2
	case pm.Cylinder:
		h += em * 0.9
	case pm.Circle, pm.DoubleCircle:
		d := max(w, h)
		w, h = d, d
		if s == pm.DoubleCircle {
			w, h = w+em*0.8, h+em*0.8
		}
	case pm.Diamond:
		w, h = w+h*0.9, h+w*0.45
	case pm.Hexagon, pm.Asymmetric:
		w += h * 0.5
	case pm.Parallelogram, pm.ParallelogramAlt, pm.Trapezoid, pm.TrapezoidAlt:
		w += h * 0.7
	}
	return w, h
}

// styled is a node's colours: the theme's, with its style's fill, stroke, color and stroke-width
// where they are colours and lengths it can read.
func styled(st pm.Style, th Theme) (fill, stroke, text color.NRGBA, width float32) {
	fill, stroke, text, width = th.NodeFill, th.NodeStroke, th.Text, 1.2
	for _, p := range st {
		switch p.Name {
		case "fill":
			if c, ok := cssColor(p.Value); ok {
				fill = c
			}
		case "stroke":
			if c, ok := cssColor(p.Value); ok {
				stroke = c
			}
		case "color":
			if c, ok := cssColor(p.Value); ok {
				text = c
			}
		case "stroke-width":
			if v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(p.Value), "px"), 32); err == nil && v > 0 {
				width = float32(min(v, 8))
			}
		}
	}
	return fill, stroke, text, width
}
