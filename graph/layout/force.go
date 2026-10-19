package layout

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
)

// ForceInput is a graph to place by force: Nodes are counted (indexes 0 to Nodes-1), and Edges
// join them by From and To. The layered fields of Edge, MinLen and Label, are ignored.
type ForceInput struct {
	Nodes int
	Edges []Edge
	// Seed seeds the initial placement: one input and one seed give one layout, whatever order
	// the edges come in.
	Seed int64
	// Steps is how many simulation steps run; 0 means 300.
	Steps int
	// Theta is the Barnes–Hut opening angle: a cell farther than its width over Theta acts as one
	// body. 0 means 0.9. Smaller is more exact and slower.
	Theta float32
	// Spacing is the length an edge settles near; 0 means 30.
	Spacing float32
	// Reach is how far a node pushes others, in Spacings; 0 or less means without limit. A finite
	// reach keeps a graph of many parts compact: a node no edge holds settles near the rest, where
	// unlimited, every node pushes it out against the gravity alone.
	Reach float32
	// LinkPull makes each edge a linear spring of that strength (|F| = LinkPull·d); 0 or less means
	// the pull grows with the square of the length (|F| = d²/Spacing). A linear pull spreads a dense
	// part out rather than drawing it tight. Theta, Spacing, Reach and LinkPull must be finite.
	LinkPull float32
}

// ForceResult is each node's centre and the extent of all of them. Coordinates start at (0, 0).
type ForceResult struct {
	Nodes []Point
	Size  Size
}

// Force places in by a seeded force simulation: every edge pulls its ends together, every pair of
// nodes pushes apart (by a Barnes–Hut quadtree, O(n log n) a step; within Reach when it is set), a
// weak gravity keeps unconnected parts near the centre, and the most a node may move shrinks step by
// step. It refuses an edge to a node that does not exist, or a Theta, Spacing, Reach or LinkPull
// that is NaN or infinite, with
// ErrInvalid; with ErrTooLarge more nodes or edges than lim allows (MaxNodes, MaxEdges), more work
// than lim.MaxWork, or a layout float32 cannot hold (a Spacing near its largest). It charges its
// work, and checks ctx, after each node's repulsion and after each step's edges: building a
// step's quadtree is not charged, so one charge may follow more than 1024 tree operations.
func Force(ctx context.Context, in ForceInput, lim Limits) (ForceResult, error) {
	s, err := NewSimulation(in, lim)
	if err != nil {
		return ForceResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ForceResult{}, err
	}
	for done := false; !done; {
		if done, err = s.Step(ctx, s.steps); err != nil {
			return ForceResult{}, err
		}
	}
	if len(s.pos) == 0 {
		return ForceResult{}, nil
	}
	// moved to start at (0, 0) in float64, then narrowed: each coordinate rounds once
	minX, minY, maxX, maxY := s.extent()
	out := ForceResult{Nodes: make([]Point, len(s.pos)), Size: Size{W: float32(maxX - minX), H: float32(maxY - minY)}}
	if !finite32(out.Size.W) || !finite32(out.Size.H) {
		return ForceResult{}, fmt.Errorf("%w: a layout %g by %g, past float32", ErrTooLarge, maxX-minX, maxY-minY)
	}
	for i, p := range s.pos {
		out.Nodes[i] = Point{X: float32(p.x - minX), Y: float32(p.y - minY)}
	}
	return out, nil
}

