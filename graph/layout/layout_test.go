package layout_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/graph/layout"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const eps = 0.01

func lay(t *testing.T, in layout.Input) layout.Result {
	t.Helper()
	res, err := layout.Layered(context.Background(), in, layout.Limits{})
	if err != nil {
		t.Fatalf("Layered: %v", err)
	}
	return res
}

func rectOf(in layout.Input, res layout.Result, i int) (x0, y0, x1, y1 float64) {
	c, s := res.Nodes[i], in.Nodes[i]
	return float64(c.X - s.W/2), float64(c.Y - s.H/2), float64(c.X + s.W/2), float64(c.Y + s.H/2)
}

func onBorder(in layout.Input, res layout.Result, i int, p layout.Point) bool {
	x0, y0, x1, y1 := rectOf(in, res, i)
	x, y := float64(p.X), float64(p.Y)
	inside := x >= x0-eps && x <= x1+eps && y >= y0-eps && y <= y1+eps
	edge := math.Abs(x-x0) < eps || math.Abs(x-x1) < eps || math.Abs(y-y0) < eps || math.Abs(y-y1) < eps
	return inside && edge
}

// checkProperties holds for every drawing: nodes never overlap, every edge leaves its From node's
// border and reaches its To node's, every point is inside the drawing, and a group's box holds its
// members and no other node's centre.
func checkProperties(t *testing.T, name string, in layout.Input, res layout.Result) {
	t.Helper()
	for i := range in.Nodes {
		for j := i + 1; j < len(in.Nodes); j++ {
			ax0, ay0, ax1, ay1 := rectOf(in, res, i)
			bx0, by0, bx1, by1 := rectOf(in, res, j)
			if ax0 < bx1-eps && bx0 < ax1-eps && ay0 < by1-eps && by0 < ay1-eps {
				t.Errorf("%s: nodes %d and %d overlap", name, i, j)
			}
		}
	}
	for k, e := range in.Edges {
		line := res.Edges[k]
		if len(line) < 2 {
			t.Errorf("%s: edge %d has %d points", name, k, len(line))
			continue
		}
		if e.From == e.To {
			continue
		}
		if !onBorder(in, res, e.From, line[0]) {
			t.Errorf("%s: edge %d starts at %v, not on node %d's border", name, k, line[0], e.From)
		}
		if !onBorder(in, res, e.To, line[len(line)-1]) {
			t.Errorf("%s: edge %d ends at %v, not on node %d's border", name, k, line[len(line)-1], e.To)
		}
		for _, p := range line {
			if p.X < -eps || p.Y < -eps || p.X > res.Size.W+eps || p.Y > res.Size.H+eps {
				t.Errorf("%s: edge %d point %v outside %v", name, k, p, res.Size)
			}
		}
	}
	for gi, grp := range in.Groups {
		r := res.Groups[gi]
		member := map[int]bool{}
		for _, v := range grp {
			member[v] = true
			x0, y0, x1, y1 := rectOf(in, res, v)
			if x0 < float64(r.X)-eps || y0 < float64(r.Y)-eps || x1 > float64(r.X+r.W)+eps || y1 > float64(r.Y+r.H)+eps {
				t.Errorf("%s: group %d's box %v does not hold node %d", name, gi, r, v)
			}
		}
		for v := range in.Nodes {
			c := res.Nodes[v]
			if !member[v] && c.X > r.X && c.X < r.X+r.W && c.Y > r.Y && c.Y < r.Y+r.H {
				t.Errorf("%s: group %d's box %v holds non-member %d at %v", name, gi, r, v, c)
			}
		}
	}
}

func boxes(n int, w, h float32) []layout.Size {
	s := make([]layout.Size, n)
	for i := range s {
		s[i] = layout.Size{W: w + float32(i%3)*20, H: h}
	}
	return s
}

// lcg is a small deterministic generator, so the random graphs are the same on every run.
type lcg uint64

func (r *lcg) next(n int) int {
	*r = *r*6364136223846793005 + 1442695040888963407
	return int(uint64(*r)>>33) % n
}

func randomDAG(seed uint64, n, m int) layout.Input {
	r := lcg(seed)
	in := layout.Input{Nodes: boxes(n, 60, 30)}
	for range m {
		a, b := r.next(n), r.next(n)
		if a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		in.Edges = append(in.Edges, layout.Edge{From: a, To: b})
	}
	return in
}

