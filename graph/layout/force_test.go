package layout

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"slices"
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
		{"NaN spacing", ForceInput{Nodes: 8, Steps: 1, Spacing: float32(math.NaN())}, Limits{}, ErrInvalid},
		{"infinite spacing", ForceInput{Nodes: 8, Steps: 1, Spacing: float32(math.Inf(1))}, Limits{}, ErrInvalid},
		{"NaN theta", ForceInput{Nodes: 8, Steps: 1, Theta: float32(math.NaN())}, Limits{}, ErrInvalid},
		{"infinite theta", ForceInput{Nodes: 8, Steps: 1, Theta: float32(math.Inf(-1))}, Limits{}, ErrInvalid},
		{"NaN reach", ForceInput{Nodes: 8, Steps: 1, Reach: float32(math.NaN())}, Limits{}, ErrInvalid},
		{"infinite reach", ForceInput{Nodes: 8, Steps: 1, Reach: float32(math.Inf(1))}, Limits{}, ErrInvalid},
		{"NaN link pull", ForceInput{Nodes: 8, Steps: 1, LinkPull: float32(math.NaN())}, Limits{}, ErrInvalid},
		{"infinite link pull", ForceInput{Nodes: 8, Steps: 1, LinkPull: float32(math.Inf(1))}, Limits{}, ErrInvalid},
		{"a layout past float32", ForceInput{Nodes: 8, Steps: 1, Seed: 7, Spacing: math.MaxFloat32}, Limits{}, ErrTooLarge},
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

