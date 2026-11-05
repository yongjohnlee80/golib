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
// the ranks between; a decision with a way back up (a cycle); wide labels; every end; edge labels
// beside their lines (TB) and along them (LR); dotted and thick lines, light where they meet
// others; self-loops; and a label with no room, a footnote.
func TestAFlowchartDrawsInCells(t *testing.T) {
	for name, src := range map[string]string{
		"fc-chain-tb":          "flowchart TB\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-chain-lr":          "flowchart LR\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-chain-bt":          "flowchart BT\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-chain-rl":          "flowchart RL\n  A[Start] --> B[Middle] --> C[End]\n",
		"fc-fanout":            "flowchart TB\n  A[Hub] --> B[one]\n  A --> C[two]\n  A --> D[three]\n  A --> E[four]\n",
		"fc-fanin":             "flowchart TB\n  B[one] --> A[Hub]\n  C[two] --> A\n  D[three] --> A\n  E[four] --> A\n",
		"fc-k22":               "flowchart TB\n  A[a] --> C[c]\n  A --> D[d]\n  B[b] --> C\n  B --> D\n",
		"fc-long":              "flowchart TB\n  A --> B --> C --> D\n  A --> D\n",
		"fc-decision":          "flowchart TD\n  S([Start]) --> Q{Ready?}\n  Q -->|yes| Go[Go]\n  Q -->|no| Wait[Wait]\n  Wait --> Q\n",
		"fc-cycle":             "flowchart TB\n  A --> B --> C --> A\n",
		"fc-wide":              "flowchart LR\n  A[시작] --> B[東京] --> C[end]\n",
		"fc-ends":              "flowchart TB\n  A --> B\n  A --o C\n  A --x D\n  A --- E\n  F <--> G\n",
		"fc-invisible":         "flowchart TB\n  A --> B\n  A ~~~ C\n",
		"fc-labels-tb":         "flowchart TD\n  Q{Ready?} -->|yes| A[Go]\n  Q -->|no| B[Wait]\n  A -->|done| C[End]\n",
		"fc-labels-lr":         "flowchart LR\n  Q{Ready?} -->|yes| A[Go]\n  Q -->|no| B[Wait]\n  A -->|done| C[End]\n",
		"fc-styles":            "flowchart TB\n  A -.-> B\n  A ==> C\n  A --> D\n  B -.- E\n  C ==> E\n  D --> E\n",
		"fc-selfloop":          "flowchart TB\n  A --> A\n  A --> B\n  B -->|again| B\n",
		"fc-footnote":          "flowchart TB\n  A --> B\n  B -->|back up| A\n",
		"fc-subgraph":          "flowchart TB\n  A[Start] --> B\n  subgraph S [Group]\n    B[inside] --> C[also]\n  end\n  C --> D[End]\n",
		"fc-subgraph-nested":   "flowchart TB\n  subgraph O [Outer]\n    A --> B\n    subgraph I [Inner]\n      B --> C\n    end\n  end\n  C --> D\n",
		"fc-subgraph-lr":       "flowchart LR\n  subgraph S [Group]\n    A --> B\n  end\n  B --> C\n",
		"fc-subgraphs-side":    "flowchart TB\n  R --> A\n  R --> B\n  subgraph S1 [One]\n    A\n  end\n  subgraph S2 [Two]\n    B\n  end\n",
		"fc-subgraph-footnote": "flowchart LR\n  subgraph S [a title far too long for its frame]\n    A\n  end\n  A --> B\n",
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
	edgeLabels := []string{"yes", "no", "a label", "東京"}
	for range r.IntN(2*n) + 1 {
		i := r.IntN(n - 1)
		j := i + 1 + r.IntN(n-1-i)
		if r.IntN(3) == 0 {
			fmt.Fprintf(&b, "  N%d -->|%s| N%d\n", i, edgeLabels[r.IntN(len(edgeLabels))], j)
		} else {
			fmt.Fprintf(&b, "  N%d --> N%d\n", i, j)
		}
	}
	return b.String()
}

// randomFramedFlowchart is randomFlowchart with subgraphs: one to three, a later one nested in an
// earlier one or not, each node in one of them or none, titles of mixed widths.
func randomFramedFlowchart(r *rand.Rand) string {
	dirs := []string{"TB", "LR", "BT", "RL"}
	labels := []string{"a", "node", "a longer label", "東京", "x", "step two"}
	titles := []string{"Group", "a longer title", "東京", "x"}
	var b strings.Builder
	fmt.Fprintf(&b, "flowchart %s\n", dirs[r.IntN(len(dirs))])
	n := 2 + r.IntN(9)
	k := 1 + r.IntN(3)
	parent := make([]int, k)
	for s := range k {
		parent[s] = -1
		if s > 0 && r.IntN(2) == 0 {
			parent[s] = r.IntN(s)
		}
	}
	home, label := make([]int, n), make([]string, n)
	for i := range n {
		home[i], label[i] = r.IntN(k+1)-1, labels[r.IntN(len(labels))]
	}
	for i := range n {
		if home[i] < 0 {
			fmt.Fprintf(&b, "  N%d[%s]\n", i, label[i])
		}
	}
	var write func(s int, indent string)
	write = func(s int, indent string) {
		fmt.Fprintf(&b, "%ssubgraph S%d [%s]\n", indent, s, titles[r.IntN(len(titles))])
		for i := range n {
			if home[i] == s {
				fmt.Fprintf(&b, "%s  N%d[%s]\n", indent, i, label[i])
			}
		}
		for c := range k {
			if parent[c] == s {
				write(c, indent+"  ")
			}
		}
		fmt.Fprintf(&b, "%send\n", indent)
	}
	for s := range k {
		if parent[s] < 0 {
			write(s, "  ")
		}
	}
	for range r.IntN(2*n) + 1 {
		i := r.IntN(n - 1)
		j := i + 1 + r.IntN(n-1-i)
		fmt.Fprintf(&b, "  N%d --> N%d\n", i, j)
	}
	return b.String()
}

// The router's invariants, and the frames', hold for random flowcharts with subgraphs (nested, side
// by side, members on any rank) in every direction.
func TestTheFramesInvariantsHold(t *testing.T) {
	for seed := range uint64(300) {
		src := randomFramedFlowchart(rand.New(rand.NewPCG(seed, 0xf4a3e)))
		checkFlowchart(t, fmt.Sprintf("seed %d", seed), src)
	}
}

// The router's invariants hold for random DAGs in every direction:
//   - no line or mark inside a box, and boxes clear of one another (checkGrid);
//   - every edge's line is one connected path, from its From box to its To box;
//   - two edges of different sources and different targets never share a run (a link);
//   - a line beside a box only ever meets it, at a port (its line pointing in, or its mark), never
//     runs along it;
//   - a label never covers a mark, another label, a turn or a junction: only a straight run of line
//     (a label along its edge, LR and RL);
//   - the same input gives the same drawing.
func TestTheFlowchartRoutersInvariantsHold(t *testing.T) {
	for seed := range uint64(300) {
		src := randomFlowchart(rand.New(rand.NewPCG(seed, 0x5eed)))
		checkFlowchart(t, fmt.Sprintf("seed %d", seed), src)
	}
}

// The invariants hold for the flowcharts that once broke them: a lower end in another net's column
// where an LR box has one port cell (the dogleg), a lower end the gap reached after a dogleg had
// taken its column (every lower end is known before any dogleg is placed), and a dogleg that would
// turn on a frame's border were it let beside one.
func TestTheFlowchartRoutersInvariantsHoldWhereTheyBroke(t *testing.T) {
	for name, src := range map[string]string{
		"one port cell": "flowchart LR\n  N0[x]\n  N1[a longer label]\n  N2[東京]\n  N3[a]\n  N4[node]\n  N5[a longer label]\n  N6[node]\n" +
			"  N3 --> N5\n  N3 --> N5\n  N5 --> N6\n  N3 --> N4\n  N5 --> N6\n  N3 --> N6\n  N1 --> N2\n  N1 --> N4\n  N1 --> N5\n",
		// seed 613 of the framed hunt: with doglegs allowed beside a frame's border, one turns on it
		"a dogleg beside a frame's border": "flowchart RL\n  N0[東京]\n  N2[a longer label]\n  subgraph S0 [Group]\n    N3[node]\n    N4[a]\n" +
			"    subgraph S1 [x]\n    end\n  end\n  subgraph S2 [Group]\n    N1[a]\n  end\n  N2 --> N4\n  N2 --> N4\n  N2 --> N3\n  N2 --> N4\n" +
			"  N2 --> N3\n  N2 --> N4\n  N2 --> N3\n  N1 --> N4\n  N3 --> N4\n  N1 --> N3\n",
		"a lower end found late": "flowchart RL\n  N0[x]\n  N1[a]\n  N2[東京]\n  N3[node]\n  N4[x]\n  N5[step two]\n  N6[x]\n  N7[東京]\n" +
			"  N4 --> N5\n  N3 --> N5\n  N1 --> N3\n  N3 --> N7\n  N1 --> N6\n  N5 --> N6\n  N2 --> N7\n  N2 --> N4\n  N4 --> N7\n" +
			"  N1 --> N7\n  N2 --> N7\n  N4 -->|yes| N7\n  N0 -->|no| N7\n  N4 -->|東京| N5\n  N5 --> N6\n  N0 -->|a label| N6\n",
	} {
		checkFlowchart(t, name, src)
	}
}

// checkFlowchart lays src out and checks the router's invariants (TestTheFlowchartRoutersInvariantsHold).
func checkFlowchart(t *testing.T, name, src string) {
	t.Helper()
	what := fmt.Sprintf("%s:\n%s\ndrawn:\n%s", name, src, paintFC(t, src))
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
			if d&toward[[2]int{dx, dy}] == 0 && g.marks[c] == "" {
				t.Fatalf("%s a line runs along box %s %+v at %v without meeting it", what, b.ID, r, c)
			}
		}
	}
	cover := map[cell]string{}
	for _, tx := range g.texts {
		for x := tx.x; x < tx.x+tx.w; x++ {
			c := cell{x, tx.y}
			if other, ok := cover[c]; ok {
				t.Fatalf("%s the labels %q and %q overlap at %v", what, other, tx.s, c)
			}
			cover[c] = tx.s
			if g.marks[c] != "" {
				t.Fatalf("%s the label %q covers the mark %q at %v", what, tx.s, g.marks[c], c)
			}
			if d, ok := g.lines[c]; ok && d != east|west {
				t.Fatalf("%s the label %q covers a turn or a junction (%s) at %v", what, tx.s, lineRune[d], c)
			}
		}
	}
	if again := paintFC(t, src); again != paintFC(t, src) {
		t.Fatalf("%s two drawings of one flowchart differ", what)
	}
	checkFrames(t, what, fc, g)
}