func TestProperties(t *testing.T) {
	cases := map[string]layout.Input{
		"chain": {Nodes: boxes(4, 60, 30), Edges: []layout.Edge{{From: 0, To: 1}, {From: 1, To: 2}, {From: 2, To: 3}}},
		"diamond": {Nodes: boxes(4, 60, 30), Edges: []layout.Edge{
			{From: 0, To: 1}, {From: 0, To: 2}, {From: 1, To: 3}, {From: 2, To: 3}}},
		"long edge": {Nodes: boxes(4, 60, 30), Edges: []layout.Edge{
			{From: 0, To: 1}, {From: 1, To: 2}, {From: 2, To: 3}, {From: 0, To: 3}}},
		"cycle":   {Nodes: boxes(3, 60, 30), Edges: []layout.Edge{{From: 0, To: 1}, {From: 1, To: 2}, {From: 2, To: 0}}},
		"min len": {Nodes: boxes(2, 60, 30), Edges: []layout.Edge{{From: 0, To: 1, MinLen: 3}}},
		"labels": {Nodes: boxes(3, 60, 30), Edges: []layout.Edge{
			{From: 0, To: 1, Label: layout.Size{W: 40, H: 14}}, {From: 0, To: 2}, {From: 1, To: 2, Label: layout.Size{W: 70, H: 14}}}},
		"self loop": {Nodes: boxes(2, 60, 30), Edges: []layout.Edge{{From: 0, To: 0, Label: layout.Size{W: 20, H: 10}}, {From: 0, To: 1}}},
		"groups": {Nodes: boxes(6, 60, 30),
			Edges:  []layout.Edge{{From: 0, To: 1}, {From: 0, To: 2}, {From: 1, To: 3}, {From: 2, To: 4}, {From: 3, To: 5}, {From: 4, To: 5}},
			Groups: [][]int{{1, 3}, {2, 4}}},
		"nested groups": {Nodes: boxes(5, 60, 30),
			Edges:  []layout.Edge{{From: 0, To: 1}, {From: 1, To: 2}, {From: 0, To: 3}, {From: 3, To: 4}},
			Groups: [][]int{{1, 2, 3}, {2}}},
		"disconnected": {Nodes: boxes(5, 60, 30), Edges: []layout.Edge{{From: 0, To: 1}, {From: 3, To: 4}}},
	}
	for seed := range uint64(6) {
		cases[fmt.Sprintf("random %d", seed)] = randomDAG(seed+1, 14, 24)
	}
	for _, dir := range []layout.Dir{layout.TB, layout.BT, layout.LR, layout.RL} {
		for name, in := range cases {
			in.Dir = dir
			checkProperties(t, fmt.Sprintf("%s/%d", name, dir), in, lay(t, in))
		}
	}
}

// Ranks run one way: in a DAG every edge points down a top-to-bottom drawing and right in a
// left-to-right one.
func TestRanksFollowTheDirection(t *testing.T) {
	in := randomDAG(7, 12, 20)
	for _, dir := range []layout.Dir{layout.TB, layout.BT, layout.LR, layout.RL} {
		in.Dir = dir
		res := lay(t, in)
		for k, e := range in.Edges {
			a, b := res.Nodes[e.From], res.Nodes[e.To]
			ok := map[layout.Dir]bool{layout.TB: b.Y > a.Y, layout.BT: b.Y < a.Y, layout.LR: b.X > a.X, layout.RL: b.X < a.X}[dir]
			if !ok {
				t.Errorf("dir %d: edge %d from %v to %v goes against the ranks", dir, k, a, b)
			}
		}
	}
}

