package layout_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/graph/layout"
)

// treeFixture is a root with three children, the first and last with children of their own.
func treeFixture(dir layout.Dir, both bool) layout.TreeInput {
	return layout.TreeInput{
		Nodes:    []layout.Size{{80, 30}, {60, 20}, {60, 20}, {60, 20}, {40, 16}, {40, 16}, {40, 16}},
		Parent:   []int{-1, 0, 0, 0, 1, 1, 3},
		Dir:      dir,
		BothWays: both,
	}
}

func treeOverlap(a, b layout.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func treeBoxes(in layout.TreeInput, res layout.TreeResult) []layout.Rect {
	out := make([]layout.Rect, len(in.Nodes))
	for i, s := range in.Nodes {
		c := res.Nodes[i]
		out[i] = layout.Rect{X: c.X - s.W/2, Y: c.Y - s.H/2, W: s.W, H: s.H}
	}
	return out
}

// No two nodes overlap, every node is inside the size, and each edge runs from its parent to it, in
// every direction, one way or both.
func TestATreeHasNoOverlaps(t *testing.T) {
	for _, dir := range []layout.Dir{layout.TB, layout.BT, layout.LR, layout.RL} {
		for _, both := range []bool{false, true} {
			in := treeFixture(dir, both)
			res, err := layout.Tree(context.Background(), in, layout.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			boxes := treeBoxes(in, res)
			for i, a := range boxes {
				if a.X < -0.01 || a.Y < -0.01 || a.X+a.W > res.Size.W+0.01 || a.Y+a.H > res.Size.H+0.01 {
					t.Errorf("dir %v both %v: node %d at %+v outside %+v", dir, both, i, a, res.Size)
				}
				for j := i + 1; j < len(boxes); j++ {
					if treeOverlap(a, boxes[j]) {
						t.Errorf("dir %v both %v: nodes %d %+v and %d %+v overlap", dir, both, i, a, j, boxes[j])
					}
				}
			}
			if res.Edges[0] != nil {
				t.Errorf("the root has an edge")
			}
			for i := 1; i < len(in.Nodes); i++ {
				if len(res.Edges[i]) != 2 {
					t.Errorf("node %d's edge %v", i, res.Edges[i])
				}
			}
		}
	}
}

// One way, every child is past its parent the way the tree grows.
func TestATreeGrowsItsWay(t *testing.T) {
	in := treeFixture(layout.TB, false)
	res, err := layout.Tree(context.Background(), in, layout.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range in.Parent {
		if p >= 0 && res.Nodes[i].Y <= res.Nodes[p].Y {
			t.Errorf("node %d at %v is not below its parent at %v", i, res.Nodes[i], res.Nodes[p])
		}
	}
	// a parent is centred over its children
	if c := (res.Nodes[4].X + res.Nodes[5].X) / 2; treeAbs(res.Nodes[1].X-c) > 0.01 {
		t.Errorf("node 1 at x %v, its children's middle %v", res.Nodes[1].X, c)
	}
}

// Both ways, the root's children split in their order into two sides of near-equal breadth, one
// each side of the root.
func TestATreeGrowsBothWays(t *testing.T) {
	in := layout.TreeInput{
		Nodes:    []layout.Size{{80, 30}, {60, 20}, {60, 20}, {60, 20}, {60, 20}},
		Parent:   []int{-1, 0, 0, 0, 0},
		Dir:      layout.LR,
		BothWays: true,
	}
	res, err := layout.Tree(context.Background(), in, layout.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	root := res.Nodes[0].X
	var right, left []int
	for i := 1; i < 5; i++ {
		if res.Nodes[i].X > root {
			right = append(right, i)
		} else {
			left = append(left, i)
		}
	}
	if !reflect.DeepEqual(right, []int{1, 2}) || !reflect.DeepEqual(left, []int{3, 4}) {
		t.Errorf("sides: right %v, left %v; want 1 2 right, 3 4 left", right, left)
	}
	// the root is in the middle of the drawing's breadth
	if mid := res.Size.H / 2; treeAbs(res.Nodes[0].Y-mid) > 0.01 {
		t.Errorf("the root at y %v, the middle %v", res.Nodes[0].Y, mid)
	}
	// a left child's edge leaves the root's left side
	if e := res.Edges[3]; e[0].X >= root || e[1].X >= e[0].X {
		t.Errorf("a left child's edge %v", e)
	}
}

// The same input lays out the same way.
func TestATreeIsDeterministic(t *testing.T) {
	in := treeFixture(layout.LR, true)
	a, _ := layout.Tree(context.Background(), in, layout.Limits{})
	b, _ := layout.Tree(context.Background(), in, layout.Limits{})
	if !reflect.DeepEqual(a, b) {
		t.Error("two layouts of one tree differ")
	}
}

// What is not a tree is ErrInvalid; one past a limit ErrTooLarge; a cancelled ctx its error; no
// nodes, nothing.
func TestATreeRefuses(t *testing.T) {
	ctx := context.Background()
	for name, in := range map[string]layout.TreeInput{
		"two roots":    {Nodes: []layout.Size{{1, 1}, {1, 1}}, Parent: []int{-1, -1}},
		"no root":      {Nodes: []layout.Size{{1, 1}, {1, 1}}, Parent: []int{1, 0}},
		"a cycle":      {Nodes: []layout.Size{{1, 1}, {1, 1}, {1, 1}}, Parent: []int{-1, 2, 1}},
		"out of range": {Nodes: []layout.Size{{1, 1}}, Parent: []int{3}},
		"parents":      {Nodes: []layout.Size{{1, 1}}, Parent: nil},
		"a size":       {Nodes: []layout.Size{{-1, 1}}, Parent: []int{-1}},
	} {
		if _, err := layout.Tree(ctx, in, layout.Limits{}); !errors.Is(err, layout.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	chain := layout.TreeInput{Nodes: make([]layout.Size, 6), Parent: []int{-1, 0, 1, 2, 3, 4}}
	if _, err := layout.Tree(ctx, chain, layout.Limits{MaxRanks: 4}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("five levels deep with four allowed: %v", err)
	}
	if _, err := layout.Tree(ctx, chain, layout.Limits{MaxNodes: 5}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("six nodes with five allowed: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := layout.Tree(cancelled, treeFixture(layout.TB, false), layout.Limits{}); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled ctx: %v", err)
	}
	if res, err := layout.Tree(ctx, layout.TreeInput{}, layout.Limits{}); err != nil || len(res.Nodes) != 0 || res.Size != (layout.Size{}) {
		t.Errorf("no nodes: %+v %v", res, err)
	}
}

func treeAbs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
