package mermaid

import (
	"container/heap"
	"context"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

func init() { register(pm.Block, layAs(LayBlock), nil) }

// A BLOCK DIAGRAM IN CELLS: its blocks on the grid the diagram gives, as Mermaid lays them. A grid's
// columns are as wide as their widest one-column block, a block spanning columns as wide as they are
// together with the gaps between, every block in a column the column's width, so the grid's edges
// line up. A group is a frame round its own grid. A space is an empty cell of the grid.
//
// The grid is fixed, so links are routed over it afterwards: each the cheapest path of cells from a
// side of its source to a side of its target, around every box, across another link or a frame's
// border only straight through it, a turn costing more than a step, so a link runs straight where it
// can. Its arrowhead points into its target; its label sits on its longest straight run when that
// holds it, else is a footnote. A link with no path is a footnote naming its ends, never dropped.
//
//	┌───────┐    ┌─────────┐
//	│   a   ├───►│    b    │
//	└───┬───┘    └─────────┘
//	    │ text
//	    ▼
//	┌───────┐
//	│   c   │
//	└───────┘

// The gaps between a grid's columns and between its rows: room for links to pass.
const (
	blockColGap = 4
	blockRowGap = 2
)

// LayBlock lays a block diagram out in cells.
func LayBlock(ctx context.Context, d *pm.BlockDiagram, m Measurer) (*Grid, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := newGrid()
	b := &blockLayout{m: m, rects: map[string]tui.Rect{}, g: g}
	lay := b.group(&d.Root)
	lay.place(1, 1)
	// every link's line first, then their labels, which keep off every line
	paths := make([][]cell, len(d.Edges))
	for i, e := range d.Edges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		paths[i] = b.route(e)
	}
	for i, e := range d.Edges {
		if paths[i] != nil && e.Label != "" {
			b.label(paths[i], e.Label)
		}
	}
	return g.finish(m)
}

// blockLayout lays a diagram's groups out and keeps where each block and group went, by id.
type blockLayout struct {
	m     Measurer
	g     *Grid
	rects map[string]tui.Rect
	boxes []tui.Rect // every box drawn: what a link goes round
}

// laidGroup is a group measured: its size, and how to draw it at a place.
type laidGroup struct {
	w, h  int
	place func(x, y int)
}

// group measures g's grid and returns its size and its placer.
func (b *blockLayout) group(g *pm.BlockGroup) laidGroup {
	cols := g.Columns
	if cols == 0 {
		for _, it := range g.Items {
			cols += it.Width
		}
		cols = max(cols, 1)
	}
	type at struct {
		row, col, w int
		iw, ih      int // the item's own size
		sub         laidGroup
	}
	ats := make([]at, len(g.Items))
	row, col := 0, 0
	for i, it := range g.Items {
		w := min(it.Width, cols)
		if col+w > cols {
			row, col = row+1, 0
		}
		a := at{row: row, col: col, w: w}
		switch {
		case it.Group != nil:
			a.sub = b.group(it.Group)
			a.iw, a.ih = a.sub.w+4, a.sub.h+4
		case it.Space:
			a.iw, a.ih = 1, 3
		default:
			a.iw, a.ih = boxSize(FormOfShape(it.Shape), it.Label, b.m)
		}
		ats[i] = a
		col += w
	}
	rows := row + 1
	if len(g.Items) == 0 {
		rows = 0
	}
	colW, rowH := make([]int, cols), make([]int, rows)
	for _, a := range ats {
		if a.w == 1 {
			colW[a.col] = max(colW[a.col], a.iw)
		}
		rowH[a.row] = max(rowH[a.row], a.ih)
	}
	for i := range colW {
		colW[i] = max(colW[i], 3)
	}
	span := func(a at) int {
		w := (a.w - 1) * blockColGap
		for c := a.col; c < a.col+a.w; c++ {
			w += colW[c]
		}
		return w
	}
	for _, a := range ats { // a spanning item wider than its columns widens the last of them
		if d := a.iw - span(a); d > 0 {
			colW[a.col+a.w-1] += d
		}
	}
	w := (cols - 1) * blockColGap
	for _, c := range colW {
		w += c
	}
	h := max(rows-1, 0) * blockRowGap
	for _, r := range rowH {
		h += r
	}
	return laidGroup{w: max(w, 1), h: max(h, 1), place: func(x0, y0 int) {
		for i, it := range g.Items {
			a := ats[i]
			x := x0 + a.col*blockColGap
			for c := range a.col {
				x += colW[c]
			}
			y := y0 + a.row*blockRowGap
			for r := range a.row {
				y += rowH[r]
			}
			switch {
			case it.Group != nil:
				r := tui.Rect{X: x, Y: y, W: span(a), H: a.ih}
				b.g.frame(r, "")
				a.sub.place(x+2, y+2)
				if it.ID != "" {
					b.rects[it.ID] = r
				}
			case it.Space:
			default:
				r := tui.Rect{X: x, Y: y + (rowH[a.row]-a.ih)/2, W: span(a), H: a.ih}
				b.g.boxIn(Box{ID: it.ID, Label: it.Label, Form: FormOfShape(it.Shape), Ports: PortAll}, r, b.m)
				b.rects[it.ID] = r
				b.boxes = append(b.boxes, r)
			}
		}
	}}
}

