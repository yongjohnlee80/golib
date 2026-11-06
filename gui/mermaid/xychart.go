package mermaid

import (
	"context"
	"fmt"
	"image/color"
	"strconv"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
)

func init() { layers[pm.XYChart] = layXYChart }

// layXYChart lays an XY chart out: a value axis down the left with five ticks and their values, the
// x axis along the bottom with a group a point and its label centred under it, each bar a filled box
// rising from the value axis's floor, side by side in a group, each line a polyline through a dot a
// point, the axes' titles and the chart's title, and a key when there is more than one series or a
// named one. Each series takes one step of a ramp from the theme's node fill toward its text, so
// neighbours differ and every series stays of the theme. A horizontal chart is not drawn yet:
// ErrUnsupported, for the caller's fallback.
func layXYChart(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	xd := d.(*pm.XYChartDiagram)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if xd.Horizontal {
		return nil, fmt.Errorf("%w: a horizontal xychart", pm.ErrUnsupported)
	}
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.9
	l := &Laid{th: th}
	margin := em

	title, room, _ := titleLabel(m, xd.Title, th)
	top := margin + room
	if xd.YAxis.Title != "" {
		yt := newLabel(m, xd.YAxis.Title, small, wrapAt, th.Text)
		l.texts = append(l.texts, yt.at(margin, top))
		top += yt.box.H + em*0.5
	}
	lo, hi := xd.ValueRange()

	// the value axis's ticks and their labels, right-aligned before it
	var ticks [5]label
	tickW := float32(0)
	for k := range ticks {
		ticks[k] = newLabel(m, tuimermaid.XYNumber(lo+float64(k)*(hi-lo)/4), small, 0, th.Text)
		tickW = max(tickW, ticks[k].box.W)
	}
	ax := margin + tickW + em*0.5
	plotH := em * 12
	floor := top + plotH

	// the x axis: a group a point, as wide as its label and its bars need
	n := xd.Points()
	cats := tuimermaid.XYCategories(xd, n)
	catLabels := make([]label, n)
	bars := 0
	for _, s := range xd.Series {
		if s.Kind == pm.XYBar {
			bars++
		}
	}
	barW := em * 0.9
	groupW := max(em*3, float32(bars)*barW+em*0.6)
	for i, c := range cats {
		catLabels[i] = newLabel(m, c, small, 0, th.Text)
		groupW = max(groupW, catLabels[i].box.W+em*0.6)
	}
	x0 := ax + em*0.5
	right := x0 + float32(n)*groupW
	centre := func(i int) float32 { return x0 + (float32(i)+0.5)*groupW }
	axis := func(pts ...gui.Point) {
		l.edges = append(l.edges, laidEdge{pts: pts, line: pm.Solid, color: th.Line})
	}
	axis(gui.Pt(ax, top), gui.Pt(ax, floor), gui.Pt(right, floor))
	for k, t := range ticks {
		y := floor - float32(k)*plotH/4
		axis(gui.Pt(ax-em*0.3, y), gui.Pt(ax, y))
		l.texts = append(l.texts, t.at(ax-em*0.5-t.box.W, y-t.box.H/2))
	}
	under := floor + em*0.4
	labelH := float32(0)
	for i, c := range catLabels {
		axis(gui.Pt(centre(i), floor), gui.Pt(centre(i), floor+em*0.3))
		l.texts = append(l.texts, c.at(centre(i)-c.box.W/2, under))
		labelH = max(labelH, c.box.H)
	}
	bottom := under + labelH
	if xd.XAxis.Title != "" {
		xt := newLabel(m, xd.XAxis.Title, small, wrapAt, th.Text)
		l.texts = append(l.texts, xt.at(x0+(right-x0-xt.box.W)/2, bottom+em*0.4))
		bottom += em*0.4 + xt.box.H
	}

	// the series: bars side by side about a group's centre, then lines over them
	frac := func(v float64) float32 { return float32(min(max((v-lo)/(hi-lo), 0), 1)) }
	colours := make([]color.NRGBA, len(xd.Series))
	b := 0
	for si, s := range xd.Series {
		colours[si] = sliceColor(th, si, len(xd.Series))
		if s.Kind != pm.XYBar {
			continue
		}
		for i, v := range s.Values {
			x := centre(i) - float32(bars)*barW/2 + float32(b)*barW
			h := frac(v) * plotH
			l.nodes = append(l.nodes, laidNode{form: formRect, fill: colours[si], stroke: th.NodeStroke, width: 1,
				box: gui.Rect{X: x, Y: floor - h, W: barW, H: h}})
		}
		b++
	}
	dot := em * 0.5
	for si, s := range xd.Series {
		if s.Kind != pm.XYLine {
			continue
		}
		pts := make([]gui.Point, len(s.Values))
		for i, v := range s.Values {
			pts[i] = gui.Pt(centre(i), floor-frac(v)*plotH)
			l.nodes = append(l.nodes, laidNode{form: formStart, fill: colours[si], stroke: colours[si], width: 1,
				box: gui.Rect{X: pts[i].X - dot/2, Y: pts[i].Y - dot/2, W: dot, H: dot}})
		}
		if len(pts) > 1 {
			l.edges = append(l.edges, laidEdge{pts: pts, line: pm.Thick, color: colours[si], over: true})
		}
	}

	// the key: a chip and a name a series, at the right of the plot
	w := right + margin
	named := false
	for _, s := range xd.Series {
		named = named || s.Name != ""
	}
	if len(xd.Series) > 1 || named {
		kx, ky := right+em, top
		chip := em * 0.7
		for si, s := range xd.Series {
			name := s.Name
			if name == "" {
				name = s.Kind.String() + " " + strconv.Itoa(si+1)
			}
			lb := newLabel(m, name, small, 0, th.Text)
			form := formRect
			if s.Kind == pm.XYLine {
				form = formStart
			}
			l.nodes = append(l.nodes, laidNode{form: form, fill: colours[si], stroke: th.NodeStroke, width: 1,
				box: gui.Rect{X: kx, Y: ky + (lb.box.H-chip)/2, W: chip, H: chip}})
			l.texts = append(l.texts, lb.at(kx+chip+em*0.5, ky))
			w = max(w, kx+chip+em*0.5+lb.box.W+margin)
			ky += lb.box.H * 1.4
		}
	}
	w = max(w, title.box.W+2*margin)
	if xd.Title != "" {
		l.texts = append(l.texts, title.at((w-title.box.W)/2, margin))
	}
	l.size = gui.Size{W: w, H: max(bottom+margin, em*2)}
	return l, nil
}
