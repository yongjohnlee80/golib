package mermaid

import (
	"context"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Block] = layBlock }

// layBlock lays a block diagram out on the grid it gives, as the cells do (tui/mermaid's LayBlock):
// a grid's columns as wide as their widest one-column block, a block spanning columns as wide as
// they are with the gaps between, every block in a column the column's width, a group a frame round
// its own grid, a space an empty cell. Each link is a straight line from its source's outline toward
// its target's centre, ending at the target's outline, as Mermaid draws them; its label on its middle.
func layBlock(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	bd := d.(*pm.BlockDiagram)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	em := th.Font.Size
	b := &blockWindow{th: th, m: m, em: em, l: &Laid{th: th}, boxes: map[string]gui.Rect{}, group: map[string]int{}}
	root := b.measure(&bd.Root, -1)
	margin := em
	root.place(margin, margin)
	small := th.Font
	small.Size *= 0.9
	for _, e := range bd.Edges {
		from, to := b.boxes[e.From], b.boxes[e.To]
		a, z := rectCentre(to), rectCentre(from)
		de := laidEdge{pts: []gui.Point{toward(from, a), toward(to, z)}, line: e.Line, head: arrowEnd[e.Head],
			tail: arrowEnd[e.Tail], color: th.Line}
		if e.Label != "" {
			lb := newLabel(m, e.Label, small, wrapAt, th.Text)
			mid := gui.Pt((de.pts[0].X+de.pts[1].X)/2, (de.pts[0].Y+de.pts[1].Y)/2)
			lb = lb.at(mid.X-lb.box.W/2, mid.Y-lb.box.H/2)
			de.label = &lb
		}
		if g, ok := b.group[e.From]; ok && g >= 0 && g == b.group[e.To] {
			de.ground = th.ClusterFill
		}
		b.l.edges = append(b.l.edges, de)
	}
	b.l.size = gui.Size{W: root.w + 2*margin, H: root.h + 2*margin}
	return b.l, nil
}

// blockWindow lays a block diagram's groups out in the window, keeping each block's box and each
// block's innermost group (by its place in l.group; -1 the root), by id.
type blockWindow struct {
	th    Theme
	m     gui.Measurer
	em    float32
	l     *Laid
	boxes map[string]gui.Rect
	group map[string]int
}

// windowGroup is a group measured: its size, and how to lay it out at a place.
type windowGroup struct {
	w, h  float32
	place func(x, y float32)
}

// measure measures g's grid, its blocks in group in (an index into l.group, -1 the root), and
// returns its size and its placer.
func (b *blockWindow) measure(g *pm.BlockGroup, in int) windowGroup {
	em := b.em
	gap, pad := em*1.6, em
	cols := g.Columns
	if cols == 0 {
		for _, it := range g.Items {
			cols += it.Width
		}
		cols = max(cols, 1)
	}
	type at struct {
		row, col, w int
		iw, ih      float32
		lb          label
		sub         windowGroup
		gi          int
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
			a.gi = len(b.l.group)
			b.l.group = append(b.l.group, laidGroup{})
			a.sub = b.measure(it.Group, a.gi)
			a.iw, a.ih = a.sub.w+2*pad, a.sub.h+2*pad
		case it.Space:
			a.iw, a.ih = em*2, em*2
		default:
			a.lb = newLabel(b.m, it.Label, b.th.Font, wrapAt, b.th.Text)
			a.iw, a.ih = nodeSize(it.Shape, gui.Size{W: a.lb.box.W, H: a.lb.box.H}, em)
		}
		ats[i] = a
		col += w
	}
	rows := row + 1
	if len(g.Items) == 0 {
		rows = 0
	}
	colW, rowH := make([]float32, cols), make([]float32, rows)
	for _, a := range ats {
		if a.w == 1 {
			colW[a.col] = max(colW[a.col], a.iw)
		}
		rowH[a.row] = max(rowH[a.row], a.ih)
	}
	for i := range colW {
		colW[i] = max(colW[i], em*2)
	}
	span := func(a at) float32 {
		w := float32(a.w-1) * gap
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
	w := float32(cols-1) * gap
	for _, c := range colW {
		w += c
	}
	h := float32(max(rows-1, 0)) * gap
	for _, r := range rowH {
		h += r
	}
	return windowGroup{w: max(w, em), h: max(h, em), place: func(x0, y0 float32) {
		for i, it := range g.Items {
			a := ats[i]
			x := x0 + float32(a.col)*gap
			for c := range a.col {
				x += colW[c]
			}
			y := y0 + float32(a.row)*gap
			for r := range a.row {
				y += rowH[r]
			}
			switch {
			case it.Group != nil:
				box := gui.Rect{X: x, Y: y, W: span(a), H: a.ih}
				b.l.group[a.gi].box = box
				a.sub.place(x+pad, y+pad)
				if it.ID != "" {
					b.boxes[it.ID], b.group[it.ID] = box, in
				}
			case it.Space:
			default:
				box := gui.Rect{X: x, Y: y + (rowH[a.row]-a.ih)/2, W: span(a), H: a.ih}
				lb := a.lb.at(box.X+(box.W-a.lb.box.W)/2, box.Y+(box.H-a.lb.box.H)/2)
				b.l.nodes = append(b.l.nodes, laidNode{id: it.ID, form: shapeForm[it.Shape], box: box,
					fill: b.th.NodeFill, stroke: b.th.NodeStroke, width: 1.2, label: lb})
				b.boxes[it.ID], b.group[it.ID] = box, in
			}
		}
	}}
}

// rectCentre is r's middle.
func rectCentre(r gui.Rect) gui.Point { return gui.Pt(r.X+r.W/2, r.Y+r.H/2) }

// toward is where the line from r's centre to p leaves r's outline (its rect): r's centre when p is
// inside it.
func toward(r gui.Rect, p gui.Point) gui.Point {
	c := rectCentre(r)
	dx, dy := p.X-c.X, p.Y-c.Y
	t := float32(1)
	if dx != 0 {
		t = min(t, (r.W/2)/abs32(dx))
	}
	if dy != 0 {
		t = min(t, (r.H/2)/abs32(dy))
	}
	return gui.Pt(c.X+dx*t, c.Y+dy*t)
}

func abs32(v float32) float32 { return max(v, -v) }
