package layout

import (
	"fmt"
	"slices"
)

// vert is a node or a dummy, in the top-to-bottom frame: w runs along a rank, h across ranks. A
// left-to-right drawing swaps a node's width and height on the way in and the coordinates on the
// way out.
type vert struct {
	w, h  float64
	rank  int
	dummy bool
	path  []int // the groups holding it, outermost first
	order int   // its index in its rank
	x, y  float64
	// side is -1 or +1 for a group's left or right border on one rank, 0 otherwise; group is
	// the border's group. A group's borders stand on every rank it spans, one above the other,
	// so its box stays a rectangle that nothing outside it enters.
	side  int8
	group int
}

// dagEdge is an input edge pointing down the ranks: reversed when it closed a cycle.
type dagEdge struct {
	from, to int
	minLen   int
	orig     int
	rev      bool
}

type graph struct {
	in        Input
	nodeSep   float64
	edgeSep   float64
	rankSep   float64
	pad       float64
	hasLabels bool

	v       []vert
	edges   []dagEdge
	loops   []int // input edges from a node to itself
	ranks   int
	chains  [][]int // each dag edge's vertices, top to bottom
	labelV  []int   // each input edge's label dummy, or -1
	layers  [][]int
	preds   [][]int
	succs   [][]int
	nGroups int
}

func orDefault(f, d float32) float64 {
	if f == 0 {
		return float64(d)
	}
	return float64(f)
}

func (in Input) sideways() bool { return in.Dir == LR || in.Dir == RL }

func build(in Input, m *meter) (*graph, error) {
	g := &graph{
		in:      in,
		nodeSep: orDefault(in.NodeSep, 50),
		rankSep: orDefault(in.RankSep, 50),
		pad:     orDefault(in.GroupPad, 16),
		nGroups: len(in.Groups),
	}
	g.edgeSep = g.nodeSep * 0.4
	g.v = make([]vert, len(in.Nodes))
	for i, n := range in.Nodes {
		w, h := float64(n.W), float64(n.H)
		if in.sideways() {
			w, h = h, w
		}
		g.v[i] = vert{w: w, h: h}
	}
	for _, e := range in.Edges {
		if e.Label.W > 0 || e.Label.H > 0 {
			g.hasLabels = true
		}
	}
	if err := g.groupPaths(m); err != nil {
		return nil, err
	}
	return g, nil
}

// groupPaths gives each node the groups holding it, outermost first, and refuses groups that
// overlap without one holding the other: such groups cannot both be drawn as boxes.
func (g *graph) groupPaths(m *meter) error {
	type grp struct{ i, size int }
	gs := make([]grp, 0, len(g.in.Groups))
	member := make([]map[int]bool, len(g.in.Groups))
	for i, ns := range g.in.Groups {
		if err := m.add(len(ns)); err != nil {
			return err
		}
		member[i] = map[int]bool{}
		for _, n := range ns {
			member[i][n] = true
		}
		gs = append(gs, grp{i, len(member[i])})
	}
	// Larger groups first; equal ones by input order, so a group nested in an equal one is inner.
	slices.SortStableFunc(gs, func(a, b grp) int { return b.size - a.size })
	for _, gr := range gs {
		if err := m.add(len(g.in.Nodes)); err != nil {
			return err
		}
		for n := range g.in.Nodes {
			if member[gr.i][n] {
				g.v[n].path = append(g.v[n].path, gr.i)
			}
		}
	}
	for _, gr := range gs {
		if err := m.add(len(g.in.Nodes)); err != nil {
			return err
		}
		var prefix []int
		first := true
		for n := range g.in.Nodes {
			if !member[gr.i][n] {
				continue
			}
			p := g.v[n].path[:slices.Index(g.v[n].path, gr.i)]
			if first {
				prefix, first = p, false
			} else if !slices.Equal(prefix, p) {
				return fmt.Errorf("%w: group %d overlaps another without holding it or being held", ErrInvalid, gr.i)
			}
		}
	}
	return nil
}

