package mermaid

import (
	"context"
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.State] = layState }

// stateLayer lays a state diagram out scope by scope: a composite's body is laid out first, on its
// own, and the composite is then one box of that size in its parent's layout. So a transition may
// reach a composite, and none crosses one (the parser refuses those).
type stateLayer struct {
	ctx      context.Context
	d        *pm.StateDiagram
	th       Theme
	m        gui.Measurer
	f, small gui.Font
	states   map[string][]int // a scope's states, by the composite's id ("" at top level)
	trans    map[string][]int // a scope's transitions
	notes    map[string][]int // a scope's notes
	index    map[string]int   // a state's index by id
}

// stateScope is a scope laid out with its top left at the origin.
type stateScope struct {
	size   gui.Size
	nodes  []laidNode
	edges  []laidEdge
	groups []laidGroup
}

func layState(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	sd := d.(*pm.StateDiagram)
	small := th.Font
	small.Size *= 0.9
	sl := &stateLayer{ctx: ctx, d: sd, th: th, m: m, f: th.Font, small: small,
		states: map[string][]int{}, trans: map[string][]int{}, notes: map[string][]int{}, index: map[string]int{}}
	for i, s := range sd.States {
		sl.index[s.ID] = i
		sl.states[s.Parent] = append(sl.states[s.Parent], i)
	}
	for i, t := range sd.Transitions {
		scope := sd.States[sl.index[t.From]].Parent
		sl.trans[scope] = append(sl.trans[scope], i)
	}
	for i, n := range sd.Notes {
		scope := sd.States[sl.index[n.State]].Parent
		sl.notes[scope] = append(sl.notes[scope], i)
	}
	s, err := sl.scope("", sd.Dir)
	if err != nil {
		return nil, err
	}
	pad := th.Font.Size * 0.5
	l := &Laid{th: th, size: gui.Size{W: s.size.W + 2*pad, H: s.size.H + 2*pad}}
	s.shift(pad, pad)
	l.nodes, l.edges, l.group = s.nodes, s.edges, s.groups
	return l, nil
}

// stateItem is one node of a scope's layout before it is placed: a state's box, a composite's body,
// or a note.
type stateItem struct {
	node  laidNode
	inner *stateScope // a composite's body, laid out
	title label       // a composite's title
	band  float32     // a composite's title band, above its body
}

