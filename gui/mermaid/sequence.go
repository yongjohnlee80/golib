package mermaid

import (
	"context"
	"strconv"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Sequence] = laySequence }

// seqEnd is each message end's end.
var seqEnd = [...]end{pm.SeqNone: endNone, pm.SeqArrow: endArrow, pm.SeqCross: endCross, pm.SeqAsync: endOpen}

// seqFrame is a frame being laid out: where it opens, its sections' dividers, and the
// participants its steps touch.
type seqFrame struct {
	step   pm.Step
	top    float32
	splits []float32
	labels []label
	x0, x1 float32 // its horizontal extent so far: the touched participants' and inner frames'
	used   bool
	depth  int
}

// seqLayout holds what laySequence works with: the diagram's sizes, the participants' centres,
// and the drawing so far.
type seqLayout struct {
	l       *Laid
	m       gui.Measurer
	em      float32
	f       gui.Font
	index   map[string]int
	centre  []float32
	boxes   []laidNode // the participants' top boxes
	top     []laidNode // notes and numbers: drawn over the activations
	boxH    float32
	active  map[int][]float32 // each participant's open activations: where each started
	frames  []*seqFrame       // open, outermost first
	num     int               // the next message's number; 0: numbering off
	inc     int
	actW    float32 // an activation's width
	lastMsg float32 // the last message's y: where an activate or deactivate statement acts
}

// laySequence lays a sequence diagram out: participants left to right, spaced so each message's
// and note's text fits between them, then its steps top to bottom, and the participants again
// at the bottom.
func laySequence(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	sd := d.(*pm.SequenceDiagram)
	em := th.Font.Size
	s := &seqLayout{l: &Laid{th: th}, m: m, em: em, f: th.Font, index: map[string]int{}, active: map[int][]float32{}, actW: em * 0.7}
	if err := s.participants(sd); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pad := em
	y := pad
	if sd.Title != "" {
		t := newLabel(m, sd.Title, gui.Font{Family: th.Font.Family, Size: em * 1.15, Bold: true}, 0, th.Text)
		s.l.texts = append(s.l.texts, t.at(0, y)) // centred over the diagram below, once its width is known
		y += t.box.H + em
	}
	top := y
	y += s.boxH + em*1.2
	for _, st := range sd.Steps {
		y = s.step(st, y)
	}
	// activations still open end at the last step
	for i, starts := range s.active {
		for range starts {
			s.closeActivation(i, y)
		}
	}
	y += em * 0.6
	s.place(top, y)
	return s.l, nil
}

// participants measures each participant and sets its centre: far enough from each to its left
// for the boxes, and for every message and note between them.
func (s *seqLayout) participants(sd *pm.SequenceDiagram) error {
	em, th := s.em, s.l.th
	n := len(sd.Participants)
	widths := make([]float32, n)
	for i, p := range sd.Participants {
		s.index[p.ID] = i
		lb := newLabel(s.m, p.Label, s.f, wrapAt, th.Text)
		b := laidNode{form: formRect, fill: th.NodeFill, stroke: th.NodeStroke, width: 1.2, label: lb}
		w, h := max(lb.box.W+em*2, em*6), lb.box.H+em*1.6
		if p.Actor {
			b.form = formActor
			w, h = max(lb.box.W+em, em*3), em*2.6+em*0.4+lb.box.H
		}
		b.box = gui.Rect{W: w, H: h}
		s.boxes = append(s.boxes, b)
		widths[i] = w
		s.boxH = max(s.boxH, h)
	}
	// need[i][j], i < j: how far apart the centres of i and j must be
	need := make([]map[int]float32, n)
	for i := range need {
		need[i] = map[int]float32{}
	}
	want := func(a, b int, d float32) {
		if a > b {
			a, b = b, a
		}
		need[a][b] = max(need[a][b], d)
	}
	for i := 0; i+1 < n; i++ {
		want(i, i+1, (widths[i]+widths[i+1])/2+em*2.5)
	}
	for _, st := range sd.Steps {
		switch st.Kind {
		case pm.StepMessage:
			a, b := s.index[st.From], s.index[st.To]
			tw := s.textW(st.Text)
			if a == b { // a self message's loop and text stand to its right
				if a+1 < n {
					want(a, a+1, widths[a+1]/2+tw+em*3)
				}
				continue
			}
			want(a, b, tw+em*2.5)
		case pm.StepNote:
			a, b := s.index[st.From], s.index[st.To]
			tw := s.textW(st.Text) + em*2
			switch st.Place {
			case pm.LeftOf:
				if a > 0 {
					want(a-1, a, widths[a-1]/2+tw+em)
				}
			case pm.RightOf:
				if a+1 < n {
					want(a, a+1, widths[a+1]/2+tw+em)
				}
			case pm.Over:
				if a != b {
					want(a, b, tw-em)
				}
			}
		}
	}
	s.centre = make([]float32, n)
	for j := range n {
		x := widths[j] / 2
		for i := range j {
			if d, ok := need[i][j]; ok {
				x = max(x, s.centre[i]+d)
			}
		}
		s.centre[j] = x
	}
	return nil
}

