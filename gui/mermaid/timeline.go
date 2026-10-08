package mermaid

import (
	"context"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Timeline] = layTimeline }

// timelineCol is one period's column: its box, and its events' boxes under it.
type timelineCol struct {
	period laidNode
	events []laidNode
}

// layTimeline lays a timeline out in columns: its periods left to right, each a box on the
// timeline's axis, each period's events stacked under it, joined to it by a line; a titled section
// is a band behind its periods' columns, its title in the band's head; the title over all.
func layTimeline(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	td := d.(*pm.TimelineDiagram)
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.9
	bold := f
	bold.Bold = true
	l := &Laid{th: th}
	wrap := em * 11 // a column's text wraps at this width
	padX, padY := em*0.7, em*0.45

	// measure every label first: the columns are one width, the widest label's
	var cols []timelineCol
	var colSection []int // each column's section
	colW := em * 7
	for si, s := range td.Sections {
		for _, p := range s.Periods {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			c := timelineCol{period: laidNode{form: formRound, fill: th.NodeFill, stroke: th.NodeStroke, width: 1.2,
				label: newLabel(m, p.Label, bold, wrap, th.Text)}}
			colW = max(colW, c.period.label.box.W+2*padX)
			for _, e := range p.Events {
				ev := laidNode{form: formRound, fill: mix(th.NodeFill, th.Background, 0.55), stroke: mix(th.NodeStroke, th.Background, 0.45),
					width: 1, label: newLabel(m, e.Text, small, wrap, th.Text)}
				colW = max(colW, ev.label.box.W+2*padX)
				c.events = append(c.events, ev)
			}
			cols = append(cols, c)
			colSection = append(colSection, si)
		}
	}
	gap := em * 1.2
	margin := em

	y := margin
	var title label
	if td.Title != "" {
		title = newLabel(m, td.Title, gui.Font{Family: f.Family, Size: em * 1.15, Bold: true}, 0, th.Text)
		y += title.box.H + em
	}
	// the sections' heads, when any section has a title
	var headH float32
	sectionTitles := map[int]label{}
	for si, s := range td.Sections {
		if s.Title != "" {
			lb := newLabel(m, s.Title, bold, 0, th.Text)
			sectionTitles[si] = lb
			headH = max(headH, lb.box.H+2*padY)
		}
	}
	bandTop := y
	if headH > 0 {
		y += headH + em*0.6
	}
	// the periods' row: one height, the tallest label's
	var periodH float32
	for _, c := range cols {
		periodH = max(periodH, c.period.label.box.H+2*padY)
	}
	periodY := y
	axisY := periodY + periodH + em*1.1
	eventTop := axisY + em*1.1
	bottom := periodY + periodH
	if len(cols) > 0 {
		bottom = axisY
	}
	x := func(i int) float32 { return margin + float32(i)*(colW+gap) }
	for i := range cols {
		c := &cols[i]
		c.period.box = gui.Rect{X: x(i), Y: periodY, W: colW, H: periodH}
		c.period.label = c.period.label.centredIn(c.period.box)
		ey := eventTop
		for j := range c.events {
			ev := &c.events[j]
			h := ev.label.box.H + 2*padY
			ev.box = gui.Rect{X: x(i) + em*0.4, Y: ey, W: colW - em*0.8, H: h}
			ev.label = ev.label.centredIn(ev.box)
			ey += h + em*0.6
		}
		if len(c.events) > 0 {
			bottom = max(bottom, ey-em*0.6)
			// the period's line down through its events, under their boxes
			cx := x(i) + colW/2
			l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: cx, Y: periodY + periodH}, {X: cx, Y: c.events[len(c.events)-1].box.Y}},
				line: pm.Dotted, color: mix(th.Line, th.Background, 0.3)})
		}
	}
	right := margin
	if len(cols) > 0 {
		right = x(len(cols)-1) + colW
		// the axis, across every column, under the boxes
		l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: margin - em*0.5, Y: axisY}, {X: right + em*0.8, Y: axisY}},
			line: pm.Thick, head: endArrow, color: th.Line})
		right += em * 0.8
	}

	// a band behind each titled section's columns, its title centred in its head
	bandBottom := bottom + em*0.6
	for si := range td.Sections {
		lb, ok := sectionTitles[si]
		if !ok {
			continue
		}
		first, last := -1, -1
		for i, s := range colSection {
			if s == si {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		var bx, bw float32
		if first < 0 { // a section with no period: a head of its own width, at the end
			bx, bw = right+gap/2, max(lb.box.W+2*padX, colW)
			right = bx + bw
		} else {
			bx, bw = x(first)-gap/2, x(last)+colW+gap/2-(x(first)-gap/2)
		}
		fill := th.ClusterFill
		if si%2 == 1 {
			fill = mix(th.ClusterFill, th.NodeFill, 0.5)
		}
		box := gui.Rect{X: bx, Y: bandTop, W: bw, H: bandBottom - bandTop}
		head := gui.Rect{X: bx, Y: bandTop, W: bw, H: headH}
		l.group = append(l.group, laidGroup{box: box, fill: fill, stroke: th.ClusterStroke, title: lb.centredIn(head)})
		right = max(right, bx+bw)
	}

	l.nodes = make([]laidNode, 0, len(cols))
	for _, c := range cols {
		l.nodes = append(l.nodes, c.period)
		l.nodes = append(l.nodes, c.events...)
	}
	w := max(right+margin, title.box.W+2*margin, em*4)
	if td.Title != "" {
		l.texts = append(l.texts, title.at((w-title.box.W)/2, margin))
	}
	h := bottom + margin
	if len(l.group) > 0 {
		h = bandBottom + margin
	}
	l.size = gui.Size{W: w, H: max(h, em*2)}
	return l, nil
}