// scope lays out the states, transitions and notes of the composite id ("" at top level), in dir.
func (sl *stateLayer) scope(id string, dir pm.Dir) (stateScope, error) {
	if err := sl.ctx.Err(); err != nil {
		return stateScope{}, err
	}
	em := sl.f.Size
	in := layout.Input{Dir: dirOf(dir), NodeSep: em * 2.2, RankSep: em * 2.4}
	var items []stateItem
	at := map[string]int{} // a state's node in this layout
	for _, i := range sl.states[id] {
		st := sl.d.States[i]
		it, err := sl.item(st, dir)
		if err != nil {
			return stateScope{}, err
		}
		at[st.ID] = len(items)
		items = append(items, it)
		in.Nodes = append(in.Nodes, layout.Size{W: it.node.box.W, H: it.node.box.H})
	}
	var edges []laidEdge
	for _, i := range sl.trans[id] {
		t := sl.d.Transitions[i]
		le := layout.Edge{From: at[t.From], To: at[t.To]}
		e := laidEdge{line: pm.Solid, head: endArrow, color: sl.th.Line}
		if t.Label != "" {
			lb := newLabel(sl.m, t.Label, sl.small, wrapAt, sl.th.Text)
			pad := sl.small.Size * 0.3
			le.Label = layout.Size{W: lb.box.W + 2*pad, H: lb.box.H + 2*pad}
			e.label = &lb
		}
		in.Edges = append(in.Edges, le)
		edges = append(edges, e)
	}
	// a note is a node of its own, joined to its state by a dotted line
	for _, i := range sl.notes[id] {
		n := sl.d.Notes[i]
		lb := newLabel(sl.m, n.Text, sl.small, wrapAt, sl.th.Text)
		box := gui.Rect{W: lb.box.W + em*1.4, H: lb.box.H + em*0.9}
		items = append(items, stateItem{node: laidNode{form: formNote, box: box, fill: sl.th.ClusterFill,
			stroke: sl.th.ClusterStroke, width: 1, label: lb}})
		in.Nodes = append(in.Nodes, layout.Size{W: box.W, H: box.H})
		from, to := at[n.State], len(items)-1
		if n.Side == pm.LeftOf {
			from, to = to, from
		}
		in.Edges = append(in.Edges, layout.Edge{From: from, To: to})
		edges = append(edges, laidEdge{line: pm.Dotted, color: sl.th.ClusterStroke})
	}
	if len(items) == 0 {
		return stateScope{}, nil
	}
	res, err := layout.Layered(sl.ctx, in, layout.Limits{})
	if err != nil {
		if errors.Is(err, layout.ErrTooLarge) {
			return stateScope{}, fmt.Errorf("%w: %v", pm.ErrTooLarge, err)
		}
		return stateScope{}, err
	}
	out := stateScope{size: gui.Size{W: res.Size.W, H: res.Size.H}}
	for i, it := range items {
		sz, c := in.Nodes[i], res.Nodes[i]
		x, y := c.X-sz.W/2, c.Y-sz.H/2
		if it.inner == nil {
			out.nodes = append(out.nodes, shiftNode(it.node, x, y))
			continue
		}
		box := gui.Rect{X: x, Y: y, W: sz.W, H: sz.H}
		pad := em * 0.6
		out.groups = append(out.groups, laidGroup{box: box, title: it.title.at(x+pad, y+pad*0.6)})
		inner := *it.inner
		inner.shift(x+(sz.W-inner.size.W)/2, y+it.band)
		out.nodes = append(out.nodes, inner.nodes...)
		out.edges = append(out.edges, inner.edges...)
		out.groups = append(out.groups, inner.groups...)
	}
	for i, e := range edges {
		for _, p := range res.Edges[i] {
			e.pts = append(e.pts, gui.Pt(p.X, p.Y))
		}
		if e.label != nil {
			c := res.Labels[i]
			lb := e.label.at(c.X-e.label.box.W/2, c.Y-e.label.box.H/2)
			e.label = &lb
		}
		out.edges = append(out.edges, e)
	}
	return out, nil
}

