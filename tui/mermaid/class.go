package mermaid

import (
	"context"
	"strings"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A CLASS DIAGRAM IN CELLS: the model adapted to the router's graph (graph.go), laid out by
// layGraph, as a flowchart is.
//
// A class is a box of sections: its annotations («interface») and name, centred, then its
// attributes, then its methods, a rule between (an empty one left out: a class with no members is
// its name alone). A relation is an edge: its line's stroke, each end's mark (inheritance a hollow
// triangle, composition a filled diamond, aggregation a hollow one, an association an arrowhead),
// its label on the line, its cardinalities beside its ends. A namespace is a frame. A note is a
// dashed box, tied to the class it is for by a dashed line with no ends.

func init() {
	register(pm.Class, layAs(LayClass), nil)
	registerNarrower(pm.Class, classDown)
}

// LayClass lays d out in cells, through the router (layGraph). A relation whose end isn't a class
// is ErrUnsupported, for the host's notice, never dropped. layout.ErrTooLarge comes back as it is.
func LayClass(ctx context.Context, d *pm.ClassDiagram, m Measurer) (*Grid, error) {
	g, _, err := layGraph(ctx, classGraph(d), m)
	return g, err
}

// classGraph is d as the router's graph: a node per class, then one per note; an edge per relation,
// then one per note tied to its class; a frame per namespace.
func classGraph(d *pm.ClassDiagram) graph {
	gr := graph{dir: d.Dir}
	frameOf := map[string]int{}
	for _, ns := range d.Namespaces {
		if _, dup := frameOf[ns.Name]; dup || ns.Name == "" {
			continue
		}
		frameOf[ns.Name] = len(gr.frames)
		gr.frames = append(gr.frames, gframe{title: ns.Name, parent: -1})
	}
	index := map[string]int{}
	for _, c := range d.Classes {
		index[c.ID] = len(gr.nodes)
		frame := -1
		if f, ok := frameOf[c.Namespace]; ok && c.Namespace != "" {
			frame = f
		}
		gr.nodes = append(gr.nodes, gnode{id: c.ID, form: FormSquare, label: c.Label, sections: classSections(c), frame: frame})
	}
	for _, r := range d.Relations {
		from, okf := index[r.From]
		to, okt := index[r.To]
		if !okf || !okt {
			from, to = -1, -1
		}
		gr.edges = append(gr.edges, gedge{from: from, to: to, stroke: strokeOf(r.Line), head: relEndOf(r.ToEnd),
			tail: relEndOf(r.FromEnd), label: r.Label, headLabel: r.ToCard, tailLabel: r.FromCard})
	}
	for _, n := range d.Notes {
		note := gnode{form: FormNote, label: n.Text}
		if lines := strings.Split(n.Text, "\n"); len(lines) > 1 {
			note.label, note.sections = strings.Join(lines, " "), [][]string{lines}
		}
		note.frame = -1
		gr.nodes = append(gr.nodes, note)
		if c, ok := index[n.For]; ok && n.For != "" {
			gr.edges = append(gr.edges, gedge{from: len(gr.nodes) - 1, to: c, stroke: StrokeDashed})
		}
	}
	return gr
}

// classSections is c's box's rows by section: its annotations and name, then its attributes and
// its methods, an empty one left out; nil for a class with neither (its name alone, the label).
func classSections(c pm.ClassBox) [][]string {
	if len(c.Annotations) == 0 && len(c.Attributes) == 0 && len(c.Methods) == 0 {
		return nil
	}
	head := make([]string, 0, len(c.Annotations)+1)
	for _, a := range c.Annotations {
		head = append(head, "«"+a+"»")
	}
	sections := [][]string{append(head, c.Label)}
	for _, members := range [][]string{c.Attributes, c.Methods} {
		if len(members) > 0 {
			sections = append(sections, members)
		}
	}
	return sections
}

// relEndOf is the router's End for a relation's end.
func relEndOf(e pm.RelEnd) End {
	switch e {
	case pm.RelInheritance:
		return EndTriangle
	case pm.RelComposition:
		return EndDiamond
	case pm.RelAggregation:
		return EndDiamondHollow
	case pm.RelAssociation:
		return EndArrow
	}
	return EndNone
}

// classDown is a class diagram that runs across (LR, RL) as one running down (TB): its narrower
// form (LayWithin); nil for one already running down.
func classDown(d pm.Diagram) pm.Diagram {
	cd, ok := d.(*pm.ClassDiagram)
	if !ok || cd.Dir != pm.LR && cd.Dir != pm.RL {
		return nil
	}
	down := *cd
	down.Dir = pm.TB
	return &down
}
