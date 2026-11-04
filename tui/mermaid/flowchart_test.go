package mermaid

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

// layFC parses src, a flowchart, and lays it out, with its edges' links.
func layFC(t *testing.T, src string) (*pm.FlowchartDiagram, *Grid, [][][2]cell) {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fc, ok := d.(*pm.FlowchartDiagram)
	if !ok {
		t.Fatalf("not a flowchart: %T", d)
	}
	g, routes, err := layFlowchart(context.Background(), fc, newCells(1, 1))
	if err != nil {
		t.Fatalf("lay: %v", err)
	}
	return fc, g, routes
}

// paintFC is src laid out and painted into a surface its own size.
func paintFC(t *testing.T, src string) string {
	t.Helper()
	_, g, _ := layFC(t, src)
	s := newCells(g.Size.W, g.Size.H)
	g.Paint(s, 0, 0, -1, Theme{})
	return s.String()
}

// A flowchart draws in cells, each case its golden: ranks down (TB, TD), across (LR), up (BT) and
// back (RL); a fan-out sharing one trunk, a fan-in into one port; K2,2's edges; a long edge passing
// the ranks between; a decision with a way back up (a cycle); wide labels; and every end.
func TestAFlowchartDrawsInCells(t *testing.T) {
	for name, src := range map[string]string{
		"fc-chain-tb":  "flowchart TB\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-chain-lr":  "flowchart LR\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-chain-bt":  "flowchart BT\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-chain-rl":  "flowchart RL\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-fanout":    "flowchart TB\n  A[Hub] --> B[one]\n  A --> C[two]\n  A --> D[three]\n  A --> E[four]\n",
		"fc-fanin":     "flowchart TB\n  B[one] --> A[Hub]\n  C[two] --> A\n  D[three] --> A\n  E[four] --> A\n",
		"fc-k22":       "flowchart TB\n  A[a] --> C[c]\n  A --> D[d]\n  B[b] --> C\n  B --> D\n",
		"fc-long":      "flowchart TB\n  A --> B --> C --> D\n  A --> D\n",
		"fc-decision":  "flowchart TD\n  S([Start]) --> Q{Ready?}\n  Q -->|yes| Go[Go]\n  Q -->|no| Wait[Wait]\n  Wait --> Q\n",
		"fc-cycle":     "flowchart TB\n  A --> B --> C --> A\n",
		"fc-wide":      "flowchart LR\n  A[시작] --> B[東京] --> C[end]\n",
		"fc-ends":      "flowchart TB\n  A --> B\n  A --o C\n  A --x D\n  A --- E\n  F <--> G\n",
		"fc-invisible": "flowchart TB\n  A --> B\n  A ~~~ C\n",
	} {
		t.Run(name, func(t *testing.T) { golden(t, name, paintFC(t, src)) })
	}
}

// randomFlowchart is a seeded random DAG as flowchart source: n nodes with labels of mixed widths
// (wide ones among them), edges from earlier nodes to later ones, some long, in one of the four
// directions.
func randomFlowchart(r *rand.Rand) string {
	dirs := []string{"TB", "LR", "BT", "RL"}
	labels := []string{"a", "node", "a longer label", "東京", "x", "step two"}
	var b strings.Builder
	fmt.Fprintf(&b, "flowchart %s\n", dirs[r.IntN(len(dirs))])
	n := 2 + r.IntN(9)
	for i := range n {
		fmt.Fprintf(&b, "  N%d[%s]\n", i, labels[r.IntN(len(labels))])
	}
	for range r.IntN(2*n) + 1 {
		i := r.IntN(n - 1)
		j := i + 1 + r.IntN(n-1-i)
		fmt.Fprintf(&b, "  N%d --> N%d\n", i, j)
	}
	return b.String()
}

