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
	// Spacing is the length an edge settles near; 0 means 30. Theta and Spacing must be finite.
	Spacing float32
}

// ForceResult is each node's centre and the extent of all of them. Coordinates start at (0, 0).
type ForceResult struct {
	Nodes []Point
	Size  Size
}

// Force places in by a seeded force simulation: every edge pulls its ends together, every pair of
// nodes pushes apart (by a Barnes–Hut quadtree, O(n log n) a step), a weak gravity keeps
// unconnected parts near the centre, and the most a node may move shrinks step by step. It refuses
// an edge to a node that does not exist, or a Theta or Spacing that is NaN or infinite, with
// ErrInvalid; with ErrTooLarge more nodes or edges than lim allows (MaxNodes, MaxEdges), more work
// than lim.MaxWork, or a layout float32 cannot hold (a Spacing near its largest). It charges its
// work, and checks ctx, after each node's repulsion and after each step's edges: building a
// step's quadtree is not charged, so one charge may follow more than 1024 tree operations.
func Force(ctx context.Context, in ForceInput, lim Limits) (ForceResult, error) {
	lim = lim.withDefaults()
	if err := forceValidate(in, lim); err != nil {
		return ForceResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ForceResult{}, err
	}
	n := in.Nodes
	if n == 0 {
		return ForceResult{}, nil
	}
	steps, theta, spacing := in.Steps, float64(in.Theta), float64(in.Spacing)
	if steps <= 0 {
		steps = 300
	}
	if theta <= 0 {
		theta = 0.9
	}
	if spacing <= 0 {
		spacing = 30
	}

	// Edges in a canonical order, self-loops and duplicates dropped: their order in the input does
	// not change the sums, so it does not change the layout.
	edges := make([][2]int, 0, len(in.Edges))
	for _, e := range in.Edges {
		a, b := min(e.From, e.To), max(e.From, e.To)
		if a != b {
			edges = append(edges, [2]int{a, b})
		}
	}
	slices.SortFunc(edges, func(x, y [2]int) int { return cmp.Or(cmp.Compare(x[0], y[0]), cmp.Compare(x[1], y[1])) })
	edges = slices.Compact(edges)

	m := &meter{ctx: ctx, max: lim.MaxWork}
	rng := rand.New(rand.NewPCG(uint64(in.Seed), 0x9e3779b97f4a7c15))
	radius := spacing * math.Sqrt(float64(n))
	pos := make([]vec, n)
	for i := range pos {
		// Uniform in a disc as wide as the graph will roughly settle to.
		r, a := radius*math.Sqrt(rng.Float64()), 2*math.Pi*rng.Float64()
		pos[i] = vec{r * math.Cos(a), r * math.Sin(a)}
	}

	k2 := spacing * spacing
	gravity := 0.02
	disp := make([]vec, n)
	var tree quadTree
	for step := range steps {
		// The most a node moves this step: a tenth of the starting radius, cooling to nothing.
		heat := radius * 0.1 * (1 - float64(step)/float64(steps))
		clear(disp)
		tree.build(pos)
		for i, p := range pos {
			f, work := tree.repulse(i, p, theta, k2)
			if err := m.add(work); err != nil {
				return ForceResult{}, err
			}
			disp[i] = disp[i].add(f).add(p.scale(-gravity))
		}
		for _, e := range edges {
			d := pos[e[1]].sub(pos[e[0]])
			l := d.len()
			if l == 0 {
				continue
			}
			pull := d.scale(l / spacing) // |F| = d²/k, along d
			disp[e[0]] = disp[e[0]].add(pull)
			disp[e[1]] = disp[e[1]].sub(pull)
		}
		if err := m.add(len(edges)); err != nil {
			return ForceResult{}, err
		}
		for i, d := range disp {
			if l := d.len(); l > heat && l > 0 {
				d = d.scale(heat / l)
			}
			pos[i] = pos[i].add(d)
		}
	}

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range pos {
		minX, minY = min(minX, p.x), min(minY, p.y)
		maxX, maxY = max(maxX, p.x), max(maxY, p.y)
	}
	out := ForceResult{Nodes: make([]Point, n), Size: Size{W: float32(maxX - minX), H: float32(maxY - minY)}}
	if !finite32(out.Size.W) || !finite32(out.Size.H) {
		return ForceResult{}, fmt.Errorf("%w: a layout %g by %g, past float32", ErrTooLarge, maxX-minX, maxY-minY)
	}
	for i, p := range pos {
		out.Nodes[i] = Point{X: float32(p.x - minX), Y: float32(p.y - minY)}
	}
	return out, nil
}

// finite32 reports whether v is a number, and not an infinity.
func finite32(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

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
	case !finite32(in.Theta) || !finite32(in.Spacing):
		return fmt.Errorf("%w: theta %v, spacing %v", ErrInvalid, in.Theta, in.Spacing)
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
func (t *quadTree) repulse(i int, p vec, theta, k2 float64) (f vec, work int) {
	stack := append(t.stack[:0], 0)
	defer func() { t.stack = stack }()
	for len(stack) > 0 {
		c := t.cells[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		work++
		if c.mass == 0 || c.node == i {
			continue
		}
		d, l := p.near(c.centre)
		leaf := c.kids == [4]int{}
		if leaf || (c.hi.x-c.lo.x)/max(l, 1e-9) < theta {
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
