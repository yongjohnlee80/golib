package mermaid

import (
	"context"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A SANKEY IN CELLS: the model adapted to the router's graph (graph.go) and laid out by layGraph,
// left to right, as a flowchart LR is. A cell can't draw a band's width, so a flow is a line labelled
// with its amount, thick (━) when it carries at least half the largest flow, light (─) otherwise,
// and a node is a box of its name and what passes through it.
//
//	┌──────────┐            ┌───────┐           ┌───────────────┐
//	│ Coal     │ 75.571     │ Solid │ 200       │ Electricity   │
//	├──────────┤━━━━━━━━━━━►├───────┤━━━━━━━━━━►│ grid          │
//	│ 75.571   │            │ 391.9 │           ├───────────────┤
//	└──────────┘            └───────┘           │ 260           │
//	                                            └───────────────┘

func init() { register(pm.Sankey, layAs(LaySankey), nil) } // a wide one pans: it has no other direction

// LaySankey lays a sankey out in cells, through the router.
func LaySankey(ctx context.Context, d *pm.SankeyDiagram, m Measurer) (*Grid, error) {
	g, _, err := layGraph(ctx, sankeyGraph(d), m)
	return g, err
}

// sankeyGraph is d as the router's graph, laid left to right: a node per node, its name and what
// passes through it in two sections; an edge per flow, its amount its label, its stroke its weight.
func sankeyGraph(d *pm.SankeyDiagram) graph {
	gr := graph{dir: pm.LR}
	for i, name := range d.Nodes {
		gr.nodes = append(gr.nodes, gnode{id: name, form: FormSquare, label: name,
			sections: [][]string{{name}, {XYNumber(d.Through(i))}}, frame: -1})
	}
	largest := 0.0
	for _, l := range d.Links {
		largest = max(largest, l.Value)
	}
	for _, l := range d.Links {
		stroke := StrokeLight
		if l.Value*2 >= largest {
			stroke = StrokeThick
		}
		gr.edges = append(gr.edges, gedge{from: l.Source, to: l.Target, stroke: stroke, head: EndArrow, label: XYNumber(l.Value)})
	}
	return gr
}
