package mermaid

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A FLOWCHART IN CELLS (prototype; not among Kinds yet). graph/layout's layered layout ranks and
// orders the nodes, in cells; every edge is then routed on the grid between the ranks, channel
// routing as a chip's wires are: in the gap between two ranks an edge is a vertical from its upper
// end, a horizontal on a track of the gap, and a vertical to its lower end, two bends at most. A
// long edge does that in each gap it crosses and passes each rank between at the column the layout
// gave its dummy there. All the work is in a frame where ranks run down (TB); LR, BT and RL are
// that frame turned, so the router has one case.
//
// Tracks are a gap's own: two edges share one only where their horizontal runs don't overlap
// (the left-edge algorithm). A lower end in a column another net drops from takes a dogleg beside
// it (fcTerm), so verticals never run together; the vertical-constraint graph that would otherwise
// order the tracks then has nothing to order, and can't go round in a cycle.
// A fan-out keeps one net: the edges a node sends down its port share one track, and split from it
// (┬ ├ ┤ come from the OR'd masks). A fan-in shares only the target's own port, below its tracks.
// Crossings are a vertical through another net's track, so they read ┼.
//
// Not yet (later PRs): edge labels, dotted and thick lines, subgraph frames, self-loops and edges
// between nodes of one rank (both left out), and the too-wide notice. An edge back up the ranks (a
// cycle) is routed as any other, by ports beside the forward ones.

// Frame spacing, in cells: a box's clearance from its neighbours on a rank (room for a long
// edge's column between them, a cell clear of each), and the first guess at the gap between
// ranks (each gap is then as tall as its tracks need).
const (
	fcNodeSep = 3
	fcRankSep = 4
)

// formOfShape is the outline a flowchart node's shape is drawn with: the nearest corner set.
func formOfShape(s pm.Shape) Form {
	switch s {
	case pm.Round, pm.Stadium, pm.Circle, pm.DoubleCircle, pm.Cylinder:
		return FormRounded
	case pm.Diamond, pm.Hexagon:
		return FormHexagon
	}
	return FormSquare
}

// fcNode is a node in the frame: ranks run down it, whatever the diagram's direction.
type fcNode struct {
	rank int
	x, y int // the frame top-left
	w, h int // the frame size
}

// port is the frame column of n's ports: its top's and bottom's middle, where its box takes a
// stem. An edge back up the ranks keeps beside it, a column over, so it never shares a forward
// edge's vertical.
func (n fcNode) port(back bool) int {
	if back {
		return n.x + n.w/2 + 1
	}
	return n.x + n.w/2
}

// fcEdge is an edge being routed: its nodes, the ranks it spans, and the column it passes each
// rank between at.
type fcEdge struct {
	e        pm.Edge
	from, to int
	lo, hi   int         // its upper and lower rank
	back     bool        // it runs up the ranks: from its lower node to its upper
	cols     map[int]int // a rank strictly between lo and hi → the column it passes it at
	drawn    bool        // routed: false for a self-loop, an edge within one rank, an invisible edge
	lowCol   int         // the column it meets its lower node's top at (lowPort)
}

// lowPort is the column, on n's top, that edges come down into it at (back false) or leave it up by
// (back true), in a gap where taken holds the columns of upper ends whose edges go elsewhere: the
// middle (where the box takes a stem) or the nearest cell of its top beside it. So the port is never
// in a column another edge drops from (two boxes stacked in one column would otherwise make the
// gap's verticals run together), while an edge straight down into it keeps its column; and the two
// kinds never share one. avoid is the other kind's, -1 for none.
func (n fcNode) lowPort(back bool, taken map[int]bool, avoid int) int {
	mid := n.x + n.w/2
	if back {
		mid++
	}
	for d := 0; d < n.w; d++ {
		for _, c := range []int{mid - d, mid + d} {
			if c > n.x && c < n.x+n.w-1 && !taken[c] && c != avoid {
				return c
			}
		}
	}
	return mid
}

