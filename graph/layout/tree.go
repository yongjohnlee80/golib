package layout

import (
	"context"
	"fmt"
	"math"
)

// TreeInput is a tree to lay out: each node's size and its parent.
type TreeInput struct {
	Nodes []Size
	// Parent is each node's parent, an index into Nodes; -1 for the root. A tree with nodes has one
	// root, and every node reaches it.
	Parent []int
	// Dir is the way the tree grows: TB puts children below their parent, LR to its right.
	Dir Dir
	// BothWays splits the root's children into two sides of near-equal breadth, in their order:
	// the first grows in Dir, the second the opposite way, as a mind map grows from its centre.
	BothWays bool
	// NodeSep is the gap between neighbouring subtrees, LevelSep between a parent and its
	// children. Zero means 20 and 40.
	NodeSep, LevelSep float32
}

// TreeResult is where a tree's nodes go. Coordinates start at (0, 0).
type TreeResult struct {
	Nodes []Point // each node's centre
	// Edges is each node's line from its parent's border to its own, indexed by the node; nil for
	// the root.
	Edges [][]Point
	Size  Size
}

// treeBox is a node in the layout's own terms: depth runs the way the tree grows, breadth across.
type treeBox struct {
	depth, breadth float32 // the node's extent along each
	span           float32 // its subtree's breadth
	at             float32 // its centre's depth, from the root's centre, signed by its side
	across         float32 // its centre's breadth, from its subtree's start
	kids           []int
}

// Tree lays in out as a tidy tree: each subtree in a band of its own breadth, a parent centred
// over its children, each child one LevelSep past its parent's far side. It refuses with
// ErrTooLarge before allocating past lim (MaxNodes, MaxRanks as the deepest level, MaxWork), and
// checks ctx every 1024 steps of work.
func Tree(ctx context.Context, in TreeInput, lim Limits) (TreeResult, error) {
	lim = lim.withDefaults()
	root, err := treeValidate(in, lim)
	if err != nil {
		return TreeResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return TreeResult{}, err
	}
	n := len(in.Nodes)
	if n == 0 {
		return TreeResult{}, nil
	}
	nodeSep, levelSep := in.NodeSep, in.LevelSep
	if nodeSep == 0 {
		nodeSep = 20
	}
	if levelSep == 0 {
		levelSep = 40
	}
	m := &meter{ctx: ctx, max: lim.MaxWork}
	vertical := in.Dir == TB || in.Dir == BT
	boxes := make([]treeBox, n)
	for i, s := range in.Nodes {
		boxes[i].depth, boxes[i].breadth = s.W, s.H
		if vertical {
			boxes[i].depth, boxes[i].breadth = s.H, s.W
		}
	}
	for i, p := range in.Parent {
		if p >= 0 {
			boxes[p].kids = append(boxes[p].kids, i)
		}
	}
	// order: every node after its parent, so a pass backwards sees children before parents
	order := make([]int, 0, n)
	order = append(order, root)
	for i := 0; i < len(order); i++ {
		if err := m.add(1); err != nil {
			return TreeResult{}, err
		}
		order = append(order, boxes[order[i]].kids...)
	}
	// spans, children first
	for i := n - 1; i >= 0; i-- {
		b := &boxes[order[i]]
		var kids float32
		for j, k := range b.kids {
			if j > 0 {
				kids += nodeSep
			}
			kids += boxes[k].span
		}
		b.span = max(b.breadth, kids)
	}

	// the root's children, by side: one side, or two of near-equal breadth in their order
	sides := [][]int{boxes[root].kids}
	if in.BothWays && len(boxes[root].kids) > 1 {
		sides = treeSplit(boxes, boxes[root].kids, nodeSep)
	}
	sign := []float32{1, -1}
	center := make([]float32, n) // each node's centre's breadth, absolute in its side's band
	var minB, maxB float32
	for si, kids := range sides {
		var band float32
		for j, k := range kids {
			if j > 0 {
				band += nodeSep
			}
			band += boxes[k].span
		}
		// the side's band is centred on the root's centre (breadth 0)
		start := -band / 2
		for _, k := range kids {
			if err := treePlace(boxes, center, k, start, sign[si]*(boxes[root].depth/2+levelSep+boxes[k].depth/2), sign[si], nodeSep, levelSep, m); err != nil {
				return TreeResult{}, err
			}
			start += boxes[k].span + nodeSep
		}
		minB, maxB = min(minB, -band/2), max(maxB, band/2)
	}
	minB, maxB = min(minB, -boxes[root].breadth/2), max(maxB, boxes[root].breadth/2)

	// the extent along depth, then everything moved to start at (0, 0)
	var minD, maxD float32
	for i := range boxes {
		minD, maxD = min(minD, boxes[i].at-boxes[i].depth/2), max(maxD, boxes[i].at+boxes[i].depth/2)
	}
	res := TreeResult{Nodes: make([]Point, n), Edges: make([][]Point, n)}
	point := func(d, b float32) Point {
		d -= minD
		b -= minB
		switch in.Dir {
		case BT:
			return Point{X: b, Y: maxD - minD - d}
		case LR:
			return Point{X: d, Y: b}
		case RL:
			return Point{X: maxD - minD - d, Y: b}
		}
		return Point{X: b, Y: d}
	}
	for i := range boxes {
		res.Nodes[i] = point(boxes[i].at, center[i])
	}
	for i, p := range in.Parent {
		if p < 0 {
			continue
		}
		// from the parent's side facing the child to the child's side facing the parent
		s := float32(1)
		if boxes[i].at < boxes[p].at {
			s = -1
		}
		res.Edges[i] = []Point{
			point(boxes[p].at+s*boxes[p].depth/2, center[p]),
			point(boxes[i].at-s*boxes[i].depth/2, center[i]),
		}
	}
	res.Size = Size{W: maxB - minB, H: maxD - minD}
	if !vertical {
		res.Size = Size{W: maxD - minD, H: maxB - minB}
	}
	return res, nil
}

