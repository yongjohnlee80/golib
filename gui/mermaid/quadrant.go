package mermaid

import (
	"context"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Quadrant] = layQuadrant }

// layQuadrant lays a quadrant chart out: a square of four filled quadrants, each its label centred
// on its top, the points dots at their places with their labels to their right, the y-axis's
// labels left of the square beside the half they name, the x-axis's under it, the title over all.
// The quadrants alternate two tints of the theme's node fill toward its background, so a quadrant
// reads apart from its neighbours in every theme. A point's own style (its radius, its colours) is
// read by the parser and not drawn: every point is a dot in the theme's stroke.
func layQuadrant(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	qd := d.(*pm.QuadrantDiagram)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.9
	l := &Laid{th: th}
	margin := em

	// the room each part takes: the y-axis's labels at the left, a quadrant's label across its half
	yLow, yHigh := newLabel(m, qd.YAxis.Low, small, wrapAt, th.Text), newLabel(m, qd.YAxis.High, small, wrapAt, th.Text)
	axisW := max(yLow.box.W, yHigh.box.W)
	if axisW > 0 {
		axisW += em
	}
	var quads [4]label
	half := em * 12
	for i, s := range qd.Quadrants {
		quads[i] = newLabel(m, s, f, wrapAt, th.Text)
		half = max(half, quads[i].box.W+2*em)
	}
	halfH := half * 0.75

	title, room, _ := titleLabel(m, qd.Title, th)
	x0, y0 := margin+axisW, margin+room
	xm, ym := x0+half, y0+halfH

	// the quadrants, in Mermaid's numbering: 1 top right, 2 top left, 3 bottom left, 4 bottom right
	tints := [4]float32{0.25, 0.55, 0.25, 0.55}
	boxes := [4]gui.Rect{{X: xm, Y: y0, W: half, H: halfH}, {X: x0, Y: y0, W: half, H: halfH},
		{X: x0, Y: ym, W: half, H: halfH}, {X: xm, Y: ym, W: half, H: halfH}}
	for i, b := range boxes {
		l.nodes = append(l.nodes, laidNode{form: formRect, box: b, fill: gui.Mix(th.NodeFill, th.Background, tints[i]),
			stroke: th.NodeStroke, width: 1})
		if qd.Quadrants[i] != "" {
			l.texts = append(l.texts, quads[i].at(b.X+(b.W-quads[i].box.W)/2, b.Y+em*0.5))
		}
	}

	// the points: a dot at its place, x from the square's left and y from its bottom, its label
	// to its right
	dot := em * 0.6
	for _, p := range qd.Points {
		cx, cy := x0+float32(p.X)*2*half, y0+2*halfH-float32(p.Y)*2*halfH
		l.nodes = append(l.nodes, laidNode{form: formStart, fill: th.NodeStroke, stroke: th.NodeStroke, width: 1,
			box: gui.Rect{X: cx - dot/2, Y: cy - dot/2, W: dot, H: dot}})
		lb := newLabel(m, p.Label, small, wrapAt, th.Text)
		l.texts = append(l.texts, lb.at(cx+dot, cy-lb.box.H/2))
	}

	// the axes' labels: the y-axis's right-aligned to the square, each centred on its half; the
	// x-axis's under the square, each centred on its half, or one alone centred on the whole
	if qd.YAxis.High != "" {
		l.texts = append(l.texts, yHigh.at(x0-em/2-yHigh.box.W, y0+(halfH-yHigh.box.H)/2))
	}
	if qd.YAxis.Low != "" {
		l.texts = append(l.texts, yLow.at(x0-em/2-yLow.box.W, ym+(halfH-yLow.box.H)/2))
	}
	xLow, xHigh := newLabel(m, qd.XAxis.Low, small, wrapAt, th.Text), newLabel(m, qd.XAxis.High, small, wrapAt, th.Text)
	under := y0 + 2*halfH + em*0.4
	switch {
	case qd.XAxis.High != "":
		l.texts = append(l.texts, xLow.at(x0+(half-xLow.box.W)/2, under), xHigh.at(xm+(half-xHigh.box.W)/2, under))
	case qd.XAxis.Low != "":
		l.texts = append(l.texts, xLow.at(x0+half-xLow.box.W/2, under))
	}

	// the extent: the square, a point's label running past its right edge, the axis labels under it
	w := x0 + 2*half + margin
	for _, t := range l.texts {
		w = max(w, t.box.X+t.box.W+margin)
	}
	h := under + max(xLow.box.H, xHigh.box.H) + margin
	w = max(w, title.box.W+2*margin)
	if qd.Title != "" {
		l.texts = append(l.texts, title.at((w-title.box.W)/2, margin))
	}
	l.size = gui.Size{W: w, H: h}
	return l, nil
}