// The router's invariants hold for random DAGs in every direction:
//   - no line or mark inside a box, and boxes clear of one another (checkGrid);
//   - every edge's line is one connected path, from its From box to its To box;
//   - two edges of different sources and different targets never share a run (a link);
//   - a line beside a box only ever meets it, at a port, never runs along it;
//   - the same input gives the same drawing.
func TestTheFlowchartRoutersInvariantsHold(t *testing.T) {
	for seed := range uint64(300) {
		src := randomFlowchart(rand.New(rand.NewPCG(seed, 0x5eed)))
		what := fmt.Sprintf("seed %d:\n%s", seed, src)
		fc, g, routes := layFC(t, src)
		checkGrid(t, what, g)
		box := map[string]tui.Rect{}
		for _, b := range g.Boxes {
			box[b.ID] = b.Rect
		}
		next := func(r tui.Rect, c cell) bool { // c is beside r (not diagonally)
			inX, inY := c.x >= r.X && c.x < r.X+r.W, c.y >= r.Y && c.y < r.Y+r.H
			return inX && (c.y == r.Y-1 || c.y == r.Y+r.H) || inY && (c.x == r.X-1 || c.x == r.X+r.W)
		}
		for i, e := range fc.Edges {
			links := routes[i]
			if len(links) == 0 {
				t.Fatalf("%s edge %d (%s→%s) was not routed", what, i, e.From, e.To)
			}
			adj := map[cell][]cell{}
			for _, l := range links {
				adj[l[0]] = append(adj[l[0]], l[1])
				adj[l[1]] = append(adj[l[1]], l[0])
			}
			start := links[0][0]
			seen := map[cell]bool{start: true}
			for todo := []cell{start}; len(todo) > 0; {
				c := todo[0]
				todo = todo[1:]
				for _, n := range adj[c] {
					if !seen[n] {
						seen[n] = true
						todo = append(todo, n)
					}
				}
			}
			if len(seen) != len(adj) {
				t.Fatalf("%s edge %d (%s→%s) is in pieces: %d of %d cells reached", what, i, e.From, e.To, len(seen), len(adj))
			}
			var atFrom, atTo bool
			for c := range adj {
				atFrom = atFrom || next(box[e.From], c)
				atTo = atTo || next(box[e.To], c)
			}
			if !atFrom || !atTo {
				t.Fatalf("%s edge %d (%s→%s) doesn't reach its boxes: from %v, to %v", what, i, e.From, e.To, atFrom, atTo)
			}
		}
		for i, a := range fc.Edges {
			mine := map[[2]cell]bool{}
			for _, l := range routes[i] {
				mine[l] = true
				mine[[2]cell{l[1], l[0]}] = true
			}
			for j := i + 1; j < len(fc.Edges); j++ {
				b := fc.Edges[j]
				if a.From == b.From || a.To == b.To {
					continue
				}
				for _, l := range routes[j] {
					if mine[l] {
						t.Fatalf("%s edges %s→%s and %s→%s share a run at %v", what, a.From, a.To, b.From, b.To, l)
					}
				}
			}
		}
		toward := map[[2]int]dirs{{0, -1}: south, {0, 1}: north, {-1, 0}: east, {1, 0}: west}
		for _, b := range g.Boxes {
			r := b.Rect
			for c, d := range g.lines {
				if !next(r, c) {
					continue
				}
				var dx, dy int
				switch {
				case c.y == r.Y-1:
					dy = -1
				case c.y == r.Y+r.H:
					dy = 1
				case c.x == r.X-1:
					dx = -1
				default:
					dx = 1
				}
				if d&toward[[2]int{dx, dy}] == 0 {
					t.Fatalf("%s a line runs along box %s %+v at %v without meeting it", what, b.ID, r, c)
				}
			}
		}
		if again := paintFC(t, src); again != paintFC(t, src) {
			t.Fatalf("%s two drawings of one flowchart differ", what)
		}
	}
}

// A flowchart's edge into a node of its own rank, or into itself, isn't routed yet: it's left out,
// and the rest draws.
func TestAFlowchartLeavesOutWhatItDoesntRouteYet(t *testing.T) {
	_, g, routes := layFC(t, "flowchart TB\n  A --> A\n  A --> B\n")
	if len(routes[0]) != 0 {
		t.Errorf("the self-loop was routed: %v", routes[0])
	}
	if len(routes[1]) == 0 || len(g.Boxes) != 2 {
		t.Errorf("A→B: %d links, %d boxes", len(routes[1]), len(g.Boxes))
	}
}
