package layout

import (
	"math"
	"slices"
)

// position gives every vertex its x by Brandes and Köpf's method and its y by rank.
//
// Each of four alignments (upper or lower neighbours, scanning from the left or the right)
// stacks vertices into vertical blocks along median neighbours, then packs the blocks as tightly
// as their separations allow. The four results are shifted onto the narrowest one, and each
// vertex takes the mean of its two middle values, which cancels the bias of any one scan.
func (g *graph) position(m *meter) error {
	conflicts := g.type1Conflicts()
	var xss [4][]float64
	for a := range 4 {
		down, right := a >= 2, a%2 == 1
		layering := g.copyLayers()
		if down {
			slices.Reverse(layering)
		}
		if right {
			for _, l := range layering {
				slices.Reverse(l)
			}
		}
		adj := g.preds
		if down {
			adj = g.succs
		}
		root, align, err := g.verticalAlign(layering, conflicts, adj, m)
		if err != nil {
			return err
		}
		xs, err := g.compact(layering, root, align, m)
		if err != nil {
			return err
		}
		if right {
			for i := range xs {
				xs[i] = -xs[i]
			}
		}
		xss[a] = xs
	}
	g.balance(xss)
	y := 0.0
	gap := g.rankSep
	if g.hasLabels {
		gap /= 2
	}
	for _, l := range g.layers {
		h := 0.0
		for _, v := range l {
			h = max(h, g.v[v].h)
		}
		for _, v := range l {
			g.v[v].y = y + h/2
		}
		y += h + gap
	}
	return nil
}

type pair struct{ a, b int }

func key(a, b int) pair {
	if a > b {
		a, b = b, a
	}
	return pair{a, b}
}

// type1Conflicts marks the segments that cross an inner segment (one between two dummies). The
// alignment then never aligns along a marked segment, so long edges stay straight.
func (g *graph) type1Conflicts() map[pair]bool {
	c := map[pair]bool{}
	for r := 1; r < len(g.layers); r++ {
		prev, layer := g.layers[r-1], g.layers[r]
		k0, scan := 0, 0
		for i, v := range layer {
			w := -1
			if g.v[v].dummy {
				for _, u := range g.preds[v] {
					if g.v[u].dummy {
						w = u
						break
					}
				}
			}
			k1 := len(prev)
			if w >= 0 {
				k1 = g.v[w].order
			}
			if w >= 0 || i == len(layer)-1 {
				for _, s := range layer[scan : i+1] {
					for _, u := range g.preds[s] {
						up := g.v[u].order
						if (up < k0 || k1 < up) && !(g.v[u].dummy && g.v[s].dummy) {
							c[key(u, s)] = true
						}
					}
				}
				scan, k0 = i+1, k1
			}
		}
	}
	return c
}

// verticalAlign stacks each vertex onto a median neighbour on the previous rank of layering,
// left to right, never crossing an alignment already made.
func (g *graph) verticalAlign(layering [][]int, conflicts map[pair]bool, adj [][]int, m *meter) (root, align []int, err error) {
	n := len(g.v)
	root, align = make([]int, n), make([]int, n)
	pos := make([]int, n)
	for v := range n {
		root[v], align[v] = v, v
	}
	for _, l := range layering {
		for i, v := range l {
			pos[v] = i
		}
	}
	for _, l := range layering {
		prev := -1
		for _, v := range l {
			ws := slices.Clone(adj[v])
			if len(ws) == 0 {
				continue
			}
			slices.SortFunc(ws, func(a, b int) int { return pos[a] - pos[b] })
			mp := float64(len(ws)-1) / 2
			for i := int(math.Floor(mp)); i <= int(math.Ceil(mp)); i++ {
				w := ws[i]
				if align[v] == v && prev < pos[w] && !conflicts[key(v, w)] {
					align[w] = v
					root[v] = root[w]
					align[v] = root[v]
					prev = pos[w]
				}
			}
			if err := m.add(len(ws)); err != nil {
				return nil, nil, err
			}
		}
	}
	return root, align, nil
}

