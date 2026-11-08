package mermaid

import (
	"context"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A FRONT MATTER TITLE — a diagram whose kind draws no title of its own (a flowchart, a class or
// state diagram, an ER, a mind map, a requirement, a sankey, a gitGraph, a block diagram) takes its
// front matter's title (parse/mermaid's TitleOf) above it: a bold line in the theme's text colour,
// centred, the diagram moved down under it. A kind that draws its own (a pie, a gantt, a
// quadrant, an XY chart, a journey, a timeline, a sequence) draws it as it always has.

// ownTitle are the kinds whose layer draws the diagram's title itself.
var ownTitle = map[pm.Kind]bool{pm.Pie: true, pm.Gantt: true, pm.Quadrant: true, pm.XYChart: true,
	pm.Journey: true, pm.Timeline: true, pm.Sequence: true}

// withTitle lays d out as lay does, and puts its front matter's title above it where its kind
// draws none.
func withTitle(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer, lay layer) (*Laid, error) {
	l, err := lay(ctx, d, width, th, m)
	if err != nil || ownTitle[d.Kind()] {
		return l, err
	}
	title, room, ok := titleLabel(m, pm.TitleOf(d), th)
	if !ok {
		return l, nil
	}
	em := th.Font.Size
	w := max(l.size.W, title.box.W+2*em)
	l.moveBy((w-l.size.W)/2, room)
	l.texts = append(l.texts, title.at((w-title.box.W)/2, em/2))
	l.size = gui.Size{W: w, H: l.size.H + room}
	return l, nil
}

// moveBy moves everything laid by dx, dy: every layer, a pie's wedges and a sankey's flows too.
func (l *Laid) moveBy(dx, dy float32) {
	for i := range l.nodes {
		l.nodes[i] = l.nodes[i].moved(dx, dy)
	}
	for i := range l.edges {
		l.edges[i] = l.edges[i].moved(dx, dy)
	}
	for i := range l.group {
		l.group[i] = l.group[i].moved(dx, dy)
	}
	for i := range l.texts {
		l.texts[i] = l.texts[i].moved(dx, dy)
	}
	for i := range l.sectors {
		l.sectors[i].cx += dx
		l.sectors[i].cy += dy
	}
	for i := range l.bands {
		b := &l.bands[i]
		b.x0, b.x1 = b.x0+dx, b.x1+dx
		b.y0a, b.y0b, b.y1a, b.y1b = b.y0a+dy, b.y0b+dy, b.y1a+dy, b.y1b+dy
	}
}