// finite32 reports whether v is a number, and not an infinity.
func finite32(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

// Simulation is Force a step at a time, for a caller that shows the layout as it settles: the same
// seeded start, forces, cooling, validation and limits. Stepped to the end, its Frame is Force's
// layout before Force moves it to start at (0, 0). Not safe for concurrent use.
type Simulation struct {
	edges       [][2]int
	pos, disp   []vec
	tree        quadTree
	m           meter
	step, steps int
	radius      float64
	theta, k2   float64
	spacing     float64
	reach       float64 // how far a node pushes, in layout units; 0 without limit
	pull        float64 // a linear spring's strength; 0 the d²/spacing pull
}

// NewSimulation validates in as Force does (ErrInvalid, ErrTooLarge) and places its seeded start:
// every node at random in a disc about the origin, as wide as the graph will roughly settle to.
func NewSimulation(in ForceInput, lim Limits) (*Simulation, error) {
	lim = lim.withDefaults()
	if err := forceValidate(in, lim); err != nil {
		return nil, err
	}
	n := in.Nodes
	s := &Simulation{steps: in.Steps, theta: float64(in.Theta), spacing: float64(in.Spacing), m: meter{max: lim.MaxWork}}
	if s.steps <= 0 {
		s.steps = 300
	}
	if s.theta <= 0 {
		s.theta = 0.9
	}
	if s.spacing <= 0 {
		s.spacing = 30
	}
	s.reach = max(float64(in.Reach), 0) * s.spacing
	s.pull = max(float64(in.LinkPull), 0)
	if n == 0 {
		s.step = s.steps // nothing to place: done before it starts
		return s, nil
	}

	// Edges in a canonical order, self-loops and duplicates dropped: their order in the input does
	// not change the sums, so it does not change the layout.
	s.edges = make([][2]int, 0, len(in.Edges))
	for _, e := range in.Edges {
		a, b := min(e.From, e.To), max(e.From, e.To)
		if a != b {
			s.edges = append(s.edges, [2]int{a, b})
		}
	}
	slices.SortFunc(s.edges, func(x, y [2]int) int { return cmp.Or(cmp.Compare(x[0], y[0]), cmp.Compare(x[1], y[1])) })
	s.edges = slices.Compact(s.edges)

	rng := rand.New(rand.NewPCG(uint64(in.Seed), 0x9e3779b97f4a7c15))
	s.radius = s.spacing * math.Sqrt(float64(n))
	s.pos = make([]vec, n)
	for i := range s.pos {
		// Uniform in a disc as wide as the graph will roughly settle to.
		r, a := s.radius*math.Sqrt(rng.Float64()), 2*math.Pi*rng.Float64()
		s.pos[i] = vec{r * math.Cos(a), r * math.Sin(a)}
	}
	s.k2 = s.spacing * s.spacing
	s.disp = make([]vec, n)
	return s, nil
}

// Done reports whether every step has run.
func (s *Simulation) Done() bool { return s.step >= s.steps }

// Step runs up to n more steps, and reports whether every step has now run. It checks ctx first, so
// a cancelled call moves nothing, then as Force does while it runs; it counts its work against the
// limits' MaxWork across every Step. After an error the simulation stays where it stopped.
func (s *Simulation) Step(ctx context.Context, n int) (done bool, err error) {
	// a cancelled call moves nothing: the meter checks ctx only as its work adds up, and its count
	// runs on across calls
	if err := ctx.Err(); err != nil {
		return s.Done(), err
	}
	s.m.ctx = ctx
	const gravity = 0.02
	for ; n > 0 && s.step < s.steps; n-- {
		// The most a node moves this step: a tenth of the starting radius, cooling to nothing.
		heat := s.radius * 0.1 * (1 - float64(s.step)/float64(s.steps))
		clear(s.disp)
		s.tree.build(s.pos)
		for i, p := range s.pos {
			f, work := s.tree.repulse(i, p, s.theta, s.k2, s.reach)
			if err := s.m.add(work); err != nil {
				return false, err
			}
			s.disp[i] = s.disp[i].add(f).add(p.scale(-gravity))
		}
		for _, e := range s.edges {
			d := s.pos[e[1]].sub(s.pos[e[0]])
			l := d.len()
			if l == 0 {
				continue
			}
			pull := d.scale(l / s.spacing) // |F| = d²/k, along d
			if s.pull > 0 {
				pull = d.scale(s.pull) // a linear spring: |F| = pull·d
			}
			s.disp[e[0]] = s.disp[e[0]].add(pull)
			s.disp[e[1]] = s.disp[e[1]].sub(pull)
		}
		if err := s.m.add(len(s.edges)); err != nil {
			return false, err
		}
		for i, d := range s.disp {
			if l := d.len(); l > heat && l > 0 {
				d = d.scale(heat / l)
			}
			s.pos[i] = s.pos[i].add(d)
		}
		s.step++
	}
	return s.Done(), nil
}

// extent is the smallest and largest coordinates of the nodes' positions now.
func (s *Simulation) extent() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range s.pos {
		minX, minY = min(minX, p.x), min(minY, p.y)
		maxX, maxY = max(maxX, p.x), max(maxY, p.y)
	}
	return minX, minY, maxX, maxY
}