// textW is s's width as a message or note shows it.
func (s *seqLayout) textW(t string) float32 {
	if t == "" {
		return 0
	}
	return newLabel(s.m, t, s.f, wrapAt, s.l.th.Text).box.W
}

// edgeX is where a message to or from participant i, coming from the side toward x, meets it: its
// lifeline, or its innermost activation's side.
func (s *seqLayout) edgeX(i int, toward float32) float32 {
	c := s.centre[i]
	k := len(s.active[i])
	if k == 0 {
		return c
	}
	off := float32(k-1) * s.actW / 2
	if toward < c {
		return c + off - s.actW/2
	}
	return c + off + s.actW/2
}

// touch widens the open frames to hold participant i.
func (s *seqLayout) touch(i int) {
	for _, f := range s.frames {
		lo, hi := s.centre[i]-s.boxes[i].box.W/2, s.centre[i]+s.boxes[i].box.W/2
		if !f.used {
			f.x0, f.x1, f.used = lo, hi, true
		} else {
			f.x0, f.x1 = min(f.x0, lo), max(f.x1, hi)
		}
	}
}

// step lays one step out at y and is the y below it.
func (s *seqLayout) step(st pm.Step, y float32) float32 {
	em, th := s.em, s.l.th
	switch st.Kind {
	case pm.StepMessage:
		a, b := s.index[st.From], s.index[st.To]
		s.touch(a)
		s.touch(b)
		lb := newLabel(s.m, st.Text, s.f, wrapAt, th.Text)
		if st.Text == "" {
			lb.box.H = 0
		}
		y += lb.box.H + em*0.4
		e := laidEdge{line: st.Line, head: seqEnd[st.Head], tail: seqEnd[st.Tail], color: th.Line, over: true, plain: true}
		if a == b {
			x := s.edgeX(a, s.centre[a]+1)
			loop := em * 1.8
			if st.Text != "" {
				t := lb.at(x+em*0.5, y-lb.box.H-em*0.2)
				t.left = true
				e.label = &t
			}
			e.pts = []gui.Point{{X: x, Y: y}, {X: x + loop, Y: y}, {X: x + loop, Y: y + em*1.2}, {X: x, Y: y + em*1.2}}
			s.number(&e)
			s.l.edges = append(s.l.edges, e)
			s.activations(st, a, b, y)
			s.lastMsg = y + em*1.2
			return y + em*2.2
		}
		if st.Activate { // the receiver's new activation is what the message meets
			s.active[b] = append(s.active[b], y)
		}
		x0 := s.edgeX(a, s.centre[b])
		x1 := s.edgeX(b, s.centre[a])
		e.pts = []gui.Point{{X: x0, Y: y}, {X: x1, Y: y}}
		if st.Text != "" {
			t := lb.at((x0+x1)/2-lb.box.W/2, y-lb.box.H-em*0.25)
			e.label = &t
		}
		s.number(&e)
		s.l.edges = append(s.l.edges, e)
		if st.Deactivate {
			s.closeActivation(a, y)
		}
		s.lastMsg = y
		return y + em*1.2
	case pm.StepNote:
		a, b := s.index[st.From], s.index[st.To]
		s.touch(a)
		s.touch(b)
		lb := newLabel(s.m, st.Text, s.f, wrapAt, th.Text)
		w, h := lb.box.W+em*1.6, lb.box.H+em
		var x float32
		switch st.Place {
		case pm.LeftOf:
			x = s.centre[a] - em*0.6 - w
		case pm.RightOf:
			x = s.centre[a] + em*0.6
		default:
			lo, hi := min(s.centre[a], s.centre[b]), max(s.centre[a], s.centre[b])
			if a != b {
				lo, hi = lo-em*1.5, hi+em*1.5
			}
			w = max(w, hi-lo)
			x = (lo+hi)/2 - w/2
		}
		box := gui.Rect{X: x, Y: y, W: w, H: h}
		s.top = append(s.top, laidNode{form: formNote, box: box, fill: mix(th.NodeFill, th.ClusterFill, 0.5),
			stroke: th.NodeStroke, width: 1, label: lb.centredIn(box)})
		return y + h + em*0.8
	case pm.StepActivate:
		i := s.index[st.From]
		s.active[i] = append(s.active[i], s.lastMsg)
		return y
	case pm.StepDeactivate:
		s.closeActivation(s.index[st.From], s.lastMsg)
		return y
	case pm.StepNumber:
		if st.Off {
			s.num = 0
		} else {
			s.num, s.inc = max(st.Start, 1), max(st.Increment, 1)
		}
		return y
	case pm.StepBlock:
		f := &seqFrame{step: st, top: y, depth: len(s.frames)}
		s.frames = append(s.frames, f)
		return y + em*2.4
	case pm.StepBranch:
		f := s.frames[len(s.frames)-1]
		y += em * 0.3
		f.splits = append(f.splits, y)
		lb := newLabel(s.m, "["+st.Text+"]", s.f, wrapAt, th.Text)
		f.labels = append(f.labels, lb.at(0, y+em*0.25))
		return y + lb.box.H + em*0.9
	case pm.StepEnd:
		f := s.frames[len(s.frames)-1]
		s.frames = s.frames[:len(s.frames)-1]
		y += em * 0.5
		s.closeFrame(f, y)
		return y + em*0.8
	}
	return y
}

