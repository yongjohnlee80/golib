package mermaid

import (
	"context"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A REQUIREMENT DIAGRAM IN CELLS: the model adapted to the router's graph (graph.go), laid out by
// layGraph, as a class diagram is.
//
// A requirement or an element is a box of sections: its «type» and name, centred, then the fields it
// gives (an empty section left out). A relationship is an edge named «kind»: a container holds what
// it contains by a solid line from a dot at the container, and every other kind is a dashed line with
// an arrowhead at its destination.

func init() {
	register(pm.Requirement, layAs(LayRequirement), nil)
	registerNarrower(pm.Requirement, requirementDown)
}

// LayRequirement lays d out in cells, through the router (layGraph). A relationship whose end isn't a
// requirement or an element is ErrUnsupported, for the host's notice, never dropped.
// layout.ErrTooLarge comes back as it is.
func LayRequirement(ctx context.Context, d *pm.RequirementDiagram, m Measurer) (*Grid, error) {
	g, _, err := layGraph(ctx, requirementGraph(d), m)
	return g, err
}

// requirementGraph is d as the router's graph: a node per requirement or element, an edge per
// relationship.
func requirementGraph(d *pm.RequirementDiagram) graph {
	gr := graph{dir: d.Dir}
	index := map[string]int{}
	for _, n := range d.Nodes {
		index[n.Name] = len(gr.nodes)
		sections := [][]string{{"«" + n.Type.Stereotype() + "»", n.Name}}
		if fields := requirementFields(n); len(fields) > 0 {
			sections = append(sections, fields)
		}
		gr.nodes = append(gr.nodes, gnode{id: n.Name, form: FormSquare, label: n.Name, sections: sections, frame: -1})
	}
	for _, r := range d.Relations {
		from, okf := index[r.Src]
		to, okt := index[r.Dst]
		if !okf || !okt {
			from, to = -1, -1
		}
		e := gedge{from: from, to: to, stroke: StrokeDashed, head: EndArrow, label: "«" + r.Kind.String() + "»"}
		if r.Kind == pm.ReqContains {
			e.stroke, e.head, e.tail = StrokeLight, EndNone, EndDot
		}
		gr.edges = append(gr.edges, e)
	}
	return gr
}

// requirementFields are a node's fields as its box lists them, labelled as the window's box labels
// them (gui/mermaid); the ones it doesn't give are left out.
func requirementFields(n pm.RequirementNode) []string {
	var out []string
	add := func(name, v string) {
		if v != "" {
			out = append(out, name+": "+v)
		}
	}
	if n.Type == pm.ReqElement {
		add("Type", n.ElemType)
		add("Doc Ref", n.DocRef)
		return out
	}
	add("ID", n.ID)
	add("Text", n.Text)
	if n.Risk != 0 {
		add("Risk", n.Risk.String())
	}
	if n.Verify != 0 {
		add("Verification", n.Verify.String())
	}
	return out
}

// requirementDown is a diagram that runs across (LR, RL) as one running down (TB): its narrower form
// (LayWithin); nil for one already running down.
func requirementDown(d pm.Diagram) pm.Diagram {
	rd, ok := d.(*pm.RequirementDiagram)
	if !ok || rd.Dir != pm.LR && rd.Dir != pm.RL {
		return nil
	}
	down := *rd
	down.Dir = pm.TB
	return &down
}
