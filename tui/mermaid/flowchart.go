package mermaid

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

// A FLOWCHART IN CELLS. graph/layout's layered layout ranks and
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
// A subgraph is a frame round its nodes (and its nested subgraphs'), a layer of its own under the
// lines: its borders keep to rows of the gaps no track or jog row is on, and to columns no edge runs
// down, so a line only ever crosses one, straight, drawing its own character there.
//
// Not yet: edges between nodes of one rank (left out), and a subgraph's own direction (it is laid
// in its diagram's). An edge back up the ranks (a cycle) is routed as any other, by ports beside
// the forward ones.

// Frame spacing, in cells: a box's clearance from its neighbours on a rank (room for a long
// edge's column between them, a cell clear of each), the first guess at the gap between ranks
// (each gap is then as tall as its tracks need), and the layout's margin round a subgraph's members
// (its border and a cell inside it).
const (
	fcNodeSep  = 3
	fcRankSep  = 4
	fcGroupPad = 2
)

// fcFrame is a subgraph's frame, in the frame (TB) coordinates the router works in.
type fcFrame struct {
	title    string
	members  []int // its nodes and its nested subgraphs'
	parent   int   // the frame it is nested in, -1 at top level
	lo, hi   int   // its first and last rank
	l, r     int   // its left and right borders' columns
	top, bot int   // its top and bottom borders' rows
}

// near reports whether column c is a border of f or beside one: no edge runs down it, so a line
// meets f's top or bottom only crossing it, and the cell after a corner is always clear (where a
// title that has no room goes as a footnote mark).
func (f fcFrame) near(c int) bool { return abs(c-f.l) <= 1 || abs(c-f.r) <= 1 }

func abs(v int) int { return max(v, -v) }

