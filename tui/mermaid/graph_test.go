package mermaid

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// classLike is a graph a class diagram's adapter would make: a sectioned box (name, attributes,
// methods) that two boxes reach, one by a hollow triangle (inheritance), one by a diamond
// (composition), and a dashed edge with a hollow diamond (aggregation).
func classLike(dir pm.Dir) graph {
	return graph{dir: dir,
		nodes: []gnode{
			{id: "Animal", form: FormSquare, label: "Animal", frame: -1,
				sections: [][]string{{"«abstract»", "Animal"}, {"+name string", "+age int"}, {"+eat()", "+sleep()"}}},
			{id: "Dog", form: FormSquare, label: "Dog", frame: -1},
			{id: "Leg", form: FormSquare, label: "Leg", frame: -1},
			{id: "Pack", form: FormSquare, label: "Pack", frame: -1},
		},
		edges: []gedge{
			{from: 1, to: 0, head: EndTriangle},
			{from: 0, to: 2, tail: EndDiamond},
			{from: 3, to: 1, stroke: StrokeDashed, tail: EndDiamondHollow},
		},
	}
}

// The router draws what each kind brings it, each case its golden: a sectioned box (its head
// centred, a rule between sections) and the class ends (▽ hollow triangle, ◆ and ◇ diamonds) running
// down and across; a state machine's forms (● start, ◉ end, a ━━━━━ fork bar, a rounded state), and
// across (the bar ┃ then, a way back into it from a state); and
// a self-loop's label, a footnote.
func TestTheRouterDrawsEveryKindsPieces(t *testing.T) {
	for name, gr := range map[string]graph{
		"graph-sections-tb": classLike(pm.TB),
		"graph-sections-lr": classLike(pm.LR),
		"graph-forms": {dir: pm.TB,
			nodes: []gnode{{id: "start", form: FormDot, frame: -1}, {id: "Idle", form: FormRounded, label: "Idle", frame: -1},
				{id: "fork", form: FormBar, frame: -1}, {id: "A", form: FormRounded, label: "A", frame: -1},
				{id: "B", form: FormRounded, label: "B", frame: -1}, {id: "end", form: FormEndDot, frame: -1}},
			edges: []gedge{{from: 0, to: 1, head: EndArrow}, {from: 1, to: 2, head: EndArrow}, {from: 2, to: 3, head: EndArrow},
				{from: 2, to: 4, head: EndArrow}, {from: 3, to: 5, head: EndArrow}, {from: 4, to: 5, head: EndArrow}},
		},
		"graph-forms-lr": {dir: pm.LR,
			nodes: []gnode{{id: "start", form: FormDot, frame: -1}, {id: "fork", form: FormBar, frame: -1},
				{id: "A", form: FormRounded, label: "A", frame: -1}, {id: "B", form: FormRounded, label: "B", frame: -1},
				{id: "end", form: FormEndDot, frame: -1}},
			edges: []gedge{{from: 0, to: 1, head: EndArrow}, {from: 1, to: 2, head: EndArrow}, {from: 1, to: 3, head: EndArrow},
				{from: 2, to: 4, head: EndArrow}, {from: 3, to: 4, head: EndArrow}, {from: 3, to: 1, head: EndArrow}},
		},
		"graph-selfloop-label": {dir: pm.TB,
			nodes: []gnode{{id: "A", form: FormRounded, label: "Waiting", frame: -1}, {id: "B", form: FormRounded, label: "Done", frame: -1}},
			edges: []gedge{{from: 0, to: 0, head: EndArrow, label: "retry"}, {from: 0, to: 1, head: EndArrow}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, drawn := layG(t, gr)
			golden(t, name, drawn)
		})
	}
}

// A self-loop is always drawn, a ↺ beside its box, and its label is never lost: it is a footnote,
// in the legend under the drawing.
func TestASelfLoopsLabelIsAFootnote(t *testing.T) {
	g, _, drawn := layG(t, graph{dir: pm.TB,
		nodes: []gnode{{id: "A", form: FormRounded, label: "Waiting", frame: -1}},
		edges: []gedge{{from: 0, to: 0, head: EndArrow, label: "retry"}},
	})
	if !strings.Contains(drawn, "↺") {
		t.Errorf("no ↺ beside the box:\n%s", drawn)
	}
	// finish has placed the legend under the drawing: its row is the footnote's mark and the label
	if !strings.Contains(drawn, "¹ retry") {
		t.Errorf("the self-loop's label isn't in the legend:\n%s", drawn)
	}
	_ = g
}