// fcTerm is a net's end on a gap's side: a node's port, or a long edge's column (node -1).
//
// A lower end in a column another net drops from in that gap takes a dogleg: its net's track meets
// it at a free column beside (jogCol), and it comes down that column to the gap's jog row, the row
// above its last, crosses on it to its own column, and drops into its node or on past. The other
// net's vertical ends at its own track, above the jog row, so the two never run together, and no
// net need be above another (the vertical-constraint graph stays empty).
type fcTerm struct {
	col, node int
	jog       bool
	jogCol    int
}

// at is the column t's net's track meets t at: its own, or its dogleg's.
func (t fcTerm) at() int {
	if t.jog {
		return t.jogCol
	}
	return t.col
}

// fcNet is what a gap routes as one: an edge, or the edges a node sends down (or up) its port.
type fcNet struct {
	tops, bots []fcTerm // its ends on the gap's upper and lower side
	edges      []int
	ends       map[int][2]fcTerm // each edge's own two ends in the gap
	lo, hi     int               // the columns its track runs between
	track      int               // -1: a straight drop, no track
}

// by is the net's edges that end at t, on the gap's upper side (top) or its lower.
func (n *fcNet) by(t fcTerm, top bool) []int {
	var out []int
	for _, k := range n.edges {
		if e := n.ends[k]; top && e[0] == t || !top && e[1] == t {
			out = append(out, k)
		}
	}
	return out
}

// netKey names a gap's net: the edge itself, or the node whose port it leaves by.
type netKey struct{ kind, n int }

// LayFlowchart lays d out in cells: ranks and order from graph/layout's layered layout, every edge
// routed between the ranks, its ends marked. layout.ErrTooLarge comes back as it is, for the
// host's "too large" notice.
func LayFlowchart(ctx context.Context, d *pm.FlowchartDiagram, m Measurer) (*Grid, error) {
	g, _, err := layFlowchart(ctx, d, m)
	return g, err
}

