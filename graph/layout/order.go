package layout

import (
	"cmp"
	"math"
	"math/bits"
	"slices"
)

// order arranges each rank to reduce crossings: an initial order by a depth-first walk, then
// barycentre sweeps down and up, eight rounds, keeping the order with the fewest crossings.
func (g *graph) order(m *meter) error {
	g.layers = make([][]int, g.ranks)
	byRank := make([]int, len(g.v))
	for i := range byRank {
		byRank[i] = i
	}
	slices.SortStableFunc(byRank, func(a, b int) int { return g.v[a].rank - g.v[b].rank })
	seen := make([]bool, len(g.v))
	for _, s := range byRank {
		if seen[s] {
			continue
		}
		stack := []int{s}
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[v] {
				continue
			}
			seen[v] = true
			g.layers[g.v[v].rank] = append(g.layers[g.v[v].rank], v)
			for k := len(g.succs[v]) - 1; k >= 0; k-- {
				stack = append(stack, g.succs[v][k])
			}
		}
		if err := m.add(1); err != nil {
			return err
		}
	}
	for _, l := range g.layers {
		g.setOrder(l)
		if err := g.sortLayer(m, l, func(v int) float64 { return float64(g.v[v].order) }); err != nil {
			return err
		}
	}
	best := g.copyLayers()
	bestX, err := g.crossings(m)
	if err != nil {
		return err
	}
	for range 8 {
		if bestX == 0 {
			break
		}
		for r := 1; r < g.ranks; r++ {
			if err := g.sweep(g.layers[r], g.preds, m); err != nil {
				return err
			}
		}
		for r := g.ranks - 2; r >= 0; r-- {
			if err := g.sweep(g.layers[r], g.succs, m); err != nil {
				return err
			}
		}
		x, err := g.crossings(m)
		if err != nil {
			return err
		}
		if x < bestX {
			bestX, best = x, g.copyLayers()
		}
	}
	g.layers = best
	for _, l := range g.layers {
		g.setOrder(l)
	}
	return nil
}

func (g *graph) setOrder(layer []int) {
	for i, v := range layer {
		g.v[v].order = i
	}
}

func (g *graph) copyLayers() [][]int {
	out := make([][]int, len(g.layers))
	for i, l := range g.layers {
		out[i] = slices.Clone(l)
	}
	return out
}

// sweep orders one rank by the mean position of each vertex's neighbours on the rank next to
// it (adj: the one above or below). A vertex with none keeps its place.
func (g *graph) sweep(layer []int, adj [][]int, m *meter) error {
	bc := make(map[int]float64, len(layer))
	for _, v := range layer {
		ns := adj[v]
		if len(ns) == 0 {
			bc[v] = float64(g.v[v].order)
			continue
		}
		s := 0.0
		for _, w := range ns {
			s += float64(g.v[w].order)
		}
		bc[v] = s / float64(len(ns))
		if err := m.add(len(ns)); err != nil {
			return err
		}
	}
	if err := g.sortLayer(m, layer, func(v int) float64 { return bc[v] }); err != nil {
		return err
	}
	return m.add(len(layer))
}

// sortLayer orders a rank by key, keeping each group's members together: a group sorts by the
// mean key of its members on this rank, a vertex outside it by its own, level by level from the
// outermost group in.
func (g *graph) sortLayer(m *meter, layer []int, key func(int) float64) error {
	// The work: each vertex's group path walked twice (the means, then its key sequence), and a
	// comparison sort of n keys, each comparison walking at most the deepest path.
	n, depth, walk := len(layer), 0, 0
	for _, v := range layer {
		d := len(g.v[v].path) + 1
		depth, walk = max(depth, d), walk+d
	}
	if err := m.add(2*walk + n*bits.Len(uint(n))*depth); err != nil {
		return err
	}
	sum := map[int]float64{}
	cnt := map[int]int{}
	for _, v := range layer {
		if g.v[v].side != 0 {
			continue // a border's place is fixed: first or last in its group
		}
		k := key(v)
		for _, gr := range g.v[v].path {
			sum[gr] += k
			cnt[gr]++
		}
	}
	type item struct {
		key   float64
		group bool
		id    int
	}
	seq := func(v int) []item {
		p := g.v[v].path
		s := make([]item, 0, len(p)+1)
		for _, gr := range p {
			s = append(s, item{sum[gr] / float64(max(cnt[gr], 1)), true, gr})
		}
		switch g.v[v].side {
		case -1:
			return append(s, item{math.Inf(-1), false, g.v[v].order})
		case 1:
			return append(s, item{math.Inf(1), false, g.v[v].order})
		}
		return append(s, item{key(v), false, g.v[v].order})
	}
	seqs := make(map[int][]item, len(layer))
	for _, v := range layer {
		seqs[v] = seq(v)
	}
	slices.SortStableFunc(layer, func(a, b int) int {
		sa, sb := seqs[a], seqs[b]
		for i := 0; i < len(sa) && i < len(sb); i++ {
			x, y := sa[i], sb[i]
			if x == y {
				continue
			}
			if c := cmp.Compare(x.key, y.key); c != 0 {
				return c
			}
			if x.group != y.group {
				if x.group {
					return 1
				}
				return -1
			}
			return x.id - y.id
		}
		return len(sa) - len(sb)
	})
	g.setOrder(layer)
	return nil
}

// crossings counts the edge crossings between every pair of adjacent ranks: the inversions in
// the lower ends' positions once the edges are sorted by their upper ends.
func (g *graph) crossings(m *meter) (int, error) {
	total := 0
	for r := 0; r+1 < g.ranks; r++ {
		var ends []int
		for _, u := range g.layers[r] {
			start := len(ends)
			for _, v := range g.succs[u] {
				ends = append(ends, g.v[v].order)
			}
			slices.Sort(ends[start:])
		}
		total += inversions(ends, len(g.layers[r+1]))
		if err := m.add(len(ends) + 1); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// inversions counts the pairs i < j with xs[i] > xs[j], for xs in [0, n), with a Fenwick tree.
func inversions(xs []int, n int) int {
	tree := make([]int, n+1)
	inv := 0
	for i, x := range xs {
		// how many already seen are <= x
		le := 0
		for k := x + 1; k > 0; k -= k & -k {
			le += tree[k]
		}
		inv += i - le
		for k := x + 1; k <= n; k += k & -k {
			tree[k]++
		}
	}
	return inv
}