// A simulation stepped in any chunks reaches the same frames as one stepped all at once, and its
// last frame, moved to start at (0, 0), is Force's layout.
func TestSimulationStepsToForcesLayout(t *testing.T) {
	in := clusters(9)
	in.Steps = 120
	whole, err := NewSimulation(in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if done, err := whole.Step(context.Background(), in.Steps); err != nil || !done || !whole.Done() {
		t.Fatalf("stepped to the end: done %v, %v", done, err)
	}
	chunked, _ := NewSimulation(in, Limits{})
	steps := 0
	for _, n := range []int{1, 7, 0, 33, 50, 1000} {
		done, err := chunked.Step(context.Background(), n)
		if err != nil {
			t.Fatal(err)
		}
		steps = min(steps+n, in.Steps)
		if done != (steps == in.Steps) {
			t.Fatalf("after %d steps done is %v", steps, done)
		}
	}
	a, ab, err := whole.Frame(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, bb, _ := chunked.Frame(make([]Point, 3))
	if ab != bb || !slices.Equal(a, b) {
		t.Fatal("chunked steps reached another layout")
	}
	f, err := Force(context.Background(), in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range a {
		want := Point{X: f.Nodes[i].X + ab.X, Y: f.Nodes[i].Y + ab.Y}
		if math.Abs(float64(p.X-want.X)) > 1e-2 || math.Abs(float64(p.Y-want.Y)) > 1e-2 {
			t.Fatalf("node %d at %v in the frame, %v from Force", i, p, want)
		}
	}
	if math.Abs(float64(f.Size.W-ab.W)) > 1e-2 || math.Abs(float64(f.Size.H-ab.H)) > 1e-2 {
		t.Fatalf("Force's extent %v, the frame's %v", f.Size, ab)
	}
	if done, err := whole.Step(context.Background(), 5); !done || err != nil {
		t.Fatalf("a step past the end: %v, %v", done, err)
	}
	if c, _, _ := whole.Frame(nil); !slices.Equal(c, a) {
		t.Fatal("a step past the end moved a node")
	}
}

// Each frame on the way is finite and holds every node within its bounds; the settled frame is not
// the seeded start.
func TestSimulationFramesAreFinite(t *testing.T) {
	s, err := NewSimulation(workspaceGraph(300, 600), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	start, _, _ := s.Frame(nil)
	var at []Point
	for !s.Done() {
		var b Rect
		at, b, err = s.Frame(at)
		if err != nil {
			t.Fatal(err)
		}
		// the bounds are in float32 too: X+W rounds, so a node at the edge may sit past it by a few ulps
		eps := 1e-5 * max(abs32(b.X)+b.W, abs32(b.Y)+b.H, 1)
		for i, p := range at {
			if !finite32(p.X) || !finite32(p.Y) || p.X < b.X-eps || p.Y < b.Y-eps || p.X > b.X+b.W+eps || p.Y > b.Y+b.H+eps {
				t.Fatalf("node %d at %v, the frame's bounds %v", i, p, b)
			}
		}
		if _, err := s.Step(context.Background(), 20); err != nil {
			t.Fatal(err)
		}
	}
	if end, _, _ := s.Frame(at); slices.Equal(end, start) {
		t.Error("the settled frame is the seeded start: no step moved a node")
	}
	empty, err := NewSimulation(ForceInput{}, Limits{})
	if err != nil || !empty.Done() {
		t.Fatalf("no nodes: %v, done %v", err, empty.Done())
	}
	if at, b, err := empty.Frame(nil); len(at) != 0 || b != (Rect{}) || err != nil {
		t.Fatalf("no nodes' frame: %v %v %v", at, b, err)
	}
}

// A simulation refuses what Force refuses, counts its work across every Step against MaxWork, and
// stops at a cancelled context.
func TestSimulationKeepsForcesLimits(t *testing.T) {
	if _, err := NewSimulation(ForceInput{Nodes: 2, Edges: []Edge{{From: 0, To: 5}}}, Limits{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("an edge past the nodes: %v", err)
	}
	if _, err := NewSimulation(ForceInput{Nodes: 4, Spacing: float32(math.NaN())}, Limits{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("NaN spacing: %v", err)
	}
	s, err := NewSimulation(clusters(8), Limits{MaxWork: 3000})
	if err != nil {
		t.Fatal(err)
	}
	var stepErr error
	for i := 0; i < 1000 && stepErr == nil && !s.Done(); i++ {
		_, stepErr = s.Step(context.Background(), 1) // one step at a time: the work still adds up
	}
	if !errors.Is(stepErr, ErrTooLarge) {
		t.Errorf("stepping one at a time past MaxWork: %v", stepErr)
	}
	big, err := NewSimulation(ForceInput{Nodes: 8, Steps: 1, Seed: 7, Spacing: math.MaxFloat32}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := big.Step(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if at, _, err := big.Frame(nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a frame past float32: %v, %v; want ErrTooLarge", at, err)
	}
	// a finite origin and a finite span whose far end is past float32: each point is checked
	far, err := NewSimulation(ForceInput{Nodes: 2, Seed: 23, Spacing: math.MaxFloat32}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if at, b, err := far.Frame(nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a frame with a point past float32: %v %v, %v; want ErrTooLarge", at, b, err)
	}
	// every point finite, their span past float32: the bounds are checked too
	wide, err := NewSimulation(ForceInput{Nodes: 2, Seed: 49, Spacing: 1.5e38}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if at, b, err := wide.Frame(nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a frame whose span is past float32: %v %v, %v; want ErrTooLarge", at, b, err)
	}
	// a context that ends while a step runs: the meter sees it as the work adds up
	long, err := NewSimulation(workspaceGraph(3000, 6000), Limits{MaxNodes: 20000, MaxEdges: 100000, MaxWork: math.MaxInt})
	if err != nil {
		t.Fatal(err)
	}
	soon, stopSoon := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopSoon()
	if _, err := long.Step(soon, 300); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a step outliving its context: %v; want context.DeadlineExceeded", err)
	}
	// a cancelled continuation: the meter has counted work already, so only an entry check sees it
	cont, err := NewSimulation(ForceInput{Nodes: 1, Steps: 3}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cont.Step(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	before, _, _ := cont.Frame(nil)
	cancelled, stopIt := context.WithCancel(context.Background())
	stopIt()
	if done, err := cont.Step(cancelled, 1); !errors.Is(err, context.Canceled) || done {
		t.Errorf("a cancelled continuation: done %v, %v; want context.Canceled", done, err)
	}
	if after, _, _ := cont.Frame(nil); !slices.Equal(after, before) || cont.step != 1 {
		t.Errorf("a cancelled continuation moved the simulation: %v → %v, step %d", before, after, cont.step)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := NewSimulation(clusters(4), Limits{})
	if _, err := c.Step(ctx, 10); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled step: %v", err)
	}
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

// looseAndCore is a linked core of n nodes (each joined to two earlier ones) and loose more nodes
// no edge holds, after them.
func looseAndCore(n, loose int) ForceInput {
	in := ForceInput{Nodes: n + loose, Seed: 3}
	rng := rand.New(rand.NewPCG(8, 1))
	for i := 1; i < n; i++ {
		in.Edges = append(in.Edges, Edge{From: i, To: rng.IntN(i)})
		if i > 2 {
			in.Edges = append(in.Edges, Edge{From: i, To: rng.IntN(i)})
		}
	}
	return in
}

// spread is how far the loose nodes sit from the core, against the core's own size: the loose
// nodes' median distance from the core's centre over the core's 90th-percentile radius.
func spread(r ForceResult, core int) float64 {
	var cx, cy float64
	for _, p := range r.Nodes[:core] {
		cx, cy = cx+float64(p.X), cy+float64(p.Y)
	}
	cx, cy = cx/float64(core), cy/float64(core)
	radii := func(ps []Point) []float64 {
		out := make([]float64, len(ps))
		for i, p := range ps {
			out[i] = math.Hypot(float64(p.X)-cx, float64(p.Y)-cy)
		}
		slices.Sort(out)
		return out
	}
	in, out := radii(r.Nodes[:core]), radii(r.Nodes[core:])
	return out[len(out)/2] / in[len(in)*9/10]
}

// Within a reach, and with linear links, the nodes no edge holds settle close round the linked
// core, where without them every node pushes them out to a far ring the gravity alone holds.
func TestForceReachKeepsLooseNodesNear(t *testing.T) {
	const core, loose = 600, 200
	lim := Limits{MaxNodes: 20000, MaxEdges: 100000, MaxWork: math.MaxInt}
	place := func(reach, pull float32) float64 {
		in := looseAndCore(core, loose)
		in.Reach, in.LinkPull = reach, pull
		r, err := Force(context.Background(), in, lim)
		if err != nil {
			t.Fatal(err)
		}
		return spread(r, core)
	}
	far, near := place(0, 0), place(8, 0.5)
	t.Logf("loose nodes' median distance over the core's r90: %.2f unlimited, %.2f within reach 8, link pull 0.5", far, near)
	if near > 2 || near > far/2 {
		t.Errorf("within reach: %.2f; unlimited %.2f; want under 2 and under half", near, far)
	}
	if reachOnly := place(8, 0); reachOnly > far/2 {
		t.Errorf("reach alone: %.2f; unlimited %.2f; want under half", reachOnly, far)
	}
}

// A node pushes only what is within its reach: a neighbour inside it as without one, and a far
// group not at all, whether the walk meets it as one body or opens its cell; a cell wholly out of
// reach is not opened.
func TestRepulsionStopsAtTheReach(t *testing.T) {
	pos := []vec{{0, 0}, {50, 0}}
	for i := range 20 {
		pos = append(pos, vec{1000 + float64(i%5), 1000 + float64(i/5)})
	}
	var tr quadTree
	tr.build(pos)
	const k2 = 900
	for _, theta := range []float64{0.9, 0} { // 0: every cell opened, to its leaves
		got, _ := tr.repulse(0, pos[0], theta, k2, 100)
		if want := (vec{-k2 / 50, 0}); math.Abs(got.x-want.x) > 1e-9 || math.Abs(got.y) > 1e-9 {
			t.Errorf("theta %v, reach 100: %v; want the neighbour's push alone, %v", theta, got, want)
		}
		// unlimited, the far group pushes too: down and left, where the neighbour pushes along x
		// alone (at theta 0.9 the walk may take the neighbour into one body with the rest)
		if all, _ := tr.repulse(0, pos[0], theta, k2, 0); all.y >= 0 {
			t.Errorf("theta %v, unlimited: %v; want the far group's push too", theta, all)
		}
		if none, _ := tr.repulse(1, pos[1], theta, k2, 40); none != (vec{}) {
			t.Errorf("theta %v, reach 40, neighbour 50 away: %v; want no push", theta, none)
		}
	}
	// a cell wholly out of reach is not walked into: the far group costs nothing
	_, near := tr.repulse(0, pos[0], 0, k2, 100)
	_, every := tr.repulse(0, pos[0], 0, k2, 0)
	if near*2 > every {
		t.Errorf("every cell opened: %d cells walked within reach 100, %d without; want the far group skipped", near, every)
	}
}

// Reach is in Spacings: two nodes no edge joins push apart only within it, so the gravity draws
// them in to its edge, where unlimited they rest far out.
func TestForceReachIsInSpacings(t *testing.T) {
	apart := func(reach float32) float64 {
		r, err := Force(context.Background(), ForceInput{Nodes: 2, Seed: 1, Reach: reach}, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		return dist(r.Nodes[0], r.Nodes[1])
	}
	// within reach 2 (60 at Spacing 30) the push wins; past it only the gravity acts
	if d := apart(2); d < 45 || d > 75 {
		t.Errorf("reach 2: %.1f apart; want near the reach, 60", d)
	}
	if d := apart(0); d < 150 {
		t.Errorf("unlimited: %.1f apart; want far out, where the push meets the gravity", d)
	}
}

// A linear link settles where its spring meets the push and the gravity: two nodes, one edge, at
// Spacing over the square root of the pull (and the gravity's share); the d² pull, near Spacing.
func TestForceLinkPullIsASpring(t *testing.T) {
	length := func(pull float32) float64 {
		r, err := Force(context.Background(), ForceInput{Nodes: 2, Edges: []Edge{{From: 0, To: 1}}, Seed: 1, LinkPull: pull}, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		return dist(r.Nodes[0], r.Nodes[1])
	}
	// on each node: k²/l out, pull·l in, gravity 0.02·l/2 in: l = k/sqrt(pull+0.01)
	want := 30 / math.Sqrt(0.25+0.01)
	if l := length(0.25); math.Abs(l-want) > want*0.05 {
		t.Errorf("link pull 0.25: length %.2f; want %.2f", l, want)
	}
	if l := length(0); l > 45 {
		t.Errorf("the d² pull: length %.2f; want near the spacing, 30", l)
	}
}
