package layout

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// Size is a width and a height.
type Size struct{ W, H float32 }

// Point is a position.
type Point struct{ X, Y float32 }

// Rect is a box: its top-left corner and its size.
type Rect struct{ X, Y, W, H float32 }

// Dir is the direction ranks run in.
type Dir uint8

const (
	TB Dir = iota // top to bottom
	BT            // bottom to top
	LR            // left to right
	RL            // right to left
)

// Edge joins node From to node To, both indexes into Input.Nodes.
type Edge struct {
	From, To int
	// MinLen is the fewest ranks the edge spans; below 1 counts as 1. Longer arrows in a
	// flowchart ask for more.
	MinLen int
	// Label is the size of the edge's label; zero for none. A labelled edge's label gets a
	// place of its own on the edge's middle rank.
	Label Size
}

// Input is a graph to lay out.
type Input struct {
	Nodes []Size
	Edges []Edge
	Dir   Dir
	// Groups are sets of node indexes drawn in one box each, such as a flowchart's subgraphs.
	// Two groups are either disjoint or one holds the other.
	Groups [][]int
	// NodeSep is the gap between neighbours on a rank, RankSep between ranks, and GroupPad the
	// margin a group's box keeps around its members. Zero means 50, 50 and 16.
	NodeSep, RankSep, GroupPad float32
}

// Result is where everything goes. Coordinates start at (0, 0).
type Result struct {
	Nodes  []Point   // each node's centre
	Edges  [][]Point // each edge's polyline, from its From node's border to its To node's
	Labels []Point   // each edge's label centre; zero for an edge without a label
	Groups []Rect
	Size   Size
}

// Limits bound the work a layout may do. A zero field takes its default.
type Limits struct {
	MaxNodes       int // default 1000
	MaxEdges       int // default 2000
	MaxRanks       int // default 200
	MaxGroups      int // default 200
	MaxMemberships int // nodes summed over groups; default 20000
	// MaxDummies bounds the vertices the layout adds: the dummies long edges are split by (the
	// sum over edges of the ranks each one crosses) and each group's two borders on every rank it
	// spans. It is checked after ranking, before any is made.
	MaxDummies int // default 20000
	// MaxWork bounds the steps the ranking, ordering and placing passes take, counted.
	MaxWork int // default 50000000
}

// DefaultLimits are the limits a zero Limits stands for.
var DefaultLimits = Limits{MaxNodes: 1000, MaxEdges: 2000, MaxGroups: 200, MaxMemberships: 20000, MaxRanks: 200, MaxDummies: 20000, MaxWork: 50_000_000}

var (
	// ErrTooLarge is a graph past one of its Limits.
	ErrTooLarge = errors.New("layout: over a limit")
	// ErrInvalid is an Input that does not describe a graph: an edge to a node that does not
	// exist, a negative size, or groups that overlap without one holding the other.
	ErrInvalid = errors.New("layout: invalid input")
)

func (l Limits) withDefaults() Limits {
	d := DefaultLimits
	if l.MaxNodes > 0 {
		d.MaxNodes = l.MaxNodes
	}
	if l.MaxEdges > 0 {
		d.MaxEdges = l.MaxEdges
	}
	if l.MaxRanks > 0 {
		d.MaxRanks = l.MaxRanks
	}
	if l.MaxDummies > 0 {
		d.MaxDummies = l.MaxDummies
	}
	if l.MaxGroups > 0 {
		d.MaxGroups = l.MaxGroups
	}
	if l.MaxMemberships > 0 {
		d.MaxMemberships = l.MaxMemberships
	}
	if l.MaxWork > 0 {
		d.MaxWork = l.MaxWork
	}
	return d
}

// meter counts work against a budget and looks at ctx every 1024 steps.
type meter struct {
	ctx   context.Context
	n     int
	max   int
	check int
}

func (m *meter) add(k int) error {
	m.n += k
	if m.n > m.max {
		return fmt.Errorf("%w: more than %d steps of work", ErrTooLarge, m.max)
	}
	if m.n >= m.check {
		m.check = m.n + 1024
		return m.ctx.Err()
	}
	return nil
}

// Layered lays in out, or refuses it with ErrTooLarge before allocating past lim. ctx is
// checked between passes and every 1024 steps of work inside one.
func Layered(ctx context.Context, in Input, lim Limits) (Result, error) {
	lim = lim.withDefaults()
	if err := validate(in, lim); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	m := &meter{ctx: ctx, max: lim.MaxWork}
	g, err := build(in, m)
	if err != nil {
		return Result{}, err
	}
	if err := g.rank(m); err != nil {
		return Result{}, err
	}
	if g.ranks > lim.MaxRanks {
		return Result{}, fmt.Errorf("%w: %d ranks, more than %d", ErrTooLarge, g.ranks, lim.MaxRanks)
	}
	if d := g.dummiesNeeded() + g.bordersNeeded(); d > lim.MaxDummies {
		return Result{}, fmt.Errorf("%w: %d dummy and border vertices, more than %d", ErrTooLarge, d, lim.MaxDummies)
	}
	g.split()
	if err := g.borders(m); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := g.order(m); err != nil {
		return Result{}, err
	}
	if err := g.position(m); err != nil {
		return Result{}, err
	}
	return g.result(), nil
}

func validate(in Input, lim Limits) error {
	if len(in.Nodes) > lim.MaxNodes {
		return fmt.Errorf("%w: %d nodes, more than %d", ErrTooLarge, len(in.Nodes), lim.MaxNodes)
	}
	if len(in.Edges) > lim.MaxEdges {
		return fmt.Errorf("%w: %d edges, more than %d", ErrTooLarge, len(in.Edges), lim.MaxEdges)
	}
	if len(in.Groups) > lim.MaxGroups {
		return fmt.Errorf("%w: %d groups, more than %d", ErrTooLarge, len(in.Groups), lim.MaxGroups)
	}
	members := 0
	for _, grp := range in.Groups {
		members += len(grp)
	}
	if members > lim.MaxMemberships {
		return fmt.Errorf("%w: %d group memberships, more than %d", ErrTooLarge, members, lim.MaxMemberships)
	}
	bad := func(f float32) bool { return f < 0 || math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) }
	for i, n := range in.Nodes {
		if bad(n.W) || bad(n.H) {
			return fmt.Errorf("%w: node %d has size %vx%v", ErrInvalid, i, n.W, n.H)
		}
	}
	for i, e := range in.Edges {
		if e.From < 0 || e.From >= len(in.Nodes) || e.To < 0 || e.To >= len(in.Nodes) {
			return fmt.Errorf("%w: edge %d joins %d and %d of %d nodes", ErrInvalid, i, e.From, e.To, len(in.Nodes))
		}
		if bad(e.Label.W) || bad(e.Label.H) {
			return fmt.Errorf("%w: edge %d's label has size %vx%v", ErrInvalid, i, e.Label.W, e.Label.H)
		}
	}
	for gi, grp := range in.Groups {
		for _, v := range grp {
			if v < 0 || v >= len(in.Nodes) {
				return fmt.Errorf("%w: group %d holds node %d of %d", ErrInvalid, gi, v, len(in.Nodes))
			}
		}
	}
	if bad(in.NodeSep) || bad(in.RankSep) || bad(in.GroupPad) {
		return fmt.Errorf("%w: a negative separation", ErrInvalid)
	}
	return nil
}