// Frame is every node's position now, appended to dst[:0], and the rectangle holding them all; gravity
// keeps them about the origin, so a caller can show successive frames in one view. A position
// float32 cannot hold is ErrTooLarge. No nodes is an empty frame.
func (s *Simulation) Frame(dst []Point) ([]Point, Rect, error) {
	dst = dst[:0]
	if len(s.pos) == 0 {
		return dst, Rect{}, nil
	}
	minX, minY, maxX, maxY := s.extent()
	bounds := Rect{X: float32(minX), Y: float32(minY), W: float32(maxX - minX), H: float32(maxY - minY)}
	if !finite32(bounds.X) || !finite32(bounds.Y) || !finite32(bounds.W) || !finite32(bounds.H) {
		return dst, Rect{}, fmt.Errorf("%w: a layout %g by %g, past float32", ErrTooLarge, maxX-minX, maxY-minY)
	}
	for _, p := range s.pos {
		pt := Point{X: float32(p.x), Y: float32(p.y)}
		if !finite32(pt.X) || !finite32(pt.Y) { // a finite origin and span may still end past float32
			return dst[:0], Rect{}, fmt.Errorf("%w: a node at %g, %g, past float32", ErrTooLarge, p.x, p.y)
		}
		dst = append(dst, pt)
	}
	return dst, bounds, nil
}

// forceValidate refuses an input Force cannot place: a negative count, an edge to a node that does
// not exist, or more nodes or edges than lim allows.
func forceValidate(in ForceInput, lim Limits) error {
	switch {
	case in.Nodes < 0:
		return fmt.Errorf("%w: %d nodes", ErrInvalid, in.Nodes)
	case in.Nodes > lim.MaxNodes:
		return fmt.Errorf("%w: %d nodes, more than %d", ErrTooLarge, in.Nodes, lim.MaxNodes)
	case len(in.Edges) > lim.MaxEdges:
		return fmt.Errorf("%w: %d edges, more than %d", ErrTooLarge, len(in.Edges), lim.MaxEdges)
	case !finite32(in.Theta) || !finite32(in.Spacing) || !finite32(in.Reach) || !finite32(in.LinkPull):
		return fmt.Errorf("%w: theta %v, spacing %v, reach %v, link pull %v", ErrInvalid, in.Theta, in.Spacing, in.Reach, in.LinkPull)
	}
	for i, e := range in.Edges {
		if e.From < 0 || e.From >= in.Nodes || e.To < 0 || e.To >= in.Nodes {
			return fmt.Errorf("%w: edge %d joins %d and %d, of %d nodes", ErrInvalid, i, e.From, e.To, in.Nodes)
		}
	}
	return nil
}

// vec is a point or a displacement in float64, the simulation's precision.
type vec struct{ x, y float64 }

func (a vec) add(b vec) vec       { return vec{a.x + b.x, a.y + b.y} }
func (a vec) sub(b vec) vec       { return vec{a.x - b.x, a.y - b.y} }
func (a vec) scale(s float64) vec { return vec{a.x * s, a.y * s} }
func (a vec) len() float64        { return math.Hypot(a.x, a.y) }
func (a vec) mid(b vec) vec       { return vec{(a.x + b.x) / 2, (a.y + b.y) / 2} }

// quad is the quadrant of c that a falls in: bit 0 east, bit 1 south.
func (a vec) quad(c vec) (q int) {
	if a.x >= c.x {
		q |= 1
	}
	if a.y >= c.y {
		q |= 2
	}
	return q
}
func (a vec) near(b vec) (vec, float64) {
	d := a.sub(b)
	return d, d.len()
}

// quadTree is a Barnes–Hut tree over the nodes' positions: each cell keeps its node count and
// centre of mass, so a far cell pushes as one body. Rebuilt each step, its storage reused.
type quadTree struct {
	cells []qcell
	pos   []vec
	stack []int // repulse's walk, reused
}

type qcell struct {
	lo, hi vec    // the cell's square
	mass   int    // nodes in it
	centre vec    // their mean position
	node   int    // the one node of a leaf; -1 for an inner cell or an empty one
	kids   [4]int // child cells by quadrant; 0 for none (cell 0 is the root, never a child)
}

// farthest is how far p is from the cell's square's farthest corner.
func (c qcell) farthest(p vec) float64 {
	return math.Hypot(max(p.x-c.lo.x, c.hi.x-p.x), max(p.y-c.lo.y, c.hi.y-p.y))
}

// gap is how far p is from the cell's square: 0 inside it.
func (c qcell) gap(p vec) float64 {
	dx := max(c.lo.x-p.x, 0, p.x-c.hi.x)
	dy := max(c.lo.y-p.y, 0, p.y-c.hi.y)
	return math.Hypot(dx, dy)
}

