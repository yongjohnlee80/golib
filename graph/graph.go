package graph

import "iter"

// Edge is a directed edge of a kind, from Src to Dst. Two edges are the same edge when all three
// fields are equal, so one pair of nodes may hold edges of several kinds.
type Edge[N comparable] struct {
	Src, Dst N
	Kind     string
}

// Dir is which way a walk follows edges from a node: out along them, in against them, or both.
type Dir uint8

const (
	// Out follows an edge from its Src to its Dst.
	Out Dir = 1 << iota
	// In follows an edge from its Dst back to its Src.
	In
	// Both follows edges either way.
	Both = Out | In
)

// Filter admits edges by kind. The zero Filter admits every edge.
type Filter struct {
	// Kinds lists the kinds admitted; empty admits every kind.
	Kinds []string
}

func (f Filter) admits(kind string) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	for _, k := range f.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Graph is a directed multigraph whose edges carry a kind. Nodes and edges are kept in the order
// they were added, and every iteration follows it, so the same additions always give the same
// answers. The zero Graph is not usable; call [New]. A Graph is not safe for concurrent use.
type Graph[N comparable] struct {
	index map[N]int // a node's position in nodes, out and in
	nodes []N
	out   [][]Edge[N]
	in    [][]Edge[N]
	edges []Edge[N]
	seen  map[Edge[N]]bool
}

// New returns an empty graph.
func New[N comparable]() *Graph[N] {
	return &Graph[N]{index: map[N]int{}, seen: map[Edge[N]]bool{}}
}

// AddNode adds n with no edges. It reports false when n is already a node. A node added this way
// stays in the graph with no edges: a document nothing links to is still a document.
func (g *Graph[N]) AddNode(n N) bool {
	if _, ok := g.index[n]; ok {
		return false
	}
	g.index[n] = len(g.nodes)
	g.nodes = append(g.nodes, n)
	g.out = append(g.out, nil)
	g.in = append(g.in, nil)
	return true
}

// Add adds e, and its two ends as nodes when they are not yet. It reports false when the graph
// already holds an equal edge.
func (g *Graph[N]) Add(e Edge[N]) bool {
	if g.seen[e] {
		return false
	}
	g.seen[e] = true
	g.AddNode(e.Src)
	g.AddNode(e.Dst)
	g.edges = append(g.edges, e)
	g.out[g.index[e.Src]] = append(g.out[g.index[e.Src]], e)
	g.in[g.index[e.Dst]] = append(g.in[g.index[e.Dst]], e)
	return true
}

// Has reports whether n is a node of the graph.
func (g *Graph[N]) Has(n N) bool {
	_, ok := g.index[n]
	return ok
}

// Nodes yields the nodes in the order they were added.
func (g *Graph[N]) Nodes() iter.Seq[N] {
	return func(yield func(N) bool) {
		for _, n := range g.nodes {
			if !yield(n) {
				return
			}
		}
	}
}

// Edges yields the edges f admits, in the order they were added.
func (g *Graph[N]) Edges(f Filter) iter.Seq[Edge[N]] {
	return admitted(g.edges, f)
}

// Out yields the edges f admits that leave n, in the order they were added; none when n is not a
// node.
func (g *Graph[N]) Out(n N, f Filter) iter.Seq[Edge[N]] {
	i, ok := g.index[n]
	if !ok {
		return admitted[N](nil, f)
	}
	return admitted(g.out[i], f)
}

// In yields the edges f admits that reach n, in the order they were added; none when n is not a
// node.
func (g *Graph[N]) In(n N, f Filter) iter.Seq[Edge[N]] {
	i, ok := g.index[n]
	if !ok {
		return admitted[N](nil, f)
	}
	return admitted(g.in[i], f)
}

// Neighborhood is the part of the graph within depth steps of start, walking the edges f admits
// in direction d: the nodes reached, start included even with no edge, and every admitted edge
// whose two ends were both reached. Nodes keep this graph's order. A start that is not a node
// gives an empty graph, and a depth below 0 counts as 0 (start alone).
func (g *Graph[N]) Neighborhood(start N, depth int, d Dir, f Filter) *Graph[N] {
	sub := New[N]()
	if !g.Has(start) {
		return sub
	}
	reached := map[N]bool{start: true}
	frontier := []N{start}
	for step := 0; step < depth && len(frontier) > 0; step++ {
		var next []N
		visit := func(n N) {
			if !reached[n] {
				reached[n] = true
				next = append(next, n)
			}
		}
		for _, n := range frontier {
			if d&Out != 0 {
				for e := range g.Out(n, f) {
					visit(e.Dst)
				}
			}
			if d&In != 0 {
				for e := range g.In(n, f) {
					visit(e.Src)
				}
			}
		}
		frontier = next
	}
	for _, n := range g.nodes {
		if reached[n] {
			sub.AddNode(n)
		}
	}
	for _, e := range g.edges {
		if reached[e.Src] && reached[e.Dst] && f.admits(e.Kind) {
			sub.Add(e)
		}
	}
	return sub
}

func admitted[N comparable](es []Edge[N], f Filter) iter.Seq[Edge[N]] {
	return func(yield func(Edge[N]) bool) {
		for _, e := range es {
			if f.admits(e.Kind) && !yield(e) {
				return
			}
		}
	}
}