// rank breaks cycles, assigns each node a rank by longest path, and then pulls nodes toward the
// side with more of their edges while no edge gets shorter than its MinLen.
func (g *graph) rank(m *meter) error {
	n := len(g.in.Nodes)
	out := make([][]int, n)
	for i, e := range g.in.Edges {
		if e.From == e.To {
			g.loops = append(g.loops, i)
			continue
		}
		out[e.From] = append(out[e.From], i)
	}
	rev := make([]bool, len(g.in.Edges))
	state := make([]uint8, n) // 0 unseen, 1 on the stack, 2 done
	type frame struct{ v, next int }
	for s := range n {
		if state[s] != 0 {
			continue
		}
		stack := []frame{{s, 0}}
		state[s] = 1
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < len(out[top.v]) {
				e := out[top.v][top.next]
				top.next++
				w := g.in.Edges[e].To
				switch state[w] {
				case 1:
					rev[e] = true
				case 0:
					state[w] = 1
					stack = append(stack, frame{w, 0})
				}
				continue
			}
			state[top.v] = 2
			stack = stack[:len(stack)-1]
		}
		if err := m.add(1); err != nil {
			return err
		}
	}
	inE := make([][]int, n)
	outE := make([][]int, n)
	for i, e := range g.in.Edges {
		if e.From == e.To {
			continue
		}
		de := dagEdge{from: e.From, to: e.To, minLen: max(e.MinLen, 1), orig: i, rev: rev[i]}
		if de.rev {
			de.from, de.to = de.to, de.from
		}
		if g.hasLabels {
			de.minLen *= 2
		}
		outE[de.from] = append(outE[de.from], len(g.edges))
		inE[de.to] = append(inE[de.to], len(g.edges))
		g.edges = append(g.edges, de)
	}
	indeg := make([]int, n)
	for _, e := range g.edges {
		indeg[e.to]++
	}
	topo := make([]int, 0, n)
	for v := range n {
		if indeg[v] == 0 {
			topo = append(topo, v)
		}
	}
	for i := 0; i < len(topo); i++ {
		for _, ei := range outE[topo[i]] {
			w := g.edges[ei].to
			if indeg[w]--; indeg[w] == 0 {
				topo = append(topo, w)
			}
		}
		if err := m.add(len(outE[topo[i]]) + 1); err != nil {
			return err
		}
	}
	for _, v := range topo {
		for _, ei := range inE[v] {
			e := g.edges[ei]
			g.v[v].rank = max(g.v[v].rank, g.v[e.from].rank+e.minLen)
		}
	}
	for range 8 {
		changed := false
		for _, v := range topo {
			r := g.v[v].rank
			switch {
			case len(outE[v]) > len(inE[v]):
				slack := -1
				for _, ei := range outE[v] {
					e := g.edges[ei]
					if s := g.v[e.to].rank - r - e.minLen; slack < 0 || s < slack {
						slack = s
					}
				}
				if slack > 0 {
					g.v[v].rank += slack
					changed = true
				}
			case len(inE[v]) > len(outE[v]):
				slack := -1
				for _, ei := range inE[v] {
					e := g.edges[ei]
					if s := r - g.v[e.from].rank - e.minLen; slack < 0 || s < slack {
						slack = s
					}
				}
				if slack > 0 {
					g.v[v].rank -= slack
					changed = true
				}
			}
			if err := m.add(len(inE[v]) + len(outE[v]) + 1); err != nil {
				return err
			}
		}
		if !changed {
			break
		}
	}
	lo, hi := 0, 0
	for i := range n {
		if i == 0 || g.v[i].rank < lo {
			lo = g.v[i].rank
		}
		hi = max(hi, g.v[i].rank)
	}
	for i := range n {
		g.v[i].rank -= lo
	}
	if n > 0 {
		g.ranks = hi - lo + 1
	}
	return nil
}

// dummiesNeeded is how many dummy nodes split would make.
func (g *graph) dummiesNeeded() int {
	d := 0
	for _, e := range g.edges {
		d += g.v[e.to].rank - g.v[e.from].rank - 1
	}
	return d
}

// commonPath is the groups two vertices share: a dummy belongs where both its ends do.
func commonPath(a, b []int) []int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n:n]
}