// layFlowchart is LayFlowchart, also giving each edge's links (the pairs of neighbouring cells its
// line joins), in d.Edges order: the cells check a route by them.
func layFlowchart(ctx context.Context, d *pm.FlowchartDiagram, m Measurer) (*Grid, [][][2]cell, error) {
	g := newGrid()
	if len(d.Nodes) == 0 {
		return g, nil, nil
	}
	across := d.Dir == pm.LR || d.Dir == pm.RL
	index := map[string]int{}
	nodes := make([]fcNode, len(d.Nodes))
	in := layout.Input{Dir: layout.TB, NodeSep: fcNodeSep, RankSep: fcRankSep}
	for i, n := range d.Nodes {
		index[n.ID] = i
		w, h := formSize(formOfShape(n.Shape), m.StringWidth(n.Label))
		if across {
			w, h = h, w
		}
		nodes[i].w, nodes[i].h = w, h
		in.Nodes = append(in.Nodes, layout.Size{W: float32(w), H: float32(h)})
	}
	var edges []fcEdge
	routes := make([][][2]cell, len(d.Edges))
	at := []int{} // edges[i] is d.Edges[at[i]]
	for i, e := range d.Edges {
		f, okf := index[e.From]
		t, okt := index[e.To]
		if !okf || !okt {
			continue
		}
		in.Edges = append(in.Edges, layout.Edge{From: f, To: t, MinLen: max(e.MinLen, 1)})
		edges = append(edges, fcEdge{e: e, from: f, to: t})
		at = append(at, i)
	}
	res, err := layout.Layered(ctx, in, layout.Limits{})
	if err != nil {
		return nil, nil, err
	}

	// ranks: the layout's rows of centres, top to bottom
	var ys []float64
	for _, p := range res.Nodes {
		ys = append(ys, float64(p.Y))
	}
	slices.Sort(ys)
	ys = slices.CompactFunc(ys, func(a, b float64) bool { return math.Abs(a-b) < 0.5 })
	rankOf := func(y float64) int {
		best := 0
		for r := range ys {
			if math.Abs(ys[r]-y) < math.Abs(ys[best]-y) {
				best = r
			}
		}
		return best
	}
	ranks := make([][]int, len(ys))
	for i, p := range res.Nodes {
		nodes[i].rank = rankOf(float64(p.Y))
		ranks[nodes[i].rank] = append(ranks[nodes[i].rank], i)
	}
	// a rank's boxes in the layout's order, on whole cells, each clear of the one before
	bandH := make([]int, len(ranks))
	for r, ids := range ranks {
		slices.SortStableFunc(ids, func(a, b int) int { return cmp.Compare(res.Nodes[a].X, res.Nodes[b].X) })
		next := math.MinInt
		for _, i := range ids {
			x := int(math.Round(float64(res.Nodes[i].X) - float64(nodes[i].w)/2))
			nodes[i].x = max(x, next)
			next = nodes[i].x + nodes[i].w + fcNodeSep
			bandH[r] = max(bandH[r], nodes[i].h)
		}
	}

	// each edge's span, and its column on every rank between (where the layout put its dummy,
	// moved off any box and any other edge's column)
	used := make([]map[int]bool, len(ranks))
	for r := range used {
		used[r] = map[int]bool{}
	}
	free := func(r, c int) bool {
		if used[r][c] {
			return false
		}
		for _, i := range ranks[r] {
			if c >= nodes[i].x-1 && c <= nodes[i].x+nodes[i].w {
				return false
			}
		}
		return true
	}
	for k := range edges {
		e := &edges[k]
		rf, rt := nodes[e.from].rank, nodes[e.to].rank
		if rf == rt || e.e.Line == pm.Invisible { // not routed yet; an invisible one is only placed
			continue
		}
		e.drawn, e.back = true, rf > rt
		e.lo, e.hi = min(rf, rt), max(rf, rt)
		e.cols = map[int]int{}
		line := res.Edges[k]
		for r := e.lo + 1; r < e.hi; r++ {
			c := int(math.Round(xAt(line, ys[r])))
			for step := 0; !free(r, c); step++ {
				if step%2 == 0 {
					c += step + 1
				} else {
					c -= step + 1
				}
			}
			used[r][c] = true
			e.cols[r] = c
		}
	}

	// the gaps' nets, and their tracks
	type gapNets struct{ nets []*fcNet }
	gaps := make([]gapNets, max(len(ranks)-1, 0))
	jogs := make([]bool, len(gaps)) // a gap with a dogleg: one more row, its jog row
	for gi := range gaps {
		byKey := map[netKey]*fcNet{}
		var order []netKey
		ends := func(e fcEdge) (upper, lower int) { // the nodes at its upper and lower rank
			if e.back {
				return e.to, e.from
			}
			return e.from, e.to
		}
		topOf := func(e fcEdge) fcTerm {
			if gi == e.lo {
				u, _ := ends(e)
				return fcTerm{col: nodes[u].port(e.back), node: u}
			}
			return fcTerm{col: e.cols[gi], node: -1}
		}
		keyOf := func(k int, e fcEdge) netKey { // the net the edge is part of, in this gap
			switch {
			case !e.back && gi == e.lo:
				return netKey{1, e.from} // a fan-out: the node's bottom port
			case e.back && gi+1 == e.hi:
				return netKey{2, e.from} // up from the node's top
			}
			return netKey{0, k}
		}
		type upperEnd struct {
			col, lower int // its column, and the node its edge comes down to (-1: it goes on past)
			back       bool
			key        netKey
		}
		var uppers []upperEnd // the gap's upper ends
		for k, e := range edges {
			if e.drawn && gi >= e.lo && gi < e.hi {
				lower := -1
				if gi+1 == e.hi {
					_, lower = ends(e)
				}
				uppers = append(uppers, upperEnd{topOf(e).col, lower, e.back, keyOf(k, e)})
			}
		}
		low := map[[2]int]int{} // (node, back) → its lower port's column in this gap
		lowAt := func(n int, back bool) int {
			b := 0
			if back {
				b = 1
			}
			if c, ok := low[[2]int{n, b}]; ok {
				return c
			}
			avoid, ok := low[[2]int{n, 1 - b}]
			if !ok {
				avoid = -1
			}
			// the port may keep an upper end's column only when that end's net is the one net coming
			// into it: with two, each net's column there would have to be above the other's track
			into := map[netKey]bool{}
			for _, u := range uppers {
				if u.lower == n && u.back == back {
					into[u.key] = true
				}
			}
			taken := map[int]bool{}
			for _, u := range uppers {
				if len(into) > 1 || !into[u.key] {
					taken[u.col] = true
				}
			}
			c := nodes[n].lowPort(back, taken, avoid)
			low[[2]int{n, b}] = c
			return c
		}
		topCols, botCols := map[int]bool{}, map[int]bool{}
		for _, u := range uppers {
			topCols[u.col] = true
		}
		for k := range edges {
			e := &edges[k]
			if !e.drawn || gi < e.lo || gi >= e.hi {
				continue
			}
			_, lower := ends(*e)
			key := keyOf(k, *e)
			top, bot := topOf(*e), fcTerm{col: e.cols[gi+1], node: -1}
			if gi+1 == e.hi {
				bot = fcTerm{col: lowAt(lower, e.back), node: lower}
				e.lowCol = bot.col
			}
			if slices.ContainsFunc(uppers, func(u upperEnd) bool { return u.col == bot.col && u.key != key }) {
				for d := 1; ; d++ { // a dogleg: the nearest column no end of the gap is in
					if c := bot.col - d; !topCols[c] && !botCols[c] {
						bot.jog, bot.jogCol = true, c
						break
					}
					if c := bot.col + d; !topCols[c] && !botCols[c] {
						bot.jog, bot.jogCol = true, c
						break
					}
				}
				botCols[bot.jogCol] = true
				jogs[gi] = true
			}
			botCols[bot.col] = true
			n := byKey[key]
			if n == nil {
				n = &fcNet{track: -1, ends: map[int][2]fcTerm{}}
				byKey[key] = n
				order = append(order, key)
			}
			if !slices.Contains(n.tops, top) {
				n.tops = append(n.tops, top)
			}
			if !slices.Contains(n.bots, bot) {
				n.bots = append(n.bots, bot)
			}
			n.edges = append(n.edges, k)
			n.ends[k] = [2]fcTerm{top, bot}
		}
		for _, key := range order {
			gaps[gi].nets = append(gaps[gi].nets, byKey[key])
		}
	}
	gapH := make([]int, len(gaps))
	for gi := range gaps {
		gapH[gi] = assignTracks(gaps[gi].nets) + 2
		if jogs[gi] {
			gapH[gi]++
		}
	}

	// the frame rows: each rank's band, then its gap
	bandY := make([]int, len(ranks))
	for r := 1; r < len(ranks); r++ {
		bandY[r] = bandY[r-1] + bandH[r-1] + gapH[r-1]
	}
	for i := range nodes {
		nodes[i].y = bandY[nodes[i].rank] + (bandH[nodes[i].rank]-nodes[i].h)/2
	}

	// draw, in the frame, into the grid turned to the diagram's direction
	toReal := func(c cell) cell {
		switch d.Dir {
		case pm.BT:
			return cell{c.x, -c.y}
		case pm.LR:
			return cell{c.y, c.x}
		case pm.RL:
			return cell{-c.y, c.x}
		}
		return c
	}
	draw := func(a, b cell, by []int) {
		ra, rb := toReal(a), toReal(b)
		g.run(ra, rb)
		for _, k := range by {
			routes[at[k]] = append(routes[at[k]], runLinks(ra, rb)...)
		}
	}
	for gi, gn := range gaps {
		trackY := bandY[gi] + bandH[gi] + 1
		for _, n := range gn.nets {
			startY := func(t fcTerm) int {
				if t.node >= 0 {
					return nodes[t.node].y + nodes[t.node].h
				}
				return bandY[gi] + bandH[gi]
			}
			endY := func(t fcTerm) int {
				if t.node >= 0 {
					return nodes[t.node].y - 1
				}
				return bandY[gi+1] - 1
			}
			if n.track < 0 { // a straight drop: one column, top to bottom
				for _, t := range n.tops {
					for _, b := range n.bots {
						draw(cell{t.col, startY(t)}, cell{b.col, endY(b)}, n.edges)
					}
				}
				continue
			}
			y := trackY + n.track
			for _, t := range n.tops {
				draw(cell{t.col, startY(t)}, cell{t.col, y}, n.by(t, true))
			}
			draw(cell{n.lo, y}, cell{n.hi, y}, n.edges)
			for _, b := range n.bots {
				if b.jog { // down its dogleg's column, across the jog row, into its own
					jogY := bandY[gi+1] - 2
					by := n.by(b, false)
					draw(cell{b.jogCol, y}, cell{b.jogCol, jogY}, by)
					draw(cell{b.jogCol, jogY}, cell{b.col, jogY}, by)
					draw(cell{b.col, jogY}, cell{b.col, endY(b)}, by)
					continue
				}
				draw(cell{b.col, y}, cell{b.col, endY(b)}, n.by(b, false))
			}
		}
	}
	// a long edge through the ranks it passes
	for k, e := range edges {
		for r, c := range e.cols {
			draw(cell{c, bandY[r] - 1}, cell{c, bandY[r] + bandH[r]}, []int{k})
		}
	}
	// its ends: the cell beside each node's port, the mark pointing into the node
	for _, e := range edges {
		if !e.drawn {
			continue
		}
		to, from := nodes[e.to], nodes[e.from]
		// the To end: a forward edge comes down into its top, a back edge up into its bottom
		// the cell beside each end's port, in the frame: the lower node's top (its lowCol), the upper
		// node's bottom (its port); the line there points into the box too, so the box meets it
		toEnd, fromEnd := cell{e.lowCol, to.y - 1}, cell{from.port(false), from.y + from.h}
		if e.back {
			toEnd, fromEnd = cell{to.port(true), to.y + to.h}, cell{e.lowCol, from.y - 1}
		}
		// an end with a mark meets its box by the mark, the outline plain under it; one without, by
		// its line, the outline taking a junction there
		end := func(c cell, up bool, mk string) { // up: the box is above c in the frame
			if mk != "" {
				g.marks[toReal(c)] = mk
				return
			}
			g.lines[toReal(c)] |= frameDir(up, d.Dir)
		}
		end(toEnd, e.back, endMark(e.e.Head, !e.back, d.Dir))
		end(fromEnd, !e.back, endMark(e.e.Tail, e.back, d.Dir))
	}
	// the boxes, turned
	for i, n := range d.Nodes {
		f := nodes[i]
		a, b := toReal(cell{f.x, f.y}), toReal(cell{f.x + f.w - 1, f.y + f.h - 1}) // two opposite corners
		r := Box{ID: n.ID, Label: n.Label, Form: formOfShape(n.Shape), Stem: !across}
		r.Rect.X, r.Rect.Y = min(a.x, b.x), min(a.y, b.y)
		r.Rect.W, r.Rect.H = max(a.x, b.x)-r.Rect.X+1, max(a.y, b.y)-r.Rect.Y+1
		g.Boxes = append(g.Boxes, r)
	}
	before := g.Boxes[0].Rect
	g.settle()
	dx, dy := g.Boxes[0].Rect.X-before.X, g.Boxes[0].Rect.Y-before.Y
	for i := range routes {
		for j := range routes[i] {
			for s := range routes[i][j] {
				routes[i][j][s].x += dx
				routes[i][j][s].y += dy
			}
		}
	}
	if g.tooLarge() {
		return nil, nil, layout.ErrTooLarge
	}
	return g, routes, nil
}