// blockStep is a step's direction: its offsets and the dirs it leaves and enters a cell by.
type blockStep struct {
	dx, dy    int
	out, into dirs // the dirs bit leaving a cell this way, and entering the next
}

var blockSteps = []blockStep{{0, -1, north, south}, {0, 1, south, north}, {-1, 0, west, east}, {1, 0, east, west}}

// route draws link e: the cheapest path from a cell beside its source's side to one beside its
// target's, and its ends' marks, and returns the path; with none, a footnote naming its ends (and
// its label), and nil.
func (b *blockLayout) route(e pm.Edge) []cell {
	from, to := b.rects[e.From], b.rects[e.To]
	path := b.path(from, to)
	if path == nil {
		label := e.From + " → " + e.To
		if e.Label != "" {
			label += ": " + e.Label
		}
		b.g.footnote(label)
		return nil
	}
	// the line: each cell's dirs toward its neighbours on the path, and at each end toward its box
	for i, c := range path {
		if i > 0 {
			b.g.lines[c] |= towards(c, path[i-1])
		}
		if i+1 < len(path) {
			b.g.lines[c] |= towards(c, path[i+1])
		}
	}
	first, last := path[0], path[len(path)-1]
	b.g.lines[first] |= towardsRect(first, from)
	b.g.lines[last] |= towardsRect(last, to)
	stroke := strokeOf(e.Line)
	for _, c := range path {
		b.g.addStroke(c, stroke)
	}
	if mk := blockEnd(e.Head, last, to); mk != "" {
		b.g.marks[last] = mk
	}
	if mk := blockEnd(e.Tail, first, from); mk != "" {
		b.g.marks[first] = mk
	}
	return path
}

// blockEnd is the mark an end of kind a draws at c, beside r, pointing into it.
func blockEnd(a pm.Arrow, c cell, r tui.Rect) string {
	var e End
	switch a {
	case pm.Head:
		e = EndArrow
	case pm.Dot:
		e = EndDot
	case pm.Cross:
		e = EndCross
	default:
		return ""
	}
	if e != EndArrow {
		return endMark(e, true, pm.TB)
	}
	switch {
	case c.y < r.Y:
		return "▼"
	case c.y >= r.Y+r.H:
		return "▲"
	case c.x < r.X:
		return "►"
	}
	return "◄"
}