// treePlace puts node i at depth at, its subtree's band starting at breadth start, and its
// children one level further the way sign says.
func treePlace(boxes []treeBox, center []float32, i int, start, at, sign, nodeSep, levelSep float32, m *meter) error {
	if err := m.add(1); err != nil {
		return err
	}
	b := &boxes[i]
	b.at = at
	var kids float32
	for j, k := range b.kids {
		if j > 0 {
			kids += nodeSep
		}
		kids += boxes[k].span
	}
	// the children's band, centred in this subtree's band; this node centred over it
	ks := start + (b.span-kids)/2
	center[i] = start + b.span/2
	for _, k := range b.kids {
		if err := treePlace(boxes, center, k, ks, at+sign*(b.depth/2+levelSep+boxes[k].depth/2), sign, nodeSep, levelSep, m); err != nil {
			return err
		}
		ks += boxes[k].span + nodeSep
	}
	return nil
}

// treeSplit divides kids, in their order, at the point where the two sides' breadths differ least.
func treeSplit(boxes []treeBox, kids []int, nodeSep float32) [][]int {
	var total float32
	for _, k := range kids {
		total += boxes[k].span + nodeSep
	}
	best, bestDiff := 1, float32(math.Inf(1))
	var prefix float32
	for i := 1; i < len(kids); i++ {
		prefix += boxes[kids[i-1]].span + nodeSep
		if d := float32(math.Abs(float64(total - 2*prefix))); d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return [][]int{kids[:best], kids[best:]}
}

// treeValidate checks in against lim and is its root: one root, parents in range, no cycle, and no
// level deeper than lim.MaxRanks.
func treeValidate(in TreeInput, lim Limits) (int, error) {
	n := len(in.Nodes)
	if n > lim.MaxNodes {
		return 0, fmt.Errorf("%w: %d nodes, more than %d", ErrTooLarge, n, lim.MaxNodes)
	}
	if len(in.Parent) != n {
		return 0, fmt.Errorf("%w: %d parents for %d nodes", ErrInvalid, len(in.Parent), n)
	}
	bad := func(f float32) bool { return f < 0 || math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) }
	if bad(in.NodeSep) || bad(in.LevelSep) {
		return 0, fmt.Errorf("%w: a negative separation", ErrInvalid)
	}
	root := -1
	for i, s := range in.Nodes {
		if bad(s.W) || bad(s.H) {
			return 0, fmt.Errorf("%w: node %d has size %vx%v", ErrInvalid, i, s.W, s.H)
		}
		switch p := in.Parent[i]; {
		case p == -1:
			if root >= 0 {
				return 0, fmt.Errorf("%w: nodes %d and %d are both roots", ErrInvalid, root, i)
			}
			root = i
		case p < 0 || p >= n || p == i:
			return 0, fmt.Errorf("%w: node %d's parent is %d of %d nodes", ErrInvalid, i, p, n)
		}
	}
	if n > 0 && root < 0 {
		return 0, fmt.Errorf("%w: no root", ErrInvalid)
	}
	// every node reaches the root within MaxRanks levels; depths are memoised, so this is linear
	depth := make([]int, n)
	for i := range depth {
		depth[i] = -1
	}
	if root >= 0 {
		depth[root] = 0
	}
	var walk []int
	for i := range n {
		walk = walk[:0]
		v := i
		for depth[v] < 0 {
			walk = append(walk, v)
			if len(walk) > n {
				return 0, fmt.Errorf("%w: node %d is in a cycle", ErrInvalid, i)
			}
			v = in.Parent[v]
		}
		for j := len(walk) - 1; j >= 0; j-- {
			d := depth[v] + 1
			if d > lim.MaxRanks {
				return 0, fmt.Errorf("%w: a level %d deep, more than %d", ErrTooLarge, d, lim.MaxRanks)
			}
			depth[walk[j]] = d
			v = walk[j]
		}
	}
	return root, nil
}