// sep is the least distance between the centres of two neighbours on a rank. A group's border
// keeps GroupPad from what is inside the group and the usual gap from what is outside it.
func (g *graph) sep(u, v int) float64 {
	half := func(x int) float64 {
		if g.v[x].dummy && !g.isLabel(x) {
			return g.edgeSep / 2
		}
		return g.nodeSep / 2
	}
	bu, bv := g.v[u].side != 0, g.v[v].side != 0
	space := half(u) + half(v)
	switch {
	case bu && bv:
		if g.inGroup(u, g.v[v].group) || g.inGroup(v, g.v[u].group) {
			space = g.pad
		} else {
			space = g.nodeSep
		}
	case bu:
		space = half(v)
		if g.inGroup(v, g.v[u].group) {
			space = g.pad
		}
	case bv:
		space = half(u)
		if g.inGroup(u, g.v[v].group) {
			space = g.pad
		}
	}
	return (g.v[u].w+g.v[v].w)/2 + space
}

// compact places the blocks: each as far left as its left neighbours allow, then each moved
// right as far as its right neighbours allow, which takes up slack a first pass leaves.
func (g *graph) compact(layering [][]int, root, align []int, m *meter) ([]float64, error) {
	type wedge struct {
		to int
		w  float64
	}
	in := map[int][]wedge{}
	out := map[int][]wedge{}
	idx := map[pair]int{}
	var blocks []int
	isBlock := map[int]bool{}
	addBlock := func(b int) {
		if !isBlock[b] {
			isBlock[b] = true
			blocks = append(blocks, b)
		}
	}
	for _, l := range layering {
		for i, v := range l {
			rv := root[v]
			addBlock(rv)
			if i == 0 {
				continue
			}
			u := l[i-1]
			ru := root[u]
			s := g.sep(u, v)
			if k, ok := idx[pair{ru, rv}]; ok {
				out[ru][k].w = max(out[ru][k].w, s)
				for j := range in[rv] {
					if in[rv][j].to == ru {
						in[rv][j].w = max(in[rv][j].w, s)
					}
				}
				continue
			}
			idx[pair{ru, rv}] = len(out[ru])
			out[ru] = append(out[ru], wedge{rv, s})
			in[rv] = append(in[rv], wedge{ru, s})
		}
	}
	xs := make([]float64, len(g.v))
	iterate := func(set func(int), next func(int) []int) error {
		stack := slices.Clone(blocks)
		visited := map[int]bool{}
		for len(stack) > 0 {
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[e] {
				set(e)
			} else {
				visited[e] = true
				stack = append(stack, e)
				stack = append(stack, next(e)...)
			}
			if err := m.add(1); err != nil {
				return err
			}
		}
		return nil
	}
	ends := func(ws []wedge) []int {
		r := make([]int, len(ws))
		for i, w := range ws {
			r[i] = w.to
		}
		return r
	}
	if err := iterate(func(b int) {
		x := 0.0
		for _, e := range in[b] {
			x = max(x, xs[e.to]+e.w)
		}
		xs[b] = x
	}, func(b int) []int { return ends(in[b]) }); err != nil {
		return nil, err
	}
	if err := iterate(func(b int) {
		lim := math.Inf(1)
		for _, e := range out[b] {
			lim = min(lim, xs[e.to]-e.w)
		}
		if !math.IsInf(lim, 1) {
			xs[b] = max(xs[b], lim)
		}
	}, func(b int) []int { return ends(out[b]) }); err != nil {
		return nil, err
	}
	for v := range g.v {
		xs[v] = xs[root[v]]
	}
	return xs, nil
}

// balance shifts the four alignments onto the narrowest and gives each vertex the mean of its
// two middle values.
func (g *graph) balance(xss [4][]float64) {
	bounds := func(xs []float64) (lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for v, x := range xs {
			lo = min(lo, x-g.v[v].w/2)
			hi = max(hi, x+g.v[v].w/2)
		}
		return lo, hi
	}
	narrow, best := 0, math.Inf(1)
	for a, xs := range xss {
		lo, hi := bounds(xs)
		if hi-lo < best {
			narrow, best = a, hi-lo
		}
	}
	minMax := func(xs []float64) (lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for _, x := range xs {
			lo, hi = min(lo, x), max(hi, x)
		}
		return lo, hi
	}
	alo, ahi := minMax(xss[narrow])
	for a, xs := range xss {
		if a == narrow {
			continue
		}
		lo, hi := minMax(xs)
		delta := alo - lo
		if a%2 == 1 {
			delta = ahi - hi
		}
		for i := range xs {
			xs[i] += delta
		}
	}
	for v := range g.v {
		vals := []float64{xss[0][v], xss[1][v], xss[2][v], xss[3][v]}
		slices.Sort(vals)
		g.v[v].x = (vals[1] + vals[2]) / 2
	}
}