// label puts s beside the path, as the router does: over or under the middle of its longest
// horizontal run, else right or left of the middle of its longest vertical one, a cell clear of the
// line; else a footnote, its mark in the first cell beside the path with room for it.
func (b *blockLayout) label(path []cell, s string) {
	_, w, _ := capLabel(s, RoleLabel, b.m)
	// runs: the path's straight stretches, ends excluded (they meet the boxes)
	type run struct{ from, n int }
	var across, down run
	for i := 1; i < len(path)-1; {
		j := i
		for j+1 < len(path)-1 && path[j+1].y == path[i].y {
			j++
		}
		if n := j - i + 1; n > across.n {
			across = run{i, n}
		}
		k := i
		for k+1 < len(path)-1 && path[k+1].x == path[i].x {
			k++
		}
		if n := k - i + 1; n > down.n {
			down = run{i, n}
		}
		i = max(j, k) + 1
	}
	if across.n >= w {
		a, z := path[across.from], path[across.from+across.n-1]
		x := min(a.x, z.x) + (across.n-w)/2
		for _, y := range []int{a.y - 1, a.y + 1} {
			if b.room(x, y, w) {
				b.g.label(x, y, s, b.m)
				return
			}
		}
	}
	if down.n > 0 {
		c := path[down.from+down.n/2]
		switch {
		case b.room(c.x+2, c.y, w) && !b.taken(cell{c.x + 1, c.y}):
			b.g.label(c.x+2, c.y, s, b.m)
			return
		case b.room(c.x-1-w, c.y, w) && !b.taken(cell{c.x - 1, c.y}):
			b.g.label(c.x-1-w, c.y, s, b.m)
			return
		}
	}
	mk := b.g.footnote(s)
	mw := b.m.StringWidth(mk)
	for k := range path {
		c := path[(len(path)/2+k)%len(path)]
		for _, at := range []cell{{c.x + 1, c.y}, {c.x - mw, c.y}, {c.x, c.y - 1}, {c.x, c.y + 1}} {
			if b.room(at.x, at.y, mw) {
				b.g.label(at.x, at.y, mk, b.m)
				return
			}
		}
	}
}

// room reports whether the w cells from (x, y) rightward are all clear.
func (b *blockLayout) room(x, y, w int) bool {
	for i := range w {
		if b.taken(cell{x + i, y}) {
			return false
		}
	}
	return true
}

// taken reports whether c holds a line, a mark, a label, a frame's border or part of a box, or is off
// the drawing's top or left.
func (b *blockLayout) taken(c cell) bool {
	if _, line := b.g.lines[c]; line || b.g.marks[c] != "" || b.inBox(c) || b.g.onFrame(c) || c.x < 0 || c.y < 0 {
		return true
	}
	for _, t := range b.g.texts {
		if c.y == t.y && c.x >= t.x && c.x < t.x+t.w {
			return true
		}
	}
	return false
}

func (b *blockLayout) inBox(c cell) bool {
	for _, r := range b.boxes {
		if c.x >= r.X && c.x < r.X+r.W && c.y >= r.Y && c.y < r.Y+r.H {
			return true
		}
	}
	return false
}

// towards is the dir from c to its neighbour n.
func towards(c, n cell) dirs {
	switch {
	case n.y < c.y:
		return north
	case n.y > c.y:
		return south
	case n.x < c.x:
		return west
	}
	return east
}

// towardsRect is the dir from c, beside r, into r.
func towardsRect(c cell, r tui.Rect) dirs {
	switch {
	case c.y < r.Y:
		return south
	case c.y >= r.Y+r.H:
		return north
	case c.x < r.X:
		return east
	}
	return west
}

// besides are the cells beside r's sides, its corners left out: where a link may start or end, each
// with its cost, twice its distance from its side's middle, so a link meets a side's middle when it
// is free.
func besides(r tui.Rect) map[cell]int {
	out := map[cell]int{}
	add := func(c cell, off int) {
		if old, ok := out[c]; !ok || 2*off < old {
			out[c] = 2 * off
		}
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		add(cell{x, r.Y - 1}, abs(x-cx))
		add(cell{x, r.Y + r.H}, abs(x-cx))
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		add(cell{r.X - 1, y}, abs(y-cy))
		add(cell{r.X + r.W, y}, abs(y-cy))
	}
	if r.W <= 2 || r.H <= 2 { // too small for a side between corners: the middles
		for _, c := range []cell{{cx, r.Y - 1}, {cx, r.Y + r.H}, {r.X - 1, cy}, {r.X + r.W, cy}} {
			add(c, 0)
		}
	}
	return out
}