// assignTracks gives each net that turns a track of its gap and reports how many there are. Nets
// share a track only where their runs don't overlap, a cell clear of each other (the left-edge
// algorithm), and a net whose upper end is in the column of another's lower end takes a higher
// track than it, so their verticals never run together (the vertical-constraint graph). Where the
// constraints go round in a cycle, the leftmost net left is put on the track anyway.
func assignTracks(nets []*fcNet) int {
	var turning []*fcNet
	for _, n := range nets {
		n.lo, n.hi = math.MaxInt, math.MinInt
		for _, t := range slices.Concat(n.tops, n.bots) {
			n.lo, n.hi = min(n.lo, t.at()), max(n.hi, t.at())
		}
		if n.lo < n.hi {
			turning = append(turning, n)
		} else {
			n.track = -1
		}
	}
	// above[q] are the nets that must take a higher track than q
	above := map[*fcNet][]*fcNet{}
	for _, p := range turning {
		for _, q := range turning {
			if p == q {
				continue
			}
			for _, t := range p.tops {
				if slices.ContainsFunc(q.bots, func(b fcTerm) bool { return b.at() == t.col }) {
					above[q] = append(above[q], p)
				}
			}
		}
	}
	slices.SortStableFunc(turning, func(a, b *fcNet) int { return cmp.Or(cmp.Compare(a.lo, b.lo), cmp.Compare(a.hi, b.hi)) })
	left := slices.Clone(turning)
	placed := map[*fcNet]int{}
	track := 0
	for len(left) > 0 {
		end := math.MinInt
		var rest []*fcNet
		for _, n := range left {
			ready := n.lo > end+1
			for _, p := range above[n] {
				if t, ok := placed[p]; !ok || t >= track {
					ready = false
				}
			}
			if ready {
				n.track, placed[n], end = track, track, n.hi
			} else {
				rest = append(rest, n)
			}
		}
		if len(rest) == len(left) { // a cycle: the leftmost goes on anyway
			n := rest[0]
			n.track, placed[n] = track, track
			rest = rest[1:]
		}
		left = rest
		track++
	}
	return track
}

