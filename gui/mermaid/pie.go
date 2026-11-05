package mermaid

import (
	"context"
	"image/color"
	"math"
	"strconv"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Pie] = layPie }

// layPie lays a pie out: a circle of wedges at the left, its legend at the right — a colour chip,
// the label, the value when the header's showData asks, the share as a percentage — the title over
// all, and, in each wedge wide enough to hold it, its share's percentage. A slice's colour is one
// step of a ramp from the theme's node fill toward its text, so neighbours differ and every slice
// stays of the theme. A pie whose values sum to nothing has no angles: its legend alone, every
// share zero.
func layPie(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	pd := d.(*pm.PieDiagram)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.9
	l := &Laid{th: th}
	margin := em
	r := em * 6

	total := pd.Total()

	// the legend's labels: each slice's, and its share (with its value when the header asks)
	type row struct {
		chip  laidNode
		label label
		data  label
		share float32
	}
	var rows []row
	var labelW, dataW float32
	for _, s := range pd.Slices {
		share := float32(s.Share(total))
		data := pctLabel(share)
		if pd.ShowData {
			data = strconv.FormatFloat(s.Value, 'f', -1, 64) + " (" + pctLabel(share) + ")"
		}
		lb, dt := newLabel(m, s.Label, small, 0, th.Text), newLabel(m, data, small, 0, th.Text)
		labelW, dataW = max(labelW, lb.box.W), max(dataW, dt.box.W)
		rows = append(rows, row{label: lb, data: dt, share: share})
	}

	gap := em * 0.8
	chip := em * 0.7
	legendW := max(chip+gap+labelW+gap+dataW, em*4)
	rowH := f.Size * 1.6

	y := margin
	title, room, _ := titleLabel(m, pd.Title, th)
	y += room
	cx := margin + r
	cy := y + r
	legendX := cx + r + em

	// the wedges, and the percentage in each one wide enough to hold it
	if total > 0 {
		a := float32(0)
		for i := range pd.Slices {
			share := rows[i].share
			a1 := a + share*2*math.Pi
			if i == len(pd.Slices)-1 {
				a1 = 2 * math.Pi // the last wedge absorbs the steps' rounding
			}
			fill := sliceColor(th, i, len(pd.Slices))
			l.sectors = append(l.sectors, laidSector{cx: cx, cy: cy, r: r, a0: a, a1: a1, fill: fill, stroke: th.NodeStroke})
			if lb := newLabel(m, pctLabel(share), small, 0, readable(th, fill)); share > 0.04 &&
				lb.box.W <= 2*r*0.66*float32(math.Sin(min(float64(share*math.Pi), math.Pi/2))) {
				mid := (a + a1) / 2
				l.texts = append(l.texts, lb.at(cx+0.66*r*float32(math.Sin(float64(mid)))-lb.box.W/2,
					cy-0.66*r*float32(math.Cos(float64(mid)))-lb.box.H/2))
			}
			a = a1
		}
	}

	// the legend, centred on the pie: a chip and the label at the left, the share at the right
	ry := cy - float32(len(rows))*rowH/2
	for i := range rows {
		rowY := ry + float32(i)*rowH
		rows[i].chip = laidNode{form: formRect, fill: sliceColor(th, i, len(pd.Slices)), stroke: th.NodeStroke, width: 1,
			box: gui.Rect{X: legendX, Y: rowY + (rowH-chip)/2, W: chip, H: chip}}
		rows[i].label = rows[i].label.at(legendX+chip+gap, rowY+(rowH-rows[i].label.box.H)/2)
		rows[i].data = rows[i].data.at(legendX+legendW-rows[i].data.box.W, rowY+(rowH-rows[i].data.box.H)/2)
		l.nodes = append(l.nodes, rows[i].chip)
		l.texts = append(l.texts, rows[i].label, rows[i].data)
	}

	w := margin + 2*r + em + legendW + margin
	w = max(w, title.box.W+2*margin)
	if pd.Title != "" {
		l.texts = append(l.texts, title.at((w-title.box.W)/2, margin))
	}
	h := max(cy+r, cy+float32(len(rows))*rowH/2) + margin
	l.size = gui.Size{W: w, H: max(h, em*2)}
	return l, nil
}

// sector paints one wedge: filled, then its outline stroked, so the slice boundaries read even
// where a ramp wraps two neighbours into one colour.
func (l *Laid) sector(c gui.Canvas, s laidSector) {
	c.FillPath(sectorPath(s), gui.Solid(s.fill))
	c.StrokePath(sectorPath(s), 1, gui.Solid(s.stroke))
}

// sectorPath is the wedge s as a path: two radii and its arc in short straight steps.
func sectorPath(s laidSector) *gui.Path {
	p := new(gui.Path).MoveTo(gui.Pt(s.cx, s.cy))
	const steps = 64 // along a whole circle
	n := max(int((s.a1-s.a0)/(2*math.Pi)*steps), 1)
	for i := 0; i <= n; i++ {
		a := s.a0 + (s.a1-s.a0)*float32(i)/float32(n)
		p.LineTo(gui.Pt(s.cx+s.r*float32(math.Sin(float64(a))), s.cy-s.r*float32(math.Cos(float64(a)))))
	}
	return p.Close()
}

// sliceColor is slice i of n's colour: the node fill itself for the first, then steps toward the
// theme's text, so no two neighbours share one.
func sliceColor(th Theme, i, n int) color.NRGBA {
	if i == 0 || n <= 1 {
		return th.NodeFill
	}
	return gui.Mix(th.NodeFill, th.Text, float32(i)/float32(max(n-1, 1)))
}

// readable is the colour of a percentage on fill: the theme's text on a light one, its background
// on a dark one.
func readable(th Theme, fill color.NRGBA) color.NRGBA {
	if 0.299*float32(fill.R)+0.587*float32(fill.G)+0.114*float32(fill.B) > 255.0/2 {
		return th.Text
	}
	return th.Background
}

// pctLabel is share as Mermaid's percentages: the model's one rounding (parse's Percent), shown
// whole when it is, one decimal when it is not.
func pctLabel(share float32) string {
	return strconv.FormatFloat(pm.Percent(float64(share)), 'f', -1, 64) + "%"
}