// An edge the router can't draw is refused, ErrUnsupported (the host's notice), never dropped: one
// whose end is no node. A flowchart's edge to a name that is no node (a subgraph's id) is one.
func TestAnEdgeToNoNodeIsRefused(t *testing.T) {
	ctx := context.Background()
	gr := graph{dir: pm.TB, nodes: []gnode{{id: "A", form: FormSquare, label: "A", frame: -1}},
		edges: []gedge{{from: 0, to: 3}}}
	if _, _, err := layGraph(ctx, gr, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("an edge to node 3 of 1: %v, want ErrUnsupported", err)
	}
	fc := &pm.FlowchartDiagram{Dir: pm.TB, Nodes: []pm.Node{{ID: "A", Label: "A"}},
		Subgraphs: []pm.Subgraph{{ID: "S", Title: "S"}}, Edges: []pm.Edge{{From: "A", To: "S"}}}
	if _, err := LayFlowchart(ctx, fc, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("a flowchart's edge to a subgraph's id: %v, want ErrUnsupported", err)
	}
}

// randomGraph is a seeded graph with every piece a kind brings: forms (square, rounded, hexagon,
// dots, bars, notes), sectioned boxes, every End at either end, light, dashed and thick strokes,
// labels on the line and beside either end, self-loops, ways back up the ranks, and frames nested
// or not, in one of the four directions.
func randomGraph(r *rand.Rand) graph {
	dirs := []pm.Dir{pm.TB, pm.LR, pm.BT, pm.RL}
	labels := []string{"a", "node", "a longer label", "東京", "x"}
	forms := []Form{FormSquare, FormRounded, FormHexagon, FormDot, FormEndDot, FormBar, FormNote}
	ends := []End{EndNone, EndArrow, EndDot, EndCross, EndTriangle, EndDiamond, EndDiamondHollow}
	gr := graph{dir: dirs[r.IntN(len(dirs))]}
	for f := range r.IntN(3) {
		parent := -1
		if f > 0 && r.IntN(2) == 0 {
			parent = r.IntN(f)
		}
		gr.frames = append(gr.frames, gframe{title: labels[r.IntN(len(labels))], parent: parent})
	}
	n := 2 + r.IntN(8)
	for i := range n {
		nd := gnode{id: fmt.Sprintf("N%d", i), form: forms[r.IntN(len(forms))], label: labels[r.IntN(len(labels))],
			frame: r.IntN(len(gr.frames)+1) - 1}
		if (nd.form == FormSquare || nd.form == FormRounded) && r.IntN(3) == 0 {
			nd.sections = [][]string{{nd.label}, {"+a int", labels[r.IntN(len(labels))]}}
			if r.IntN(2) == 0 {
				nd.sections = append(nd.sections, []string{"+do()"})
			}
		}
		gr.nodes = append(gr.nodes, nd)
	}
	for range r.IntN(2*n) + 1 {
		i := r.IntN(n - 1)
		j := i + 1 + r.IntN(n-1-i)
		switch r.IntN(12) {
		case 0:
			j = i // a self-loop
		case 1:
			i, j = j, i // a way back up the ranks (a cycle)
		}
		e := gedge{from: i, to: j, stroke: Stroke(r.IntN(3)), head: ends[r.IntN(len(ends))], tail: ends[r.IntN(len(ends))]}
		if r.IntN(4) == 0 {
			e.label = labels[r.IntN(len(labels))]
		}
		cards := []string{"1", "*", "0..1", "1..*", "many"}
		if r.IntN(3) == 0 {
			e.headLabel = cards[r.IntN(len(cards))]
		}
		if r.IntN(3) == 0 {
			e.tailLabel = cards[r.IntN(len(cards))]
		}
		gr.edges = append(gr.edges, e)
	}
	return gr
}

// The router's invariants, and its frames', hold for random graphs with every piece any kind
// brings (checkGraph), in every direction.
func TestTheRoutersInvariantsHoldForAnyKindsGraph(t *testing.T) {
	for seed := range uint64(300) {
		checkGraph(t, fmt.Sprintf("seed %d", seed), randomGraph(rand.New(rand.NewPCG(seed, 0x9a4f))))
	}
}