// split replaces every edge spanning several ranks by a chain through one dummy per rank between,
// and builds the adjacency of adjacent ranks the ordering and placing passes work on.
func (g *graph) split() {
	g.labelV = make([]int, len(g.in.Edges))
	for i := range g.labelV {
		g.labelV[i] = -1
	}
	g.chains = make([][]int, len(g.edges))
	for i, e := range g.edges {
		chain := []int{e.from}
		from, to := g.v[e.from].rank, g.v[e.to].rank
		lbl := g.in.Edges[e.orig].Label
		labelled := lbl.W > 0 || lbl.H > 0
		mid := from + (to-from)/2
		for r := from + 1; r < to; r++ {
			d := vert{rank: r, dummy: true, path: commonPath(g.v[e.from].path, g.v[e.to].path)}
			if labelled && r == mid {
				d.w, d.h = float64(lbl.W), float64(lbl.H)
				if g.in.sideways() {
					d.w, d.h = d.h, d.w
				}
				g.labelV[e.orig] = len(g.v)
			}
			chain = append(chain, len(g.v))
			g.v = append(g.v, d)
		}
		g.chains[i] = append(chain, e.to)
	}
	g.preds = make([][]int, len(g.v))
	g.succs = make([][]int, len(g.v))
	for _, c := range g.chains {
		for k := 1; k < len(c); k++ {
			g.succs[c[k-1]] = append(g.succs[c[k-1]], c[k])
			g.preds[c[k]] = append(g.preds[c[k]], c[k-1])
		}
	}
}

// groupSpans are each group's lowest and highest rank (-1 for a group holding no vertex) and the
// path of groups that hold it, from its first member.
func (g *graph) groupSpans() (lo, hi []int, prefix [][]int) {
	lo = make([]int, g.nGroups)
	hi = make([]int, g.nGroups)
	prefix = make([][]int, g.nGroups)
	for i := range lo {
		lo[i], hi[i] = -1, -1
	}
	for _, v := range g.v {
		for k, gr := range v.path {
			if lo[gr] < 0 || v.rank < lo[gr] {
				lo[gr] = v.rank
			}
			hi[gr] = max(hi[gr], v.rank)
			if prefix[gr] == nil {
				prefix[gr] = v.path[: k+1 : k+1]
			}
		}
	}
	return lo, hi, prefix
}

// bordersNeeded is how many border vertices borders would add: two on every rank of every
// group's span. Known before any is made, so the limit refuses first.
func (g *graph) bordersNeeded() int {
	lo, hi, _ := g.groupSpans()
	n := 0
	for gr := range lo {
		if lo[gr] >= 0 {
			n += 2 * (hi[gr] - lo[gr] + 1)
		}
	}
	return n
}

// borders puts a left and a right border on every rank a group spans, chained rank to rank so
// the placing pass keeps each side straight.
func (g *graph) borders(m *meter) error {
	lo, hi, prefix := g.groupSpans()
	for gr := range g.nGroups {
		if lo[gr] < 0 {
			continue
		}
		for _, side := range []int8{-1, 1} {
			prev := -1
			for r := lo[gr]; r <= hi[gr]; r++ {
				if err := m.add(1); err != nil {
					return err
				}
				b := len(g.v)
				g.v = append(g.v, vert{rank: r, dummy: true, path: prefix[gr], side: side, group: gr})
				g.preds = append(g.preds, nil)
				g.succs = append(g.succs, nil)
				if prev >= 0 {
					g.succs[prev] = append(g.succs[prev], b)
					g.preds[b] = append(g.preds[b], prev)
				}
				prev = b
			}
		}
	}
	return nil
}

// inGroup reports whether vertex v is inside group gr: it or a group it is in.
func (g *graph) inGroup(v, gr int) bool {
	for _, p := range g.v[v].path {
		if p == gr {
			return true
		}
	}
	return false
}

// isLabel reports whether v is a label's dummy, which takes the label's room like a node.
func (g *graph) isLabel(v int) bool { return g.v[v].dummy && (g.v[v].w > 0 || g.v[v].h > 0) }
