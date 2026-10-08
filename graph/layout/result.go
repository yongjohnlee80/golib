package layout

import (
	"math"
	"slices"
)

// frame turns the top-to-bottom frame into the drawing's direction.
func (g *graph) frame(x, y float64) (float64, float64) {
	switch g.in.Dir {
	case BT:
		return x, -y
	case LR:
		return y, x
	case RL:
		return -y, x
	}
	return x, y
}

// box is a rect in the top-to-bottom frame, by centre and size, turned to the drawing's
// direction: corners map to corners, so its bounds do too.
func (g *graph) box(cx, cy, w, h float64) (x0, y0, x1, y1 float64) {
	ax, ay := g.frame(cx-w/2, cy-h/2)
	bx, by := g.frame(cx+w/2, cy+h/2)
	return min(ax, bx), min(ay, by), max(ax, bx), max(ay, by)
}

// clip is where the segment from v's centre toward (px, py) leaves v's box.
func (g *graph) clip(v int, px, py float64) (float64, float64) {
	c := g.v[v]
	dx, dy := px-c.x, py-c.y
	hw, hh := c.w/2, c.h/2
	if (dx == 0 && dy == 0) || (hw == 0 && hh == 0) {
		return c.x, c.y
	}
	t := math.Inf(1)
	if dx != 0 {
		t = hw / math.Abs(dx)
	}
	if dy != 0 {
		t = min(t, hh/math.Abs(dy))
	}
	t = min(t, 1)
	return c.x + dx*t, c.y + dy*t
}

func (g *graph) result() Result {
	n := len(g.in.Nodes)
	type pt struct{ x, y float64 }
	edges := make([][]pt, len(g.in.Edges))
	labels := make([]pt, len(g.in.Edges))
	hasLabel := make([]bool, len(g.in.Edges))
	for i, c := range g.chains {
		e := g.edges[i]
		var mid []pt
		for _, v := range c[1 : len(c)-1] {
			mid = append(mid, pt{g.v[v].x, g.v[v].y})
		}
		toward := func(k int) pt {
			if k >= 0 && k < len(mid) {
				return mid[k]
			}
			if k < 0 {
				return pt{g.v[e.from].x, g.v[e.from].y}
			}
			return pt{g.v[e.to].x, g.v[e.to].y}
		}
		p0 := toward(0)
		fx, fy := g.clip(e.from, p0.x, p0.y)
		p1 := toward(len(mid) - 1)
		if len(mid) == 0 {
			p1 = pt{g.v[e.from].x, g.v[e.from].y}
		}
		tx, ty := g.clip(e.to, p1.x, p1.y)
		line := append(append([]pt{{fx, fy}}, mid...), pt{tx, ty})
		if e.rev {
			slices.Reverse(line)
		}
		edges[e.orig] = line
		if lv := g.labelV[e.orig]; lv >= 0 {
			labels[e.orig], hasLabel[e.orig] = pt{g.v[lv].x, g.v[lv].y}, true
		}
	}
	for _, i := range g.loops {
		v := g.v[g.in.Edges[i].From]
		r := v.x + v.w/2
		reach := g.nodeSep / 2
		edges[i] = []pt{{r, v.y - v.h/4}, {r + reach, v.y - v.h/4}, {r + reach, v.y + v.h/4}, {r, v.y + v.h/4}}
		if l := g.in.Edges[i].Label; l.W > 0 || l.H > 0 {
			lw := float64(l.W)
			if g.in.sideways() {
				lw = float64(l.H)
			}
			labels[i], hasLabel[i] = pt{r + reach + lw/2 + 4, v.y}, true
		}
	}

	// Groups: across ranks, from the leftmost left border to the rightmost right one; down
	// them, the members' extent grown by GroupPad once for the group and once more for each group
	// nested in it, so nested boxes do not touch.
	type bounds struct {
		x0, y0, x1, y1 float64
		set            bool
	}
	gb := make([]bounds, g.nGroups)
	depthBelow := make([]int, g.nGroups)
	for _, v := range g.v {
		if v.side != 0 {
			continue
		}
		for k, gr := range v.path {
			depthBelow[gr] = max(depthBelow[gr], len(v.path)-k-1)
			b := &gb[gr]
			y0, y1 := v.y-v.h/2, v.y+v.h/2
			if !b.set {
				*b = bounds{math.Inf(1), y0, math.Inf(-1), y1, true}
				continue
			}
			b.y0, b.y1 = min(b.y0, y0), max(b.y1, y1)
		}
	}
	for _, v := range g.v {
		if v.side == 0 || !gb[v.group].set {
			continue
		}
		b := &gb[v.group]
		b.x0, b.x1 = min(b.x0, v.x), max(b.x1, v.x)
	}

	// Everything turned to the drawing's direction, then moved so the drawing starts at (0, 0).
	lo := pt{math.Inf(1), math.Inf(1)}
	hi := pt{math.Inf(-1), math.Inf(-1)}
	grow := func(x0, y0, x1, y1 float64) {
		lo.x, lo.y, hi.x, hi.y = min(lo.x, x0), min(lo.y, y0), max(hi.x, x1), max(hi.y, y1)
	}
	nodes := make([]pt, n)
	for i := range n {
		v := g.v[i]
		x0, y0, x1, y1 := g.box(v.x, v.y, v.w, v.h)
		nodes[i] = pt{(x0 + x1) / 2, (y0 + y1) / 2}
		grow(x0, y0, x1, y1)
	}
	for _, line := range edges {
		for k, p := range line {
			x, y := g.frame(p.x, p.y)
			line[k] = pt{x, y}
			grow(x, y, x, y)
		}
	}
	for i := range labels {
		if hasLabel[i] {
			x, y := g.frame(labels[i].x, labels[i].y)
			labels[i] = pt{x, y}
			l := g.in.Edges[i].Label
			grow(x-float64(l.W)/2, y-float64(l.H)/2, x+float64(l.W)/2, y+float64(l.H)/2)
		}
	}
	groups := make([][4]float64, g.nGroups)
	for gi, b := range gb {
		if !b.set {
			continue
		}
		p := g.pad * float64(1+depthBelow[gi])
		x0, y0, x1, y1 := g.box((b.x0+b.x1)/2, (b.y0+b.y1)/2, b.x1-b.x0, b.y1-b.y0+2*p)
		groups[gi] = [4]float64{x0, y0, x1, y1}
		grow(x0, y0, x1, y1)
	}
	if math.IsInf(lo.x, 1) {
		lo, hi = pt{}, pt{}
	}
	f := func(x float64) float32 { return float32(x) }
	res := Result{
		Nodes:  make([]Point, n),
		Edges:  make([][]Point, len(edges)),
		Labels: make([]Point, len(labels)),
		Groups: make([]Rect, g.nGroups),
		Size:   Size{W: f(hi.x - lo.x), H: f(hi.y - lo.y)},
	}
	for i, p := range nodes {
		res.Nodes[i] = Point{f(p.x - lo.x), f(p.y - lo.y)}
	}
	for i, line := range edges {
		res.Edges[i] = make([]Point, len(line))
		for k, p := range line {
			res.Edges[i][k] = Point{f(p.x - lo.x), f(p.y - lo.y)}
		}
	}
	for i, p := range labels {
		if hasLabel[i] {
			res.Labels[i] = Point{f(p.x - lo.x), f(p.y - lo.y)}
		}
	}
	for gi, b := range groups {
		if gb[gi].set {
			res.Groups[gi] = Rect{f(b[0] - lo.x), f(b[1] - lo.y), f(b[2] - b[0]), f(b[3] - b[1])}
		}
	}
	return res
}