// FormOfShape is the outline a flowchart node's shape is drawn with: the nearest corner set.
func FormOfShape(s pm.Shape) Form {
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
// it at a free column beside (jogCol), and it comes down that column to a jog row, crosses on it to
// its own column, and drops into its node or on past. The other net's vertical ends at its own
// track, above the jog rows, so the two never run together, and no net need be above another (the
// vertical-constraint graph stays empty). A gap's jogs share its jog rows as nets share tracks:
// only where their crossings don't overlap (jogRows).
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

func init() {
	register(pm.Flowchart, layAs(LayFlowchart), nil) // every flowchart: a subgraph is a frame
	registerNarrower(pm.Flowchart, flowchartDown)
}

// flowchartDown is a flowchart that runs across (LR, RL) as one running down (TB): its narrower
// form (LayWithin); nil for one already running down.
func flowchartDown(d pm.Diagram) pm.Diagram {
	fc, ok := d.(*pm.FlowchartDiagram)
	if !ok || fc.Dir != pm.LR && fc.Dir != pm.RL {
		return nil
	}
	down := *fc
	down.Dir = pm.TB
	return &down
}

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
		w, h := boxSize(FormOfShape(n.Shape), n.Label, m) // capped, as it is drawn
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
	frames := fcFrames(d)
	for _, f := range frames {
		in.Groups = append(in.Groups, f.members)
	}
	in.GroupPad = fcGroupPad
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
	// a rank's boxes in the layout's order, on whole cells, each clear of the one before and of the
	// frames' borders; a frame spans its members' ranks
	bandH := make([]int, len(ranks))
	for r, ids := range ranks {
		slices.SortStableFunc(ids, func(a, b int) int { return cmp.Compare(res.Nodes[a].X, res.Nodes[b].X) })
		for _, i := range ids {
			bandH[r] = max(bandH[r], nodes[i].h)
		}
	}
	for f := range frames {
		frames[f].lo, frames[f].hi = math.MaxInt, math.MinInt
		for _, i := range frames[f].members {
			frames[f].lo, frames[f].hi = min(frames[f].lo, nodes[i].rank), max(frames[f].hi, nodes[i].rank)
		}
	}
	titleW := make([]int, len(frames)) // a frame's room for its title across the frame: TB and BT only
	if !across {
		for f := range frames {
			_, titleW[f], _ = capLabel(frames[f].title, RoleLabel, m) // as the frame draws it
		}
	}
	titleRank := make([]int, len(frames)) // the rank whose edges cross a frame's top border (its title's): TB's first, BT's last
	for f := range frames {
		titleRank[f] = frames[f].lo
		if d.Dir == pm.BT {
			titleRank[f] = frames[f].hi
		}
	}
	placeColumns(nodes, ranks, frames, res, titleW, titleRank)
	// a frame's rows above its first rank and below its last: a border each, outside those of the
	// frames inside it that start (or end) on that rank
	topRows, botRows := make([]int, len(frames)), make([]int, len(frames))
	frameTop, frameBot := make([]int, len(ranks)), make([]int, len(ranks)) // a rank's rows of borders above it, below it
	for f := len(frames) - 1; f >= 0; f-- {                                // a nested frame comes after the one it is in
		topRows[f], botRows[f] = max(topRows[f], 1), max(botRows[f], 1)
		if p := frames[f].parent; p >= 0 {
			if frames[p].lo == frames[f].lo {
				topRows[p] = max(topRows[p], topRows[f]+1)
			}
			if frames[p].hi == frames[f].hi {
				botRows[p] = max(botRows[p], botRows[f]+1)
			}
		}
		frameTop[frames[f].lo] = max(frameTop[frames[f].lo], topRows[f])
		frameBot[frames[f].hi] = max(frameBot[frames[f].hi], botRows[f])
	}
	// a column a line runs down in a gap: never a frame's border or beside one, where the frame is
	// on both of the gap's ranks
	besideFrame := func(gi, c int) bool {
		for _, f := range frames {
			if f.lo <= gi && f.hi > gi && f.near(c) {
				return true
			}
		}
		return false
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
		for _, f := range frames { // never down a frame's border, or beside one, on a rank it spans
			if f.lo <= r && r <= f.hi && f.near(c) {
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
	jogs := make([]bool, len(gaps)) // a gap with a dogleg: it has jog rows
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
		// every end of the gap first, its lower ends' columns all known before any dogleg is placed
		type gapEnd struct {
			k        int
			key      netKey
			top, bot fcTerm
		}
		var gapEnds []gapEnd
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
			top, bot := topOf(*e), fcTerm{col: e.cols[gi+1], node: -1}
			if gi+1 == e.hi {
				bot = fcTerm{col: lowAt(lower, e.back), node: lower}
				e.lowCol = bot.col
			}
			botCols[bot.col] = true
			gapEnds = append(gapEnds, gapEnd{k, keyOf(k, *e), top, bot})
		}
		// a lower end in another net's column takes a dogleg: the nearest column no end of the gap is
		// in, one per net and column (a net's two edges into one end share it)
		jogAt := map[[2]int]int{} // (net, column) → its dogleg's column
		netIndex := map[netKey]int{}
		for i := range gapEnds {
			ge := &gapEnds[i]
			if !slices.ContainsFunc(uppers, func(u upperEnd) bool { return u.col == ge.bot.col && u.key != ge.key }) {
				continue
			}
			ni, ok := netIndex[ge.key]
			if !ok {
				ni = len(netIndex)
				netIndex[ge.key] = ni
			}
			c, ok := jogAt[[2]int{ni, ge.bot.col}]
			for d := 1; !ok; d++ {
				for _, cand := range []int{ge.bot.col - d, ge.bot.col + d} {
					if !ok && !topCols[cand] && !botCols[cand] && !besideFrame(gi, cand) {
						c, ok = cand, true
					}
				}
			}
			jogAt[[2]int{ni, ge.bot.col}] = c
			botCols[c] = true
			ge.bot.jog, ge.bot.jogCol = true, c
			jogs[gi] = true
		}
		for _, ge := range gapEnds {
			k, key, top, bot := ge.k, ge.key, ge.top, ge.bot
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
	// a gap a labelled edge ends in keeps room for its label above the arrow row: a row beside the
	// edge's last drop (TB, BT), or the label's width along it, a cell more for the line (LR, RL)
	labelRows := make([]int, len(gaps))
	for _, e := range edges {
		if e.drawn && !e.back && e.e.Label != "" {
			rows := 1
			if across {
				_, w, _ := capLabel(e.e.Label, RoleLabel, m) // as g.label draws it
				rows = w + 1
			}
			labelRows[e.hi-1] = max(labelRows[e.hi-1], rows)
		}
	}
	gapH := make([]int, len(gaps))
	jogRow := make([]map[fcTerm]int, len(gaps)) // a gap's jogged ends → their jog row (0 the highest)
	jogN := make([]int, len(gaps))
	for gi := range gaps {
		if jogs[gi] {
			jogRow[gi], jogN[gi] = jogRows(gaps[gi].nets)
		}
		gapH[gi] = assignTracks(gaps[gi].nets) + 2 + labelRows[gi] + jogN[gi] + frameBot[gi] + frameTop[gi+1]
	}

	// the frame rows: each rank's band, then its gap
	bandY := make([]int, len(ranks))
	for r := 1; r < len(ranks); r++ {
		bandY[r] = bandY[r-1] + bandH[r-1] + gapH[r-1]
	}
	for i := range nodes {
		nodes[i].y = bandY[nodes[i].rank] + (bandH[nodes[i].rank]-nodes[i].h)/2
	}
	for f := range frames { // its borders' rows: in the gaps above its first rank and below its last
		frames[f].top = bandY[frames[f].lo] - 1 - topRows[f]
		frames[f].bot = bandY[frames[f].hi] + bandH[frames[f].hi] + botRows[f]
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
	stroked := func(c cell, k int) { g.addStroke(c, strokeOf(edges[k].e.Line)) } // finish resolves them
	draw := func(a, b cell, by []int) {
		ra, rb := toReal(a), toReal(b)
		g.run(ra, rb)
		links := runLinks(ra, rb)
		for _, k := range by {
			routes[at[k]] = append(routes[at[k]], links...)
			stroked(ra, k)
			for _, l := range links {
				stroked(l[1], k)
			}
		}
	}
	for gi, gn := range gaps {
		trackY := bandY[gi] + bandH[gi] + 1 + frameBot[gi] // past the bottom borders of the frames ending above
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
					jogY := bandY[gi+1] - 1 - frameTop[gi+1] - labelRows[gi] - jogN[gi] + jogRow[gi][b]
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
	for k, e := range edges {
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
			stroked(toReal(c), k)
		}
		end(toEnd, e.back, endMark(e.e.Head, !e.back, d.Dir))
		end(fromEnd, !e.back, endMark(e.e.Tail, e.back, d.Dir))
	}
	// the frames, turned: a layer under the lines, their titles placed by finish where no line
	// crosses them (before the labels and loops, which keep clear of them)
	for _, f := range frames {
		a, b := toReal(cell{f.l, f.top}), toReal(cell{f.r, f.bot})
		var r tui.Rect
		r.X, r.Y = min(a.x, b.x), min(a.y, b.y)
		r.W, r.H = max(a.x, b.x)-r.X+1, max(a.y, b.y)-r.Y+1
		g.frame(r, f.title)
	}
	// the boxes, turned (before the labels and loops, which keep clear of them)
	for i, n := range d.Nodes {
		f := nodes[i]
		a, b := toReal(cell{f.x, f.y}), toReal(cell{f.x + f.w - 1, f.y + f.h - 1}) // two opposite corners
		// lines meet a box on its top and bottom (TB, BT) or its sides (LR, RL), at any cell of them
		ports := PortTop | PortBottom
		if across {
			ports = PortSides
		}
		var r tui.Rect
		r.X, r.Y = min(a.x, b.x), min(a.y, b.y)
		r.W, r.H = max(a.x, b.x)-r.X+1, max(a.y, b.y)-r.Y+1
		g.boxIn(Box{ID: n.ID, Label: n.Label, Form: FormOfShape(n.Shape), Ports: ports}, r, m)
	}
	// a self-loop is a ↺ beside its box: the first free of its right, left, top-right and
	// bottom-right cells
	inBox := func(c cell) bool {
		for _, b := range g.Boxes {
			if c.x >= b.Rect.X && c.x < b.Rect.X+b.Rect.W && c.y >= b.Rect.Y && c.y < b.Rect.Y+b.Rect.H {
				return true
			}
		}
		return false
	}
	labelled := func(c cell) bool {
		for _, t := range g.texts {
			if c.y == t.y && c.x >= t.x && c.x < t.x+t.w {
				return true
			}
		}
		return false
	}
	occupied := func(c cell) bool {
		if g.onFrame(c) {
			return true
		}
		_, line := g.lines[c]
		return line || inBox(c) || g.marks[c] != "" || labelled(c)
	}
	var notes []fcEdge // labels with no room beside their edge: a footnote each
	for k, e := range edges {
		if e.from != e.to || e.e.Line == pm.Invisible {
			continue
		}
		r := g.Boxes[e.from].Rect
		for _, c := range []cell{{r.X + r.W, r.Y + r.H/2}, {r.X - 1, r.Y + r.H/2}, {r.X + r.W - 1, r.Y - 1}, {r.X + r.W - 1, r.Y + r.H}} {
			if !occupied(c) {
				g.marks[c] = "↺"
				break
			}
		}
		if e.e.Label != "" {
			notes = append(notes, edges[k])
		}
	}
	// a label beside its edge's last drop (TB, BT: on the row kept above its arrow, right of it or
	// else left), or along it (LR, RL: over the straight line kept before its arrow); a footnote when
	// that has no room, and for an edge back up the ranks
	room := func(x, y, w int) bool {
		for i := range w {
			if occupied(cell{x + i, y}) {
				return false
			}
		}
		return true
	}
	for _, e := range edges {
		if !e.drawn || e.e.Label == "" {
			continue
		}
		_, w, _ := capLabel(e.e.Label, RoleLabel, m) // as g.label draws it
		placed := false
		if !e.back {
			arrow := bandY[e.hi] - 1 - frameTop[e.hi] // the frame row its label keeps to: above its arrow and the borders of the frames starting there
			if across {
				lo, hi := toReal(cell{e.lowCol, arrow - 1 - w}), toReal(cell{e.lowCol, arrow - 2})
				x0, x1 := min(lo.x, hi.x), max(lo.x, hi.x)
				// the rows kept for it hold only its own straight last run (tracks and jogs are above
				// them), but two edges into one port share that run: one label on it
				clear := true
				for x := x0; x <= x1 && clear; x++ {
					clear = !labelled(cell{x, lo.y})
				}
				if clear {
					g.label(x0, lo.y, e.e.Label, m)
					placed = true
				}
			} else {
				c := toReal(cell{e.lowCol, arrow - 1})
				switch {
				case room(c.x+1, c.y, w+1):
					g.label(c.x+2, c.y, e.e.Label, m)
					placed = true
				case room(c.x-1-w, c.y, w+1):
					g.label(c.x-1-w, c.y, e.e.Label, m)
					placed = true
				}
			}
		}
		if !placed {
			notes = append(notes, e)
		}
	}
	// the footnotes: a mark beside each edge's lower end, its label in the legend finish places under
	// the drawing
	for _, e := range notes {
		mk := g.footnote(e.e.Label)
		r := g.Boxes[e.to].Rect
		for _, c := range []cell{{r.X + r.W, r.Y - 1}, {r.X - 1, r.Y - 1}, {r.X + r.W, r.Y + r.H}, {r.X - 1, r.Y + r.H}, {r.X + r.W, r.Y}, {r.X - 1, r.Y}} {
			if !occupied(c) {
				g.label(c.x, c.y, mk, m)
				break
			}
		}
	}
	before := g.Boxes[0].Rect
	if _, err := g.finish(m); err != nil {
		return nil, nil, err
	}
	dx, dy := g.Boxes[0].Rect.X-before.X, g.Boxes[0].Rect.Y-before.Y
	for i := range routes {
		for j := range routes[i] {
			for s := range routes[i][j] {
				routes[i][j][s].x += dx
				routes[i][j][s].y += dy
			}
		}
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

// jogRows gives the gap's jogged lower ends their rows, and reports how many there are: two share a
// row only where their crossings (from jogCol to col) don't overlap, a cell clear (the left-edge
// algorithm, as tracks).
func jogRows(nets []*fcNet) (map[fcTerm]int, int) {
	var jogged []fcTerm
	for _, n := range nets {
		for _, b := range n.bots {
			if b.jog && !slices.Contains(jogged, b) {
				jogged = append(jogged, b)
			}
		}
	}
	span := func(t fcTerm) (int, int) { return min(t.col, t.jogCol), max(t.col, t.jogCol) }
	slices.SortStableFunc(jogged, func(a, b fcTerm) int {
		al, ah := span(a)
		bl, bh := span(b)
		return cmp.Or(cmp.Compare(al, bl), cmp.Compare(ah, bh))
	})
	row := map[fcTerm]int{}
	var ends []int // each row's rightmost column so far
	for _, t := range jogged {
		lo, hi := span(t)
		r := slices.IndexFunc(ends, func(end int) bool { return lo > end+1 })
		if r < 0 {
			r = len(ends)
			ends = append(ends, 0)
		}
		ends[r] = hi
		row[t] = r
	}
	return row, len(ends)
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

// strokeOf is how an edge's line is drawn.
func strokeOf(l pm.LineKind) Stroke {
	switch l {
	case pm.Dotted:
		return StrokeDashed
	case pm.Thick:
		return StrokeThick
	}
	return StrokeLight
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

// fcFrames is a frame for each of d's subgraphs that holds a node: its members are its own nodes and
// its nested subgraphs', so a frame holds the frames inside it; parent is the nearest enclosing
// subgraph with a frame. A subgraph's own direction is not laid (it is drawn in the diagram's).
func fcFrames(d *pm.FlowchartDiagram) []fcFrame {
	parentOf := map[string]string{}
	for _, s := range d.Subgraphs {
		parentOf[s.ID] = s.Parent
	}
	// within reports whether subgraph sg is id or nested in it, walking at most every subgraph once
	// (a model built by hand might cycle)
	within := func(sg, id string) bool {
		for range len(d.Subgraphs) + 1 {
			if sg == "" {
				return false
			}
			if sg == id {
				return true
			}
			sg = parentOf[sg]
		}
		return false
	}
	var frames []fcFrame
	frameOf := map[string]int{}
	for _, s := range d.Subgraphs {
		if _, dup := frameOf[s.ID]; dup {
			continue
		}
		var members []int
		for i, n := range d.Nodes {
			if within(n.Subgraph, s.ID) {
				members = append(members, i)
			}
		}
		if len(members) == 0 {
			continue
		}
		f := fcFrame{title: cmp.Or(s.Title, s.ID), members: members, parent: -1}
		for up, steps := s.Parent, 0; up != "" && steps <= len(d.Subgraphs); up, steps = parentOf[up], steps+1 {
			if p, ok := frameOf[up]; ok {
				f.parent = p
				break
			}
		}
		frameOf[s.ID] = len(frames)
		frames = append(frames, f)
	}
	return frames
}

// fcItem is one thing on a rank, left to right: a box (node i) or a frame's left or right border
// (frame i), at where the layout put its left edge.
type fcItem struct {
	kind int // fcLeft, fcBox, fcRight: the order of items the layout put at one place
	i    int
	at   float64
}

const (
	fcLeft = iota
	fcBox
	fcRight
)

// placeColumns gives every box and frame border its column: where the layout put it, on whole
// cells, moved right as far as it must be to keep clear of what is before it on every rank it is on
// (the longest path through those constraints, so no further than it must). A box keeps fcNodeSep
// from a box and a cell from its own frame's border; between a box and a border it is outside of,
// or two frames side by side, there is room for a long edge's column a cell clear of each (the
// columns beside a border take none: fcFrame.near). A frame is wide enough for its title, where its
// title runs across it (titleW, 0 for none), and on its title's rank (titleRank) its first box or
// frame keeps clear of the title's run.
func placeColumns(nodes []fcNode, ranks [][]int, frames []fcFrame, res layout.Result, titleW, titleRank []int) {
	pos := func(it fcItem) *int {
		switch it.kind {
		case fcLeft:
			return &frames[it.i].l
		case fcRight:
			return &frames[it.i].r
		}
		return &nodes[it.i].x
	}
	for i := range nodes {
		nodes[i].x = int(math.Round(float64(res.Nodes[i].X) - float64(nodes[i].w)/2))
	}
	for f := range frames {
		g := res.Groups[f]
		frames[f].l, frames[f].r = int(math.Round(float64(g.X))), int(math.Round(float64(g.X+g.W)))-1
	}
	type sep struct {
		a, b fcItem
		d    int
	}
	// each node's innermost frame (-1: none), and each frame's children: a frame comes after the one
	// it is in, so a node's last frame in order is its innermost
	inner := make([]int, len(nodes))
	for i := range inner {
		inner[i] = -1
	}
	for f, fr := range frames {
		for _, i := range fr.members {
			inner[i] = f
		}
	}
	// a rank's order is a tree: the boxes and frames at one level side by side, each frame's own
	// between its borders, so frames nest or stand apart by construction. Siblings go by where the
	// layout put their left edge: a box's own, a frame's box's, the same on every rank, so the
	// constraints the order makes never go round in a cycle.
	key := func(it fcItem) float64 {
		if it.kind == fcBox {
			return float64(res.Nodes[it.i].X) - float64(nodes[it.i].w)/2
		}
		return float64(res.Groups[it.i].X)
	}
	var seps []sep
	for r, ids := range ranks {
		units := map[int][]fcItem{} // a level's boxes and frames (key: the frame it is in, -1 none)
		for _, i := range ids {
			units[inner[i]] = append(units[inner[i]], fcItem{fcBox, i, 0})
		}
		for f, fr := range frames {
			if fr.lo <= r && r <= fr.hi {
				units[fr.parent] = append(units[fr.parent], fcItem{fcLeft, f, 0})
			}
		}
		var items []fcItem
		var emit func(level int)
		emit = func(level int) {
			us := units[level]
			slices.SortStableFunc(us, func(a, b fcItem) int {
				return cmp.Or(cmp.Compare(key(a), key(b)), cmp.Compare(a.kind, b.kind), cmp.Compare(a.i, b.i))
			})
			for _, u := range us {
				if u.kind == fcBox {
					items = append(items, u)
					continue
				}
				items = append(items, u)
				emit(u.i)
				items = append(items, fcItem{fcRight, u.i, 0})
			}
		}
		emit(-1)
		for k := 1; k < len(items); k++ {
			a, b := items[k-1], items[k]
			w := 1
			if a.kind == fcBox {
				w = nodes[a.i].w
			}
			d := map[[2]int]int{
				{fcBox, fcBox}: w + fcNodeSep, {fcBox, fcRight}: w + 1, {fcBox, fcLeft}: w + 3,
				{fcLeft, fcBox}: 2, {fcLeft, fcLeft}: 2, {fcLeft, fcRight}: 2,
				{fcRight, fcRight}: 2, {fcRight, fcBox}: 4, {fcRight, fcLeft}: 4,
			}[[2]int{a.kind, b.kind}]
			// on the rank under its title, the first thing in a frame keeps clear of the title's run, so
			// no line meeting that rank crosses it (╭─ title ─, then the first port)
			if a.kind == fcLeft && b.kind != fcRight && titleW[a.i] > 0 && r == titleRank[a.i] {
				d = max(d, titleW[a.i]+4)
			}
			seps = append(seps, sep{a, b, d})
		}
	}
	for f := range frames {
		if titleW[f] > 0 { // ╭─ title ─╮: its corners, a rule and a space either side
			seps = append(seps, sep{fcItem{fcLeft, f, 0}, fcItem{fcRight, f, 0}, titleW[f] + 5})
		}
	}
	// the longest path: each pass moves an item only right, so it ends once no constraint moves one,
	// at most once per item (the layout's order is the same on every rank, so there is no cycle)
	for range len(nodes) + 2*len(frames) + 1 {
		moved := false
		for _, s := range seps {
			if pa, pb := pos(s.a), pos(s.b); *pb < *pa+s.d {
				*pb, moved = *pa+s.d, true
			}
		}
		if !moved {
			break
		}
	}
}
