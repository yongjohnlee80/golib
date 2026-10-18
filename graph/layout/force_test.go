package layout

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// clusters is two cliques of size n, nodes 0..n-1 and n..2n-1, with no edge between them.
func clusters(n int) ForceInput {
	in := ForceInput{Nodes: 2 * n, Seed: 7}
	for c := range 2 {
		for i := range n {
			for j := i + 1; j < n; j++ {
				in.Edges = append(in.Edges, Edge{From: c*n + i, To: c*n + j})
			}
		}
	}
	return in
}

func dist(a, b Point) float64 { return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)) }

// One input and one seed give one layout, whatever order its edges come in; another seed another.
func TestForceIsReproducible(t *testing.T) {
	in := clusters(8)
	a, err := Force(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	shuffled := in
	shuffled.Edges = append([]Edge(nil), in.Edges...)
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(shuffled.Edges), func(i, j int) {
		shuffled.Edges[i], shuffled.Edges[j] = shuffled.Edges[j], shuffled.Edges[i]
	})
	for k := range shuffled.Edges[:len(shuffled.Edges)/2] { // and some reversed
		e := &shuffled.Edges[k]
		e.From, e.To = e.To, e.From
	}
	b, err := Force(context.Background(), shuffled, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Nodes {
		if a.Nodes[i] != b.Nodes[i] {
			t.Fatalf("node %d at %v, and at %v with the edges reordered", i, a.Nodes[i], b.Nodes[i])
		}
	}
	other := in
	other.Seed = 8
	c, err := Force(context.Background(), other, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	same := true
	for i := range a.Nodes {
		same = same && a.Nodes[i] == c.Nodes[i]
	}
	if same {
		t.Fatal("another seed placed every node the same")
	}
}

// Linked nodes settle together: two cliques with no edge between them end farther apart than
// their own members, and a path's neighbours nearer than its ends.
func TestForcePullsLinkedNodesTogether(t *testing.T) {
	const n = 10
	r, err := Force(context.Background(), clusters(n), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var in, across float64
	var nin, nacross int
	for i := range 2 * n {
		for j := i + 1; j < 2*n; j++ {
			d := dist(r.Nodes[i], r.Nodes[j])
			if i/n == j/n {
				in, nin = in+d, nin+1
			} else {
				across, nacross = across+d, nacross+1
			}
		}
	}
	if in/float64(nin) >= across/float64(nacross) {
		t.Fatalf("mean distance within a clique %.1f, across %.1f; want within < across", in/float64(nin), across/float64(nacross))
	}

	path := ForceInput{Nodes: 12, Seed: 3}
	for i := range 11 {
		path.Edges = append(path.Edges, Edge{From: i, To: i + 1})
	}
	p, err := Force(context.Background(), path, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var step float64
	for i := range 11 {
		step = max(step, dist(p.Nodes[i], p.Nodes[i+1]))
	}
	if ends := dist(p.Nodes[0], p.Nodes[11]); ends <= step {
		t.Fatalf("a path's ends %.1f apart, its longest step %.1f; want the ends farther", ends, step)
	}
}

func TestForceRefusesWhatItCannotPlace(t *testing.T) {
	bg := context.Background()
	cases := []struct {
		name string
		in   ForceInput
		lim  Limits
		want error
	}{
		{"negative count", ForceInput{Nodes: -1}, Limits{}, ErrInvalid},
		{"edge past the nodes", ForceInput{Nodes: 2, Edges: []Edge{{From: 0, To: 2}}}, Limits{}, ErrInvalid},
		{"negative endpoint", ForceInput{Nodes: 2, Edges: []Edge{{From: -1, To: 1}}}, Limits{}, ErrInvalid},
		{"too many nodes", ForceInput{Nodes: 11}, Limits{MaxNodes: 10}, ErrTooLarge},
		{"too many edges", ForceInput{Nodes: 3, Edges: []Edge{{From: 0, To: 1}, {From: 1, To: 2}}}, Limits{MaxEdges: 1}, ErrTooLarge},
		{"too much work", clusters(8), Limits{MaxWork: 100}, ErrTooLarge},
	}
	for _, c := range cases {
		if _, err := Force(bg, c.in, c.lim); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; want %v", c.name, err, c.want)
		}
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := Force(ctx, clusters(4), Limits{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled before it starts: %v; want context.Canceled", err)
	}
	// Cancelled while it runs: a big graph, the context ended a moment in.
	big := workspaceGraph(3000, 6000)
	ctx, cancel = context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if _, err := Force(ctx, big, Limits{MaxNodes: 20000, MaxEdges: 100000, MaxWork: math.MaxInt}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancelled while running: %v; want context.DeadlineExceeded", err)
	}
}

func TestForcePlacesEmptyAndSingleGraphs(t *testing.T) {
	r, err := Force(context.Background(), ForceInput{}, Limits{})
	if err != nil || len(r.Nodes) != 0 || r.Size != (Size{}) {
		t.Fatalf("empty: %+v, %v", r, err)
	}
	r, err = Force(context.Background(), ForceInput{Nodes: 1, Edges: []Edge{{From: 0, To: 0}}}, Limits{})
	if err != nil || len(r.Nodes) != 1 || r.Nodes[0] != (Point{}) || r.Size != (Size{}) {
		t.Fatalf("one node with a self-loop: %+v, %v; want it at the origin, no extent", r, err)
	}
}

// workspaceGraph is n nodes and e seeded random edges: a knowledge base's shape, roughly.
func workspaceGraph(n, e int) ForceInput {
	in := ForceInput{Nodes: n, Seed: 11}
	rng := rand.New(rand.NewPCG(5, 9))
	for range e {
		in.Edges = append(in.Edges, Edge{From: rng.IntN(n), To: rng.IntN(n)})
	}
	return in
}

// A knowledge base the size of GlobalKB (2.2k documents) places within the cell's bound, every
// coordinate finite, with the limits a caller of that size passes.
func TestForcePlacesAWorkspaceSizedGraph(t *testing.T) {
	in := workspaceGraph(2200, 5000)
	start := time.Now()
	r, err := Force(context.Background(), in, Limits{MaxNodes: 20000, MaxEdges: 100000, MaxWork: 500_000_000})
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	for i, p := range r.Nodes {
		if math.IsNaN(float64(p.X)) || math.IsInf(float64(p.X), 0) || math.IsNaN(float64(p.Y)) || math.IsInf(float64(p.Y), 0) {
			t.Fatalf("node %d at %v", i, p)
		}
	}
	if r.Size.W <= 0 || r.Size.H <= 0 {
		t.Fatalf("extent %v", r.Size)
	}
	if took > 30*time.Second {
		t.Fatalf("placing 2200 nodes took %v; want under 30s", took)
	}
	t.Logf("2200 nodes, 5000 edges placed in %v; extent %.0f×%.0f", took, r.Size.W, r.Size.H)
}