// geomCrossings counts the crossings of a drawing's edges, segment by segment.
func geomCrossings(res layout.Result) int {
	type seg struct{ a, b layout.Point }
	var segs [][]seg
	for _, line := range res.Edges {
		var s []seg
		for k := 1; k < len(line); k++ {
			s = append(s, seg{line[k-1], line[k]})
		}
		segs = append(segs, s)
	}
	cross := func(p, q seg) bool {
		d := func(a, b, c layout.Point) float64 {
			return float64((b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X))
		}
		d1, d2 := d(p.a, p.b, q.a), d(p.a, p.b, q.b)
		d3, d4 := d(q.a, q.b, p.a), d(q.a, q.b, p.b)
		return ((d1 > eps && d2 < -eps) || (d1 < -eps && d2 > eps)) && ((d3 > eps && d4 < -eps) || (d3 < -eps && d4 > eps))
	}
	n := 0
	for i := range segs {
		for j := i + 1; j < len(segs); j++ {
			for _, p := range segs[i] {
				for _, q := range segs[j] {
					if cross(p, q) {
						n++
					}
				}
			}
		}
	}
	return n
}

func TestCrossingsReduced(t *testing.T) {
	cases := []struct {
		name string
		in   layout.Input
		max  int
	}{
		// Given in an order that crosses; untangled by the sweeps.
		{"swapped pairs", layout.Input{Nodes: boxes(4, 40, 20), Edges: []layout.Edge{{From: 0, To: 3}, {From: 1, To: 2}}}, 0},
		{"tree", layout.Input{Nodes: boxes(7, 40, 20), Edges: []layout.Edge{
			{From: 0, To: 2}, {From: 1, To: 2}, {From: 0, To: 5}, {From: 1, To: 4}, {From: 2, To: 6}, {From: 5, To: 3}}}, 0},
		{"ladder", layout.Input{Nodes: boxes(6, 40, 20), Edges: []layout.Edge{
			{From: 0, To: 5}, {From: 1, To: 4}, {From: 2, To: 3}, {From: 0, To: 1}, {From: 1, To: 2}}}, 1},
		// K(2,3) on two ranks: C(2,2)·C(3,2) = 3 crossings whatever the order.
		{"k23", layout.Input{Nodes: boxes(5, 40, 20), Edges: []layout.Edge{
			{From: 0, To: 2}, {From: 0, To: 3}, {From: 0, To: 4}, {From: 1, To: 2}, {From: 1, To: 3}, {From: 1, To: 4}}}, 3},
	}
	for _, c := range cases {
		if got := geomCrossings(lay(t, c.in)); got > c.max {
			t.Errorf("%s: %d crossings, want at most %d", c.name, got, c.max)
		}
	}
}

func TestDeterministic(t *testing.T) {
	in := randomDAG(11, 30, 60)
	in.Groups = [][]int{{3, 4, 5}, {10, 11}}
	a, b := lay(t, in), lay(t, in)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two layouts of one input differ")
	}
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name+".json")
	if *update {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if string(want) != string(got)+"\n" {
		t.Errorf("%s differs from its golden:\n%s", name, got)
	}
}

func TestDirectionGoldens(t *testing.T) {
	base := layout.Input{
		Nodes:  []layout.Size{{W: 80, H: 40}, {W: 120, H: 30}, {W: 60, H: 60}, {W: 90, H: 40}},
		Edges:  []layout.Edge{{From: 0, To: 1}, {From: 0, To: 2, Label: layout.Size{W: 30, H: 12}}, {From: 1, To: 3}, {From: 2, To: 3}, {From: 0, To: 3}},
		Groups: [][]int{{1, 3}},
	}
	for _, d := range []struct {
		name string
		dir  layout.Dir
	}{{"tb", layout.TB}, {"bt", layout.BT}, {"lr", layout.LR}, {"rl", layout.RL}} {
		in := base
		in.Dir = d.dir
		golden(t, "dir-"+d.name, lay(t, in))
	}
}