// maxDepth bounds the tree: nodes closer than the root's width over 2^maxDepth share a leaf cell's
// bucket, pushing as one, rather than splitting forever.
const maxDepth = 24

func (t *quadTree) build(pos []vec) {
	t.pos = pos
	t.cells = t.cells[:0]
	lo, hi := pos[0], pos[0]
	for _, p := range pos {
		lo, hi = vec{min(lo.x, p.x), min(lo.y, p.y)}, vec{max(hi.x, p.x), max(hi.y, p.y)}
	}
	side := max(hi.x-lo.x, hi.y-lo.y, 1)
	t.cells = append(t.cells, qcell{lo: lo, hi: lo.add(vec{side, side}), node: -1})
	for i := range pos {
		t.insert(0, i, 0)
	}
	t.mass(0)
}

func (t *quadTree) insert(c, i, depth int) {
	cell := &t.cells[c]
	switch {
	case cell.mass == 0 && cell.kids == [4]int{}:
		cell.node, cell.mass = i, 1
		return
	case cell.kids == [4]int{} && depth < maxDepth:
		// A leaf holding one node splits: its node moves down with the new one.
		old := cell.node
		cell.node = -1
		t.push(c, old, depth)
	}
	cell = &t.cells[c]
	cell.mass++
	if depth >= maxDepth {
		return // a bucket at the bottom: counted, its centre summed in mass()
	}
	t.push(c, i, depth)
}

// push puts node i into the right child of c, making it.
func (t *quadTree) push(c, i, depth int) {
	cell := t.cells[c]
	centre := cell.lo.mid(cell.hi)
	q := t.pos[i].quad(centre)
	if cell.kids[q] == 0 {
		lo, hi := cell.lo, centre
		if q&1 != 0 {
			lo.x, hi.x = centre.x, cell.hi.x
		}
		if q&2 != 0 {
			lo.y, hi.y = centre.y, cell.hi.y
		}
		t.cells = append(t.cells, qcell{lo: lo, hi: hi, node: -1})
		t.cells[c].kids[q] = len(t.cells) - 1
	}
	t.insert(t.cells[c].kids[q], i, depth+1)
}

// mass sets each cell's centre of mass from its children, bottom up; the mass counts were set as
// nodes went in. A leaf is its node's position, with the leaf's whole count: a bucket at maxDepth
// holds nodes too close to tell apart.
func (t *quadTree) mass(c int) (sum vec, n int) {
	cell := &t.cells[c]
	if cell.node >= 0 {
		cell.centre = t.pos[cell.node]
		return cell.centre, cell.mass
	}
	for _, k := range cell.kids {
		if k != 0 {
			s, m := t.mass(k)
			sum, n = sum.add(s.scale(float64(m))), n+m
		}
	}
	if n > 0 {
		t.cells[c].centre = sum.scale(1 / float64(n))
		return t.cells[c].centre, n
	}
	return vec{}, 0
}

// repulse is the push on node i at p from every other node, k2/d along their separation, a far
// cell acting as one body of its mass. work is the cells visited.
func (t *quadTree) repulse(i int, p vec, theta, k2, reach float64) (f vec, work int) {
	stack := append(t.stack[:0], 0)
	defer func() { t.stack = stack }()
	for len(stack) > 0 {
		c := t.cells[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		work++
		if c.mass == 0 || c.node == i || reach > 0 && c.gap(p) > reach {
			continue // empty, p itself, or wholly out of reach
		}
		d, l := p.near(c.centre)
		leaf := c.kids == [4]int{}
		// Within a reach, a cell is one body only when it is wholly in reach: one that straddles
		// the reach is opened, so its nodes in reach push and the rest do not.
		far := (c.hi.x-c.lo.x)/max(l, 1e-9) < theta && (reach == 0 || c.farthest(p) <= reach)
		if leaf || far {
			if reach > 0 && l > reach {
				continue // a leaf out of reach (a bucket's nodes, at their mean)
			}
			if l < 1e-9 {
				// Two nodes on one point: a fixed nudge by index, so they part the same way each run.
				d, l = vec{float64(i%7) - 3, float64(i%5) - 2}, 1
			}
			f = f.add(d.scale(k2 * float64(c.mass) / (l * l)))
			continue
		}
		for _, k := range c.kids {
			if k != 0 {
				stack = append(stack, k)
			}
		}
	}
	return f, work
}