// item measures a state: its box at the origin, by kind; a composite's body laid out.
func (sl *stateLayer) item(st pm.StateNode, dir pm.Dir) (stateItem, error) {
	em := sl.f.Size
	th := sl.th
	switch st.Kind {
	case pm.StateStart:
		return stateItem{node: laidNode{form: formStart, box: gui.Rect{W: em * 0.9, H: em * 0.9}, stroke: th.Text, width: 1.2}}, nil
	case pm.StateEnd:
		return stateItem{node: laidNode{form: formEnd, box: gui.Rect{W: em * 1.1, H: em * 1.1}, stroke: th.Text, width: 1.2}}, nil
	case pm.StateFork, pm.StateJoin:
		w, h := em*4.5, em*0.5
		if dir == pm.LR || dir == pm.RL {
			w, h = h, w
		}
		return stateItem{node: laidNode{form: formBar, box: gui.Rect{W: w, H: h}, stroke: th.Text}}, nil
	case pm.StateChoice:
		fill, stroke, _, sw := styled(st.Style, th)
		return stateItem{node: laidNode{form: formDiamond, box: gui.Rect{W: em * 1.6, H: em * 1.6}, fill: fill, stroke: stroke, width: sw}}, nil
	}
	fill, stroke, text, sw := styled(st.Style, th)
	name := newLabel(sl.m, st.Label, sl.f, wrapAt, text)
	if st.Composite {
		cdir := st.Dir
		if cdir == 0 {
			cdir = dir
		}
		inner, err := sl.scope(st.ID, cdir)
		if err != nil {
			return stateItem{}, err
		}
		title := newLabel(sl.m, st.Label, sl.small, wrapAt, th.Text)
		title.left = true
		pad := em * 0.6
		band := title.box.H + pad*1.4
		w := max(inner.size.W, title.box.W+pad) + 2*pad
		h := band + inner.size.H + pad
		return stateItem{node: laidNode{box: gui.Rect{W: w, H: h}}, inner: &inner, title: title, band: band}, nil
	}
	padX, padY := em*1.2, em*0.6
	n := laidNode{form: formRound, fill: fill, stroke: stroke, width: sw}
	if len(st.Descriptions) == 0 {
		w, h := max(name.box.W+2*padX, em*4), name.box.H+2*padY
		n.box = gui.Rect{W: w, H: h}
		n.label = name.centredIn(n.box)
		return stateItem{node: n}, nil
	}
	// the name above a divider, the descriptions under it, from the left
	var descs []label
	dw, dh := float32(0), float32(0)
	for _, s := range st.Descriptions {
		lb := newLabel(sl.m, s, sl.small, wrapAt, text)
		lb.left = true
		descs = append(descs, lb)
		dw = max(dw, lb.box.W)
		dh += lb.box.H
	}
	w := max(name.box.W, dw) + 2*padX
	divider := name.box.H + 2*padY*0.7
	h := divider + padY*0.7 + dh + padY*0.7
	n.box = gui.Rect{W: w, H: h}
	n.label = name.at((w-name.box.W)/2, padY*0.7)
	y := divider + padY*0.7
	for i := range descs {
		descs[i] = descs[i].at(padX, y)
		y += descs[i].box.H
	}
	n.parts = []part{{y: divider, labels: descs}}
	return stateItem{node: n}, nil
}

// shiftNode is n placed dx, dy further on.
func shiftNode(n laidNode, dx, dy float32) laidNode {
	n.box.X, n.box.Y = n.box.X+dx, n.box.Y+dy
	n.label = n.label.at(n.label.box.X+dx, n.label.box.Y+dy)
	if n.parts != nil {
		parts := make([]part, len(n.parts))
		for i, p := range n.parts {
			parts[i] = shiftPart(p, dx, dy)
		}
		n.parts = parts
	}
	return n
}

// shiftPart is p placed dx, dy further on; a part without a divider keeps none.
func shiftPart(p part, dx, dy float32) part {
	if p.y != 0 {
		p.y += dy
	}
	labels := make([]label, len(p.labels))
	for i, lb := range p.labels {
		labels[i] = lb.at(lb.box.X+dx, lb.box.Y+dy)
	}
	p.labels = labels
	return p
}

// shift places everything in s dx, dy further on.
func (s *stateScope) shift(dx, dy float32) {
	for i := range s.nodes {
		s.nodes[i] = shiftNode(s.nodes[i], dx, dy)
	}
	for i := range s.edges {
		e := &s.edges[i]
		pts := make([]gui.Point, len(e.pts))
		for j, p := range e.pts {
			pts[j] = gui.Pt(p.X+dx, p.Y+dy)
		}
		e.pts = pts
		for _, lb := range []**label{&e.label, &e.headLabel, &e.tailLabel} {
			if *lb != nil {
				moved := (*lb).at((*lb).box.X+dx, (*lb).box.Y+dy)
				*lb = &moved
			}
		}
	}
	for i := range s.groups {
		g := &s.groups[i]
		g.box.X, g.box.Y = g.box.X+dx, g.box.Y+dy
		g.title = g.title.at(g.title.box.X+dx, g.title.box.Y+dy)
		if g.parts != nil {
			parts := make([]part, len(g.parts))
			for j, p := range g.parts {
				parts[j] = shiftPart(p, dx, dy)
			}
			g.parts = parts
		}
	}
}