// path is the cheapest path of cells from beside from to beside to, or nil. A step costs 1, a turn
// 3 more, crossing another link or a frame's border 2 more, and each end its cell's cost (besides);
// it never enters a box or a mark, runs along or turns on another link, runs along or turns on a
// frame's border, or ends on a crossing.
func (b *blockLayout) path(from, to tui.Rect) []cell {
	w, h := 0, 0
	for _, r := range b.boxes {
		w, h = max(w, r.X+r.W+2), max(h, r.Y+r.H+2)
	}
	for _, f := range b.g.frames {
		w, h = max(w, f.rect.X+f.rect.W+2), max(h, f.rect.Y+f.rect.H+2)
	}
	ok := func(c cell) bool { return c.x >= 0 && c.y >= 0 && c.x < w && c.y < h && !b.inBox(c) }
	goal := besides(to) // the cost of ending at each
	const done = -2     // the dir of a state that has ended at its cell, its goal cost paid
	type state struct {
		c   cell
		dir int // the step taken into c, -1 at a start
	}
	dist := map[state]int{}
	prev := map[state]state{}
	pq := &blockQueue{}
	starts := besides(from)
	for c, cost := range starts {
		if ok(c) && !b.blocked(c, -1) {
			s := state{c, -1}
			dist[s] = cost
			heap.Push(pq, blockItem{s.c, s.dir, cost})
		}
	}
	for pq.Len() > 0 {
		it := heap.Pop(pq).(blockItem)
		s := state{it.c, it.dir}
		if it.cost > dist[s] {
			continue
		}
		if s.dir == done {
			var path []cell
			for cur := prev[s]; ; cur = prev[cur] {
				path = append(path, cur.c)
				if cur.dir == -1 {
					break
				}
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		if g, end := goal[s.c]; end && ok(s.c) && (s.dir < 0 || !b.straightOnly(s.c)) {
			ds := state{s.c, done}
			if old, seen := dist[ds]; !seen || it.cost+g < old {
				dist[ds], prev[ds] = it.cost+g, s
				heap.Push(pq, blockItem{s.c, done, it.cost + g})
			}
		}
		for d, st := range blockSteps {
			n := cell{s.c.x + st.dx, s.c.y + st.dy}
			if !ok(n) || b.blocked(n, d) || s.dir >= 0 && s.dir != d && b.straightOnly(s.c) {
				continue
			}
			cost := it.cost + 1 + b.crossing(n)
			if s.dir >= 0 && s.dir != d {
				cost += 3
			}
			ns := state{n, d}
			if old, seen := dist[ns]; !seen || cost < old {
				dist[ns], prev[ns] = cost, s
				heap.Push(pq, blockItem{n, d, cost})
			}
		}
	}
	return nil
}

// blocked reports whether a step in direction d (an index into blockSteps, -1 a start) may not enter
// c: along a line already there, or along a frame's border.
func (b *blockLayout) blocked(c cell, d int) bool {
	if b.g.marks[c] != "" {
		return true
	}
	if dd, line := b.g.lines[c]; line {
		if d < 0 {
			return true
		}
		st := blockSteps[d]
		if dd&(st.out|st.into) != 0 { // the line there runs the same way: it would join it
			return true
		}
	}
	for _, f := range b.g.frames {
		if !onBorder(f.rect, c) {
			continue
		}
		if d < 0 {
			return true
		}
		vertical := c.x == f.rect.X || c.x == f.rect.X+f.rect.W-1
		horizontal := c.y == f.rect.Y || c.y == f.rect.Y+f.rect.H-1
		st := blockSteps[d]
		if vertical && horizontal || vertical && st.dx == 0 || horizontal && st.dy == 0 {
			return true
		}
	}
	return false
}

// straightOnly reports whether a path may only go straight on through c: across a line or a frame.
func (b *blockLayout) straightOnly(c cell) bool {
	_, line := b.g.lines[c]
	return line || b.g.onFrame(c)
}

// crossing is the extra cost of entering c: crossing another link or a frame's border.
func (b *blockLayout) crossing(c cell) int {
	if b.straightOnly(c) {
		return 2
	}
	return 0
}

// blockItem and blockQueue are the path search's frontier, cheapest first.
type blockItem struct {
	c    cell
	dir  int
	cost int
}

type blockQueue []blockItem

func (q blockQueue) Len() int           { return len(q) }
func (q blockQueue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q blockQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *blockQueue) Push(x any)        { *q = append(*q, x.(blockItem)) }
func (q *blockQueue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}
