package mermaid

import (
	"context"
	"strings"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// AN ENTITY-RELATIONSHIP DIAGRAM IN CELLS: the model adapted to the router's graph (graph.go), laid
// out by layGraph, as a class diagram is.
//
// An entity is a box of sections: its name centred, then a row per attribute, its type, name, keys and
// comment each in a column of its own (a column no row fills takes no room). A relationship is an
// edge, solid when it is identifying and dashed when it isn't, its label on the line and a crow's foot
// at either end:
//
//	exactly one    zero or one    one or more    zero or more
//	     ╪              ○              ┼              ○
//	 ┌───┴───┐          ┼              ∧              ∧
//	 │       │      ┌───┴───┐      ┌───┴───┐      ┌───┴───┐
//
// A relationship of an entity with itself is the router's ↺ and a footnote, which says its
// cardinalities: a self-loop has no ends to draw them at.

func init() {
	register(pm.ER, layAs(LayER), nil)
	registerNarrower(pm.ER, erDown)
}

// LayER lays d out in cells, through the router (layGraph). A relationship whose end isn't an entity
// is ErrUnsupported, for the host's notice, never dropped. layout.ErrTooLarge comes back as it is.
func LayER(ctx context.Context, d *pm.ERDiagram, m Measurer) (*Grid, error) {
	g, _, err := layGraph(ctx, erGraph(d, m), m)
	return g, err
}

// erGraph is d as the router's graph: a node per entity, an edge per relationship.
func erGraph(d *pm.ERDiagram, m Measurer) graph {
	gr := graph{dir: d.Dir}
	index := map[string]int{}
	for _, e := range d.Entities {
		index[e.ID] = len(gr.nodes)
		n := gnode{id: e.ID, form: FormSquare, label: e.Label, frame: -1}
		if len(e.Attributes) > 0 {
			n.sections = [][]string{{e.Label}, attributeRows(e.Attributes, m)}
		}
		gr.nodes = append(gr.nodes, n)
	}
	for _, r := range d.Relationships {
		from, okf := index[r.From]
		to, okt := index[r.To]
		if !okf || !okt {
			from, to = -1, -1
		}
		e := gedge{from: from, to: to, stroke: StrokeDashed, head: cardEnd(r.ToCard), tail: cardEnd(r.FromCard), label: r.Label}
		if r.Identifying {
			e.stroke = StrokeLight
		}
		if from == to && from >= 0 { // no ends to draw them at: the footnote says them
			e.label = strings.TrimSpace(r.Label + " " + cardText(r.FromCard) + " to " + cardText(r.ToCard))
		}
		gr.edges = append(gr.edges, e)
	}
	return gr
}

// attributeRows is an entity's attributes as a table's rows, each column as wide as its widest cell
// and two cells from the next, a column no row fills left out; a row has no spaces after its last.
func attributeRows(attrs []pm.Attribute, m Measurer) []string {
	const columns = 4
	cells := make([][columns]string, len(attrs))
	var widths [columns]int
	for i, a := range attrs {
		cells[i] = [columns]string{a.Type, a.Name, strings.Join(a.Keys, ", "), a.Comment}
		for c, s := range cells[i] {
			widths[c] = max(widths[c], m.StringWidth(s))
		}
	}
	rows := make([]string, len(attrs))
	for i := range cells {
		var b strings.Builder
		for c, s := range cells[i] {
			if widths[c] == 0 {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("  ")
			}
			b.WriteString(s + strings.Repeat(" ", widths[c]-m.StringWidth(s)))
		}
		rows[i] = strings.TrimRight(b.String(), " ")
	}
	return rows
}

// cardEnd is the crow's foot of a relationship's end.
func cardEnd(c pm.Cardinality) End {
	switch c {
	case pm.ExactlyOne:
		return EndOne
	case pm.ZeroOrOne:
		return EndZeroOne
	case pm.OneOrMore:
		return EndOneMany
	case pm.ZeroOrMore:
		return EndZeroMany
	}
	return EndNone
}

// cardText is a cardinality as a footnote writes it.
func cardText(c pm.Cardinality) string {
	switch c {
	case pm.ExactlyOne:
		return "1"
	case pm.ZeroOrOne:
		return "0..1"
	case pm.OneOrMore:
		return "1..*"
	case pm.ZeroOrMore:
		return "0..*"
	}
	return "?"
}

// erDown is a diagram that runs across (LR, RL) as one running down (TB): its narrower form
// (LayWithin); nil for one already running down.
func erDown(d pm.Diagram) pm.Diagram {
	er, ok := d.(*pm.ERDiagram)
	if !ok || er.Dir != pm.LR && er.Dir != pm.RL {
		return nil
	}
	down := *er
	down.Dir = pm.TB
	return &down
}