// checkFrames checks a flowchart's frames (vacuous with no subgraphs):
//   - a frame is inside the grid, and two frames nest (one strictly inside the other) or are apart;
//   - a node's box is inside each frame its subgraphs make, a free cell inside the border, and any
//     other box is a cell clear of the frame;
//   - a line on a frame's border crosses it, straight (│ over a top or bottom, ─ over a side),
//     never runs along it and never takes a corner, and a frame is never OR'd into the lines;
//   - no mark or label lies on a border;
//   - a title is on its top or bottom border with no line through it or the space either side,
//     or its frame carries a footnote mark, the title in the legend.
func checkFrames(t *testing.T, what string, fc *pm.FlowchartDiagram, g *Grid) {
	t.Helper()
	parentOf := map[string]string{}
	for _, s := range fc.Subgraphs {
		parentOf[s.ID] = s.Parent
	}
	byTitle := map[string]int{} // a frame by its subgraph: frames are made in fc.Subgraphs' order, non-empty ones
	fi := 0
	for _, s := range fc.Subgraphs {
		members := 0
		for _, n := range fc.Nodes {
			for sg, steps := n.Subgraph, 0; sg != "" && steps <= len(fc.Subgraphs); sg, steps = parentOf[sg], steps+1 {
				if sg == s.ID {
					members++
					break
				}
			}
		}
		if members > 0 {
			byTitle[s.ID] = fi
			fi++
		}
	}
	if fi != len(g.frames) {
		t.Fatalf("%s %d frames, want %d (one per subgraph holding a node)", what, len(g.frames), fi)
	}
	inside := func(a, b tui.Rect) bool { // a strictly inside b's border
		return a.X > b.X && a.Y > b.Y && a.X+a.W < b.X+b.W && a.Y+a.H < b.Y+b.H
	}
	apart := func(a, b tui.Rect) bool {
		return a.X+a.W <= b.X || b.X+b.W <= a.X || a.Y+a.H <= b.Y || b.Y+b.H <= a.Y
	}
	for i, f := range g.frames {
		r := f.rect
		if r.X < 0 || r.Y < 0 || r.X+r.W > g.Size.W || r.Y+r.H > g.Size.H {
			t.Fatalf("%s frame %d %+v outside the grid %+v", what, i, r, g.Size)
		}
		for j := i + 1; j < len(g.frames); j++ {
			o := g.frames[j].rect
			if !inside(r, o) && !inside(o, r) && !apart(r, o) {
				t.Fatalf("%s frames %d %+v and %d %+v overlap without nesting", what, i, r, j, o)
			}
		}
	}
	for bi, b := range g.Boxes {
		n := fc.Nodes[bi]
		mine := map[int]bool{}
		for sg, steps := n.Subgraph, 0; sg != "" && steps <= len(fc.Subgraphs); sg, steps = parentOf[sg], steps+1 {
			if f, ok := byTitle[sg]; ok {
				mine[f] = true
			}
		}
		grown := tui.Rect{X: b.Rect.X - 1, Y: b.Rect.Y - 1, W: b.Rect.W + 2, H: b.Rect.H + 2}
		for i, f := range g.frames {
			switch {
			case mine[i] && !inside(grown, f.rect):
				t.Fatalf("%s box %s %+v isn't a cell inside its frame %d %+v", what, b.ID, b.Rect, i, f.rect)
			case !mine[i] && !apart(grown, f.rect) && !inside(f.rect, grown):
				t.Fatalf("%s box %s %+v touches frame %d %+v it isn't in", what, b.ID, b.Rect, i, f.rect)
			}
		}
	}
	for i, f := range g.frames {
		r := f.rect
		for c, d := range g.lines {
			if !onBorder(r, c) {
				continue
			}
			corner := (c.x == r.X || c.x == r.X+r.W-1) && (c.y == r.Y || c.y == r.Y+r.H-1)
			topOrBottom := c.y == r.Y || c.y == r.Y+r.H-1
			switch {
			case corner:
				t.Fatalf("%s a line at frame %d's corner %v", what, i, c)
			case topOrBottom && d != north|south:
				t.Fatalf("%s a line along frame %d's top or bottom at %v (%s)", what, i, c, lineRune[d])
			case !topOrBottom && d != east|west:
				t.Fatalf("%s a line along frame %d's side at %v (%s)", what, i, c, lineRune[d])
			}
		}
		for c, mk := range g.marks {
			if onBorder(r, c) {
				t.Fatalf("%s the mark %q on frame %d's border at %v", what, mk, i, c)
			}
		}
		for _, tx := range g.texts {
			for x := tx.x; x < tx.x+tx.w; x++ {
				if onBorder(r, cell{x, tx.y}) {
					t.Fatalf("%s the label %q on frame %d's border at %v", what, tx.s, i, cell{x, tx.y})
				}
			}
		}
		switch {
		case f.shown != "":
			if f.ty != r.Y && f.ty != r.Y+r.H-1 || f.tx < r.X+3 {
				t.Fatalf("%s frame %d's title at (%d, %d) isn't on its top or bottom border %+v", what, i, f.tx, f.ty, r)
			}
			for x := f.tx - 1; x <= f.tx+tui.StringWidth(f.shown); x++ {
				if _, ok := g.lines[cell{x, f.ty}]; ok {
					t.Fatalf("%s a line crosses frame %d's title %q at %v", what, i, f.shown, cell{x, f.ty})
				}
			}
		case f.title != "":
			if f.mark == "" {
				t.Fatalf("%s frame %d's title %q is neither shown nor a footnote", what, i, f.title)
			}
			if _, ok := g.lines[cell{r.X + 1, r.Y}]; ok {
				t.Fatalf("%s a line crosses frame %d's footnote mark at %v", what, i, cell{r.X + 1, r.Y})
			}
		}
	}
}

