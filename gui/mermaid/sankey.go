package mermaid

import (
	"context"
	"slices"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
)

func init() { layers[pm.Sankey] = laySankey }

// laySankey lays a sankey out: its nodes in columns by depth (a source at the left, each other node
// one column past the deepest flowing into it), each a bar as tall as what passes through it, all on
// one scale, stacked down its column with a gap between; each flow a band as thick as its amount,
// leaving its source in the order of its targets' heights and reaching its target in the order of its
// sources', so bands cross as little as they can; each node's name and amount beside it (left of it in
// the last column). A node takes a step of a ramp from the theme's node fill toward its text, and its
// flows are its colour halfway to the page.
func laySankey(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	sd := d.(*pm.SankeyDiagram)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.9
	l := &Laid{th: th}
	margin := em
	if len(sd.Nodes) == 0 {
		l.size = gui.Size{W: em * 2, H: em * 2}
		return l, nil
	}

	depth := sd.Depths()
	cols := slices.Max(depth) + 1
	column := make([][]int, cols)
	for i, dpt := range depth {
		column[dpt] = append(column[dpt], i)
	}
	labels := make([]label, len(sd.Nodes))
	labelW := float32(0)
	for i, name := range sd.Nodes {
		labels[i] = newLabel(m, name+"  "+tuimermaid.XYNumber(sd.Through(i)), small, wrapAt, th.Text)
		labelW = max(labelW, labels[i].box.W)
	}

	// the scale: the tallest column, its nodes and its gaps, fills the plot's height
	nodeW, gap := em*0.9, em*0.8
	plotH := em * 20
	scale := float32(1e30)
	for _, c := range column {
		var sum float64
		for _, i := range c {
			sum += sd.Through(i)
		}
		if sum > 0 {
			scale = min(scale, (plotH-gap*float32(len(c)-1))/float32(sum))
		}
	}
	colGap := max(labelW+em*2, em*8)
	top := margin
	x := func(c int) float32 { return margin + float32(c)*(nodeW+colGap) }
	boxes := make([]gui.Rect, len(sd.Nodes))
	for c, nodes := range column {
		y := top
		for _, i := range nodes {
			h := max(float32(sd.Through(i))*scale, 2)
			boxes[i] = gui.Rect{X: x(c), Y: y, W: nodeW, H: h}
			y += h + gap
		}
	}
	// the bands: each flow leaves its source below the flows to higher targets, and reaches its
	// target below the flows from higher sources
	out, in := make([]float32, len(sd.Nodes)), make([]float32, len(sd.Nodes))
	order := make([]int, len(sd.Links))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmpY(boxes[sd.Links[a].Target].Y, boxes[sd.Links[b].Target].Y)
	})
	type ends struct{ y0, y1 float32 }
	start := make([]ends, len(sd.Links))
	for _, k := range order {
		lk := sd.Links[k]
		start[k].y0 = boxes[lk.Source].Y + out[lk.Source]
		out[lk.Source] += float32(lk.Value) * scale
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmpY(boxes[sd.Links[a].Source].Y, boxes[sd.Links[b].Source].Y)
	})
	for _, k := range order {
		lk := sd.Links[k]
		start[k].y1 = boxes[lk.Target].Y + in[lk.Target]
		in[lk.Target] += float32(lk.Value) * scale
	}
	for k, lk := range sd.Links {
		h := float32(lk.Value) * scale
		s, t := boxes[lk.Source], boxes[lk.Target]
		l.bands = append(l.bands, laidBand{x0: s.X + s.W, y0a: start[k].y0, y0b: start[k].y0 + h,
			x1: t.X, y1a: start[k].y1, y1b: start[k].y1 + h,
			fill: gui.Mix(sliceColor(th, lk.Source, len(sd.Nodes)), th.Background, 0.5)})
	}

	// the nodes and their labels
	w := float32(0)
	for i, b := range boxes {
		l.nodes = append(l.nodes, laidNode{id: sd.Nodes[i], form: formRect, box: b,
			fill: sliceColor(th, i, len(sd.Nodes)), stroke: th.NodeStroke, width: 1})
		lb := labels[i]
		ly := b.Y + (b.H-lb.box.H)/2
		if depth[i] == cols-1 && cols > 1 {
			l.texts = append(l.texts, lb.at(b.X-em*0.5-lb.box.W, ly))
		} else {
			l.texts = append(l.texts, lb.at(b.X+b.W+em*0.5, ly))
			w = max(w, b.X+b.W+em*0.5+lb.box.W)
		}
		w = max(w, b.X+b.W)
	}
	h := float32(0)
	for _, b := range boxes {
		h = max(h, b.Y+b.H)
	}
	l.size = gui.Size{W: w + margin, H: h + margin}
	return l, nil
}

// cmpY orders two heights.
func cmpY(a, b float32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
