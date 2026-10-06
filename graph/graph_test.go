package graph

import (
	"fmt"
	"slices"
	"testing"
)

type edge = Edge[string]

func collect[T any](seq func(func(T) bool)) []T {
	var out []T
	for v := range seq {
		out = append(out, v)
	}
	return out
}

// sample is a small document graph:
//
//	a --wikilink--> b --wikilink--> c --supersedes--> d
//	a --supersedes--> b             e (alone)
func sample() *Graph[string] {
	g := New[string]()
	g.Add(edge{"a", "b", "wikilink"})
	g.Add(edge{"b", "c", "wikilink"})
	g.Add(edge{"c", "d", "supersedes"})
	g.Add(edge{"a", "b", "supersedes"})
	g.AddNode("e")
	return g
}

func TestAddKeepsOneEdgePerSrcDstKind(t *testing.T) {
	t.Parallel()
	g := New[string]()
	if !g.Add(edge{"a", "b", "wikilink"}) {
		t.Fatal("the first edge was refused")
	}
	if g.Add(edge{"a", "b", "wikilink"}) {
		t.Error("an equal edge was added twice")
	}
	if !g.Add(edge{"a", "b", "supersedes"}) {
		t.Error("an edge of another kind between the same nodes was refused")
	}
	if !g.Add(edge{"b", "a", "wikilink"}) {
		t.Error("the reverse edge was refused")
	}
	if got := len(collect(g.Edges(Filter{}))); got != 3 {
		t.Errorf("%d edges, want 3", got)
	}
	if got := collect(g.Nodes()); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("nodes %v, want [a b]: an edge's ends become nodes once", got)
	}
}

func TestAddNodeKeepsAnIsolatedNode(t *testing.T) {
	t.Parallel()
	g := New[string]()
	if !g.AddNode("x") {
		t.Fatal("AddNode refused a new node")
	}
	if g.AddNode("x") {
		t.Error("AddNode added a node twice")
	}
	g.Add(edge{"x", "y", "wikilink"})
	if g.AddNode("y") {
		t.Error("an edge's end was not a node")
	}
	if !g.Has("x") || !g.Has("y") || g.Has("z") {
		t.Errorf("Has: x %v, y %v, z %v; want true, true, false", g.Has("x"), g.Has("y"), g.Has("z"))
	}
}

func TestOutAndInFilterByKindInOrder(t *testing.T) {
	t.Parallel()
	g := sample()
	if got := collect(g.Out("a", Filter{})); !slices.Equal(got, []edge{{"a", "b", "wikilink"}, {"a", "b", "supersedes"}}) {
		t.Errorf("Out(a) = %v, want both edges in the order added", got)
	}
	if got := collect(g.Out("a", Filter{Kinds: []string{"supersedes"}})); !slices.Equal(got, []edge{{"a", "b", "supersedes"}}) {
		t.Errorf("Out(a, supersedes) = %v", got)
	}
	if got := collect(g.In("b", Filter{Kinds: []string{"wikilink"}})); !slices.Equal(got, []edge{{"a", "b", "wikilink"}}) {
		t.Errorf("In(b, wikilink) = %v", got)
	}
	if got := collect(g.In("a", Filter{})); len(got) != 0 {
		t.Errorf("In(a) = %v, want none", got)
	}
	if got := collect(g.Out("missing", Filter{})); len(got) != 0 {
		t.Errorf("Out of a node not in the graph = %v, want none", got)
	}
	if got := collect(g.Edges(Filter{Kinds: []string{"supersedes"}})); !slices.Equal(got, []edge{{"c", "d", "supersedes"}, {"a", "b", "supersedes"}}) {
		t.Errorf("Edges(supersedes) = %v, want the two in the order added", got)
	}
}

func TestNeighborhood(t *testing.T) {
	t.Parallel()
	g := sample()
	for _, c := range []struct {
		name  string
		start string
		depth int
		dir   Dir
		f     Filter
		nodes []string
		edges []edge
	}{
		{"depth 1, both ways", "b", 1, Both, Filter{},
			[]string{"a", "b", "c"},
			[]edge{{"a", "b", "wikilink"}, {"b", "c", "wikilink"}, {"a", "b", "supersedes"}}},
		{"depth 2, both ways", "b", 2, Both, Filter{},
			[]string{"a", "b", "c", "d"},
			[]edge{{"a", "b", "wikilink"}, {"b", "c", "wikilink"}, {"c", "d", "supersedes"}, {"a", "b", "supersedes"}}},
		{"out only", "b", 2, Out, Filter{},
			[]string{"b", "c", "d"},
			[]edge{{"b", "c", "wikilink"}, {"c", "d", "supersedes"}}},
		{"in only", "c", 2, In, Filter{},
			[]string{"a", "b", "c"},
			[]edge{{"a", "b", "wikilink"}, {"b", "c", "wikilink"}, {"a", "b", "supersedes"}}},
		// the kind filter stops the walk, not only the answer: c is reached by a wikilink alone
		{"one kind", "b", 2, Both, Filter{Kinds: []string{"supersedes"}},
			[]string{"a", "b"},
			[]edge{{"a", "b", "supersedes"}}},
		{"an isolated start", "e", 2, Both, Filter{},
			[]string{"e"}, nil},
		{"depth 0", "b", 0, Both, Filter{},
			[]string{"b"}, nil},
		{"a start not in the graph", "zzz", 2, Both, Filter{},
			nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sub := g.Neighborhood(c.start, c.depth, c.dir, c.f)
			if got := collect(sub.Nodes()); !slices.Equal(got, c.nodes) {
				t.Errorf("nodes %v, want %v", got, c.nodes)
			}
			if got := collect(sub.Edges(Filter{})); !slices.Equal(got, c.edges) {
				t.Errorf("edges %v, want %v", got, c.edges)
			}
		})
	}
}

// TestNeighborhoodStopsAtDepth: depth 1 does not reach a node two steps away, even through a
// node it does reach.
func TestNeighborhoodStopsAtDepth(t *testing.T) {
	t.Parallel()
	sub := sample().Neighborhood("a", 1, Out, Filter{})
	if sub.Has("c") {
		t.Errorf("c is two steps from a and was reached at depth 1: %v", collect(sub.Nodes()))
	}
	if !sub.Has("b") {
		t.Error("b, one step from a, was not reached")
	}
}

// TestIterationStops: an iteration broken off yields nothing more.
func TestIterationStops(t *testing.T) {
	t.Parallel()
	g := sample()
	n := 0
	for range g.Edges(Filter{}) {
		n++
		break
	}
	for range g.Nodes() {
		n++
		break
	}
	if n != 2 {
		t.Errorf("%d yields after break, want 2", n)
	}
}

func Example() {
	g := New[string]()
	g.Add(Edge[string]{Src: "adrs/0002.md", Dst: "adrs/0001.md", Kind: "supersedes"})
	g.Add(Edge[string]{Src: "notes/a.md", Dst: "adrs/0002.md", Kind: "wikilink"})
	g.AddNode("notes/orphan.md")

	near := g.Neighborhood("adrs/0002.md", 1, Both, Filter{Kinds: []string{"supersedes"}})
	for e := range near.Edges(Filter{}) {
		fmt.Println(e.Src, e.Kind, e.Dst)
	}
	// Output: adrs/0002.md supersedes adrs/0001.md
}