// A self-loop isn't a routed line: it's a ↺ beside its box, and the rest draws.
func TestAFlowchartsSelfLoopIsAMarkBesideItsBox(t *testing.T) {
	_, g, routes := layFC(t, "flowchart TB\n  A --> A\n  A --> B\n")
	if len(routes[0]) != 0 {
		t.Errorf("the self-loop was routed as a line: %v", routes[0])
	}
	if len(routes[1]) == 0 || len(g.Boxes) != 2 {
		t.Errorf("A→B: %d links, %d boxes", len(routes[1]), len(g.Boxes))
	}
	r := g.Boxes[0].Rect
	var beside bool
	for c, mk := range g.marks {
		if mk == "↺" {
			beside = beside || c.x >= r.X-1 && c.x <= r.X+r.W && c.y >= r.Y-1 && c.y <= r.Y+r.H
		}
	}
	if !beside {
		t.Errorf("no ↺ beside A %+v: marks %v", r, g.marks)
	}
}

// A gap's doglegs share a jog row only where their crossings don't overlap, a cell clear: two that
// overlap take a row each, and one clear of both shares the first.
func TestAGapsDoglegsShareJogRowsOnlyClearOfEachOther(t *testing.T) {
	a := fcTerm{col: 5, node: 1, jog: true, jogCol: 8}   // crosses 5..8
	b := fcTerm{col: 7, node: 2, jog: true, jogCol: 4}   // crosses 4..7: overlaps a
	c := fcTerm{col: 12, node: 3, jog: true, jogCol: 11} // crosses 11..12: clear of both
	rows, n := jogRows([]*fcNet{{bots: []fcTerm{a, b}}, {bots: []fcTerm{c, {col: 2, node: 4}}}})
	if n != 2 || rows[a] == rows[b] {
		t.Fatalf("overlapping doglegs: %d rows, a on %d, b on %d; want 2, apart", n, rows[a], rows[b])
	}
	if rows[c] != 0 {
		t.Errorf("a dogleg clear of the others is on row %d, want 0", rows[c])
	}
	if _, ok := rows[fcTerm{col: 2, node: 4}]; ok {
		t.Error("an end with no dogleg was given a jog row")
	}
}