// number puts the next number, when numbering, in a dot at the message's start, the line starting
// past the dot.
func (s *seqLayout) number(e *laidEdge) {
	if s.num == 0 {
		return
	}
	th := s.l.th
	f := s.f
	f.Size *= 0.75
	lb := newLabel(s.m, strconv.Itoa(s.num), f, 0, th.Background)
	r := max(lb.box.W, lb.box.H)/2 + s.em*0.2
	at := e.pts[0]
	dot := gui.Rect{X: at.X - r, Y: at.Y - r, W: 2 * r, H: 2 * r}
	s.top = append(s.top, laidNode{form: formCircle, box: dot, fill: th.Line, stroke: th.Line, width: 1, label: lb.centredIn(dot)})
	e.pts[0] = back(e.pts[0], e.pts[1], -r)
	s.num += s.inc
}

// activations applies a self message's + and -: the activation opens or closes where it is drawn.
func (s *seqLayout) activations(st pm.Step, a, b int, y float32) {
	if st.Activate {
		s.active[b] = append(s.active[b], y)
	}
	if st.Deactivate {
		s.closeActivation(a, y)
	}
}

// closeActivation ends participant i's innermost activation at y: a narrow box on its lifeline,
// beside any it is nested in.
func (s *seqLayout) closeActivation(i int, y float32) {
	open := s.active[i]
	if len(open) == 0 {
		return
	}
	top := open[len(open)-1]
	s.active[i] = open[:len(open)-1]
	k := len(open) - 1
	th := s.l.th
	x := s.centre[i] - s.actW/2 + float32(k)*s.actW/2
	h := max(y-top, s.em*0.6)
	s.l.nodes = append(s.l.nodes, laidNode{form: formRect, box: gui.Rect{X: x, Y: top, W: s.actW, H: h},
		fill: th.ClusterFill, stroke: th.NodeStroke, width: 1})
}