func TestLimits(t *testing.T) {
	ctx := context.Background()
	big := layout.Input{Nodes: boxes(1500, 10, 10)}
	if _, err := layout.Layered(ctx, big, layout.Limits{}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("1500 nodes: %v, want ErrTooLarge", err)
	}
	edges := layout.Input{Nodes: boxes(3, 10, 10), Edges: make([]layout.Edge, 2001)}
	for i := range edges.Edges {
		edges.Edges[i] = layout.Edge{From: 0, To: 1}
	}
	if _, err := layout.Layered(ctx, edges, layout.Limits{}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("2001 edges: %v, want ErrTooLarge", err)
	}
	tall := layout.Input{Nodes: boxes(2, 10, 10), Edges: []layout.Edge{{From: 0, To: 1, MinLen: 300}}}
	if _, err := layout.Layered(ctx, tall, layout.Limits{}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("an edge over 300 ranks: %v, want ErrTooLarge", err)
	}
	long := layout.Input{Nodes: boxes(2, 10, 10), Edges: []layout.Edge{{From: 0, To: 1, MinLen: 50}}}
	if _, err := layout.Layered(ctx, long, layout.Limits{MaxDummies: 10}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("49 dummies over a limit of 10: %v, want ErrTooLarge", err)
	}
	busy := randomDAG(3, 200, 600)
	if _, err := layout.Layered(ctx, busy, layout.Limits{MaxWork: 5000}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("work over its budget: %v, want ErrTooLarge", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := layout.Layered(cancelled, busy, layout.Limits{}); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled ctx: %v, want context.Canceled", err)
	}
}

func TestInvalid(t *testing.T) {
	for name, in := range map[string]layout.Input{
		"edge to nowhere":    {Nodes: boxes(2, 10, 10), Edges: []layout.Edge{{From: 0, To: 5}}},
		"negative size":      {Nodes: []layout.Size{{W: -1, H: 10}}},
		"negative label":     {Nodes: boxes(2, 10, 10), Edges: []layout.Edge{{From: 0, To: 1, Label: layout.Size{W: -3}}}},
		"group to nowhere":   {Nodes: boxes(2, 10, 10), Groups: [][]int{{0, 9}}},
		"negative sep":       {Nodes: boxes(2, 10, 10), NodeSep: -5},
		"overlapping groups": {Nodes: boxes(4, 10, 10), Groups: [][]int{{0, 1, 2}, {2, 3}}},
	} {
		if _, err := layout.Layered(context.Background(), in, layout.Limits{}); !errors.Is(err, layout.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}

func TestEmptyAndLabels(t *testing.T) {
	if res := lay(t, layout.Input{}); res.Size != (layout.Size{}) || len(res.Nodes) != 0 {
		t.Errorf("empty input: %+v", res)
	}
	in := layout.Input{Nodes: boxes(2, 60, 30), Edges: []layout.Edge{{From: 0, To: 1, Label: layout.Size{W: 50, H: 16}}, {From: 0, To: 1}}}
	res := lay(t, in)
	l := res.Labels[0]
	for i := range in.Nodes {
		x0, y0, x1, y1 := rectOf(in, res, i)
		if float64(l.X) > x0 && float64(l.X) < x1 && float64(l.Y) > y0 && float64(l.Y) < y1 {
			t.Errorf("the label at %v sits on node %d", l, i)
		}
	}
	if res.Labels[1] != (layout.Point{}) {
		t.Errorf("an edge without a label has a label point %v", res.Labels[1])
	}
}

// Groups' borders are vertices the layout adds, so they count against MaxDummies before any is
// made; group count and memberships are refused before anything is built; and the group passes
// spend MaxWork.
func TestGroupWorkIsBounded(t *testing.T) {
	two := layout.Input{Nodes: []layout.Size{{W: 10, H: 10}, {W: 10, H: 10}}, Edges: []layout.Edge{{From: 0, To: 1, MinLen: 1}}}
	nested := two
	for range 100 {
		nested.Groups = append(nested.Groups, []int{0, 1})
	}
	if _, err := layout.Layered(context.Background(), nested, layout.Limits{MaxDummies: 1}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("100 nested groups' borders under MaxDummies 1: %v, want layout.ErrTooLarge", err)
	}
	if _, err := layout.Layered(context.Background(), nested, layout.Limits{MaxGroups: 99}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("100 groups over MaxGroups 99: %v, want layout.ErrTooLarge", err)
	}
	if _, err := layout.Layered(context.Background(), nested, layout.Limits{MaxMemberships: 199}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("200 memberships over 199: %v, want layout.ErrTooLarge", err)
	}
	if _, err := layout.Layered(context.Background(), nested, layout.Limits{MaxWork: 300}); !errors.Is(err, layout.ErrTooLarge) {
		t.Errorf("group passes under MaxWork 300: %v, want layout.ErrTooLarge", err)
	}
	if _, err := layout.Layered(context.Background(), nested, layout.Limits{}); err != nil {
		t.Errorf("100 nested groups within the defaults: %v", err)
	}
}