// LayWithin fits a diagram to its room where its kind can: a flowchart running across (LR) wider
// than the room is laid running down (TB), the narrower; one that fits is laid as written; and a
// kind with no narrower form (a sequence) is laid as written however wide, for its host to pan.
func TestLayWithinFitsAFlowchartRunningAcross(t *testing.T) {
	ctx, m := context.Background(), newCells(1, 1)
	lay := func(src string, room int) (*Grid, *Grid) {
		t.Helper()
		d, err := pm.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		as, err := Lay(ctx, d, m)
		if err != nil {
			t.Fatal(err)
		}
		within, err := LayWithin(ctx, d, m, room)
		if err != nil {
			t.Fatal(err)
		}
		return as, within
	}
	across := "flowchart LR\n  A[first step] --> B[second step] --> C[third step]\n"
	as, _ := lay(across, 1000)
	down, _ := lay(strings.Replace(across, "LR", "TB", 1), 1000)
	if down.Size.W >= as.Size.W {
		t.Fatalf("precondition: the chain laid TB (%d wide) is narrower than LR (%d)", down.Size.W, as.Size.W)
	}
	if _, within := lay(across, as.Size.W-1); within.Size != down.Size {
		t.Errorf("the LR chain in a room narrower than it: laid %+v, want it running down, %+v", within.Size, down.Size)
	}
	if _, within := lay(across, as.Size.W); within.Size != as.Size {
		t.Errorf("the LR chain in a room it fits: laid %+v, want it as written, %+v", within.Size, as.Size)
	}
	seq := "sequenceDiagram\n  Alice->>Bob: hello\n  Bob->>Carol: and on\n  Carol->>Dave: the end\n"
	if as, within := lay(seq, 5); within.Size != as.Size {
		t.Errorf("a sequence (no narrower form) in a room of 5: laid %+v, want it as written, %+v", within.Size, as.Size)
	}
}
