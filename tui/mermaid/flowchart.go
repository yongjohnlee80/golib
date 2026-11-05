package mermaid

import (
	"cmp"
	"context"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A FLOWCHART IN CELLS: the model adapted to the router's graph (graph.go), laid out by layGraph.

func init() {
	register(pm.Flowchart, layAs(LayFlowchart), nil) // every flowchart: a subgraph is a frame
	registerNarrower(pm.Flowchart, flowchartDown)
}

// LayFlowchart lays d out in cells, through the router (layGraph): its subgraphs are frames, an edge
// whose end isn't a node (a subgraph's id) is ErrUnsupported, for the host's notice, never dropped.
// layout.ErrTooLarge comes back as it is.
func LayFlowchart(ctx context.Context, d *pm.FlowchartDiagram, m Measurer) (*Grid, error) {
	g, _, err := layFlowchart(ctx, d, m)
	return g, err
}

// layFlowchart is LayFlowchart, also giving each edge's links, in d.Edges order (layGraph).
func layFlowchart(ctx context.Context, d *pm.FlowchartDiagram, m Measurer) (*Grid, [][][2]cell, error) {
	return layGraph(ctx, flowchartGraph(d), m)
}

// flowchartGraph is d as the router's graph: a node per node, its shape's outline; an edge per edge,
// its line's stroke and its arrows' ends; a frame per subgraph, nested as they are (a subgraph comes
// after the one it is in, as the model keeps them). An edge's end that names no node is given -1,
// which layGraph refuses.
func flowchartGraph(d *pm.FlowchartDiagram) graph {
	gr := graph{dir: d.Dir}
	frameOf := map[string]int{}
	for _, s := range d.Subgraphs {
		if _, dup := frameOf[s.ID]; dup {
			continue
		}
		frameOf[s.ID] = len(gr.frames)
		gr.frames = append(gr.frames, gframe{title: cmp.Or(s.Title, s.ID), parent: -1})
	}
	for _, s := range d.Subgraphs {
		if p, ok := frameOf[s.Parent]; ok && s.Parent != "" && frameOf[s.ID] > p {
			gr.frames[frameOf[s.ID]].parent = p
		}
	}
	index := map[string]int{}
	for i, n := range d.Nodes {
		index[n.ID] = i
		frame := -1
		if f, ok := frameOf[n.Subgraph]; ok && n.Subgraph != "" {
			frame = f
		}
		gr.nodes = append(gr.nodes, gnode{id: n.ID, form: FormOfShape(n.Shape), label: n.Label, frame: frame})
	}
	for _, e := range d.Edges {
		from, okf := index[e.From]
		to, okt := index[e.To]
		if !okf || !okt {
			from, to = -1, -1
		}
		gr.edges = append(gr.edges, gedge{from: from, to: to, stroke: strokeOf(e.Line), head: endOf(e.Head),
			tail: endOf(e.Tail), label: e.Label, minLen: e.MinLen, invisible: e.Line == pm.Invisible})
	}
	return gr
}

// endOf is the router's End for a flowchart arrow.
func endOf(a pm.Arrow) End {
	switch a {
	case pm.Head:
		return EndArrow
	case pm.Dot:
		return EndDot
	case pm.Cross:
		return EndCross
	}
	return EndNone
}

// flowchartDown is a flowchart that runs across (LR, RL) as one running down (TB): its narrower
// form (LayWithin); nil for one already running down.
func flowchartDown(d pm.Diagram) pm.Diagram {
	fc, ok := d.(*pm.FlowchartDiagram)
	if !ok || fc.Dir != pm.LR && fc.Dir != pm.RL {
		return nil
	}
	down := *fc
	down.Dir = pm.TB
	return &down
}

// FormOfShape is the outline a flowchart node's shape is drawn with: the nearest corner set.
func FormOfShape(s pm.Shape) Form {
	switch s {
	case pm.Round, pm.Stadium, pm.Circle, pm.DoubleCircle, pm.Cylinder:
		return FormRounded
	case pm.Diamond, pm.Hexagon:
		return FormHexagon
	}
	return FormSquare
}

// strokeOf is how an edge's line is drawn.
func strokeOf(l pm.LineKind) Stroke {
	switch l {
	case pm.Dotted:
		return StrokeDashed
	case pm.Thick:
		return StrokeThick
	}
	return StrokeLight
}