// closeFrame lays a frame out, its steps done: its box round the participants they touched (all,
// when none), its title in a tab, its sections' dividers; an outer frame grows to hold it.
func (s *seqLayout) closeFrame(f *seqFrame, bottom float32) {
	th, em := s.l.th, s.em
	switch n := len(s.centre) - 1; {
	case f.used:
	case n < 0: // no participant at all: a frame of its own width
		f.x0, f.x1 = 0, em*8
	default: // touched none: across them all
		f.x0, f.x1 = s.centre[0]-s.boxes[0].box.W/2, s.centre[n]+s.boxes[n].box.W/2
	}
	inset := em * 0.5
	x0, x1 := f.x0-inset, f.x1+inset
	g := laidGroup{bare: true, stroke: th.NodeStroke, tab: true}
	if f.step.Block == pm.RectBlock {
		fill, ok := cssColor(f.step.Text)
		if !ok {
			fill = th.ClusterFill
		}
		g = laidGroup{fill: fill, stroke: fill}
	} else {
		small := s.f
		small.Size *= 0.9
		kind := newLabel(s.m, f.step.Block.String(), gui.Font{Family: small.Family, Size: small.Size, Bold: true}, 0, th.Text)
		g.title = kind.at(x0+em*0.4, f.top+em*0.25)
		g.title.left = true
		if f.step.Text != "" {
			cond := newLabel(s.m, "["+f.step.Text+"]", s.f, wrapAt, th.Text)
			tabEnd := g.title.box.X + g.title.box.W + em*1.5
			x1 = max(x1, tabEnd+cond.box.W+em)
			g.parts = append(g.parts, part{labels: []label{cond.at(tabEnd+(x1-tabEnd-cond.box.W)/2, f.top+em*0.25)}})
		}
		for i, y := range f.splits {
			lb := f.labels[i]
			g.parts = append(g.parts, part{y: y, dashed: true, labels: []label{lb.at(x0+(x1-x0-lb.box.W)/2, lb.box.Y)}})
		}
	}
	g.box = gui.Rect{X: x0, Y: f.top, W: x1 - x0, H: bottom - f.top}
	s.l.group = append(s.l.group, g)
	if n := len(s.frames); n > 0 {
		p := s.frames[n-1]
		if !p.used {
			p.x0, p.x1, p.used = x0, x1, true
		} else {
			p.x0, p.x1 = min(p.x0, x0), max(p.x1, x1)
		}
	}
}

// place sets the participants' boxes at top and bottom, the lifelines between, centres the title,
// and moves everything right so nothing starts left of the margin.
func (s *seqLayout) place(top, bottom float32) {
	th, em, l := s.l.th, s.em, s.l
	var boxes []laidNode
	for i, b := range s.boxes {
		for _, y := range []float32{top, bottom} {
			n := b
			n.box.X, n.box.Y = s.centre[i]-b.box.W/2, y+(s.boxH-b.box.H)
			if n.form == formActor {
				n.fill = th.NodeFill
				n.label = n.label.at(n.box.X+(n.box.W-n.label.box.W)/2, n.box.Y+n.box.H-n.label.box.H)
			} else {
				n.label = n.label.centredIn(n.box)
			}
			boxes = append(boxes, n)
		}
		life := laidEdge{pts: []gui.Point{{X: s.centre[i], Y: top + s.boxH}, {X: s.centre[i], Y: bottom}}, line: pm.Dotted,
			color: mix(th.Line, th.Background, 0.35)}
		l.edges = append([]laidEdge{life}, l.edges...)
	}
	l.nodes = append(append(boxes, l.nodes...), s.top...)

	// the extent: everything drawn, then a margin round it
	x0, x1 := float32(0), float32(0)
	grow := func(r gui.Rect) { x0, x1 = min(x0, r.X), max(x1, r.X+r.W) }
	for _, n := range l.nodes {
		grow(n.box)
	}
	for _, g := range l.group {
		grow(g.box)
	}
	for _, e := range l.edges {
		for _, p := range e.pts {
			grow(gui.Rect{X: p.X, W: 0})
		}
		if e.label != nil {
			grow(e.label.box)
		}
	}
	for i := range l.texts { // the title, over the middle
		t := &l.texts[i]
		t.box.X = (x0+x1)/2 - t.box.W/2
		grow(t.box)
	}
	dx := em - x0
	l.shift(dx)
	l.size = gui.Size{W: x1 - x0 + 2*em, H: bottom + s.boxH + em}
}

// shift moves everything laid right by dx.
func (l *Laid) shift(dx float32) {
	mv := func(r *gui.Rect) { r.X += dx }
	mvl := func(lb *label) {
		if lb != nil {
			mv(&lb.box)
		}
	}
	for i := range l.nodes {
		n := &l.nodes[i]
		mv(&n.box)
		mvl(&n.label)
		for j := range n.parts {
			for k := range n.parts[j].labels {
				mvl(&n.parts[j].labels[k])
			}
		}
	}
	for i := range l.edges {
		e := &l.edges[i]
		for j := range e.pts {
			e.pts[j].X += dx
		}
		mvl(e.label)
		mvl(e.headLabel)
		mvl(e.tailLabel)
	}
	for i := range l.group {
		g := &l.group[i]
		mv(&g.box)
		mvl(&g.title)
		for j := range g.parts {
			for k := range g.parts[j].labels {
				mvl(&g.parts[j].labels[k])
			}
		}
	}
	for i := range l.texts {
		mvl(&l.texts[i])
	}
}