// xAt is the polyline's column where it crosses row y (its first point's when it never does).
func xAt(line []layout.Point, y float64) float64 {
	for i := 0; i+1 < len(line); i++ {
		a, b := line[i], line[i+1]
		ay, by := float64(a.Y), float64(b.Y)
		if (ay <= y && y <= by) || (by <= y && y <= ay) {
			if ay == by {
				return float64(a.X)
			}
			return float64(a.X) + (y-ay)/(by-ay)*float64(b.X-a.X)
		}
	}
	if len(line) == 0 {
		return 0
	}
	return float64(line[0].X)
}

// runLinks is the links a straight run from a to b makes: each pair of neighbouring cells.
func runLinks(a, b cell) [][2]cell {
	var out [][2]cell
	step := func(v, to int) int {
		switch {
		case v < to:
			return v + 1
		case v > to:
			return v - 1
		}
		return v
	}
	for c := a; c != b; {
		n := cell{step(c.x, b.x), step(c.y, b.y)}
		if c.x != b.x && c.y != b.y {
			n.y = c.y
		}
		out = append(out, [2]cell{c, n})
		c = n
	}
	return out
}

// frameDir is the real direction of the frame's north (up) or south, the diagram turned by dir.
func frameDir(up bool, dir pm.Dir) dirs {
	north, south := north, south
	switch dir {
	case pm.BT:
		north, south = south, north
	case pm.LR:
		north, south = west, east
	case pm.RL:
		north, south = east, west
	}
	if up {
		return north
	}
	return south
}

// endMark is the mark an edge's end draws: an arrowhead pointing into its node (down the frame
// when down is set), a dot or a cross; "" for none.
func endMark(a pm.Arrow, down bool, dir pm.Dir) string {
	switch a {
	case pm.Dot:
		return "●"
	case pm.Cross:
		return "✕"
	case pm.Head:
	default:
		return ""
	}
	heads := map[pm.Dir][2]string{pm.BT: {"▲", "▼"}, pm.LR: {"►", "◄"}, pm.RL: {"◄", "►"}}
	h, ok := heads[dir]
	if !ok {
		h = [2]string{"▼", "▲"}
	}
	if down {
		return h[0]
	}
	return h[1]
}
