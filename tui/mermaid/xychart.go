package mermaid

import (
	"context"
	"math"
	"strconv"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { register(pm.XYChart, layAs(LayXYChart), xychartDrawn) }

// AN XY CHART IN CELLS: a value axis down the left with five ticks, the x axis along the bottom
// with a group of columns a point, its label centred under it; each bar a column rising from the
// value axis's floor in eighths of a cell (▁▂▃▄▅▆▇█), each line a mark a point joined by dots.
// Series are told apart by their glyphs, not colours, so a chart reads in every theme: bars █ ▓ ▒ ░
// in turn, lines ● ◆ ▲ ■. With more than one series, or a named one, a key under the drawing names
// each.
//
//	          Sales Revenue
//	11000 ┤                  ●
//	 9250 ┤             ▆█  ▆█●█▇
//	 7500 ┤      ▂█●█▂  ██  █████▆●▅
//	 5750 ┤  ●▃█ ████●  ██  ████████
//	 4000 └───┬───┬───┬───┬───┬───┬──
//	         jan feb mar apr may jun
//
// A horizontal chart (bars running across) is not drawn here yet: ErrUnsupported, for the host's
// notice.

// xyPlotH is the plot's height in rows.
const xyPlotH = 12

// xyTicks is how many value ticks the value axis carries, its floor and its top among them.
const xyTicks = 5

var (
	xyBarGlyphs  = []string{"█", "▓", "▒", "░"}
	xyLineGlyphs = []string{"●", "◆", "▲", "■"}
	xyEighths    = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇"}
)

// xyLineDot joins a line's marks.
const xyLineDot = "·"

// xychartDrawn is ErrUnsupported for a horizontal chart, which cells don't draw yet.
func xychartDrawn(d pm.Diagram) error {
	if x, ok := d.(*pm.XYChartDiagram); ok && x.Horizontal {
		return ErrUnsupported
	}
	return nil
}

// LayXYChart lays an XY chart out in cells.
func LayXYChart(ctx context.Context, d *pm.XYChartDiagram, m Measurer) (*Grid, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.Horizontal {
		return nil, ErrUnsupported
	}
	g := newGrid()
	top := g.title(d.Title, m)
	if d.YAxis.Title != "" {
		g.label(0, top, d.YAxis.Title, m)
		top += 2
	}
	lo, hi := d.ValueRange()

	// the value axis: its ticks' labels right-aligned before it, ┤ at each, └ at its floor
	labels := make([]string, xyTicks)
	for k := range xyTicks {
		labels[k] = XYNumber(lo + float64(k)*(hi-lo)/(xyTicks-1))
	}
	yw := widest(m, labels...)
	ax := yw + 1
	floor := top + xyPlotH // the axis's floor row, under the plot's last
	g.run(cell{ax, top}, cell{ax, floor})
	for k, s := range labels {
		row := floor - 1 - int(math.Round(float64(k)*float64(xyPlotH-1)/(xyTicks-1)))
		_, w, _ := capLabel(s, RoleLabel, m)
		g.label(ax-1-w, row, s, m)
		g.lines[cell{ax, row}] |= west // ┤: the tick points at its label
	}

	// the x axis: a group of columns a point, wide enough for its label and its bars
	n := d.Points()
	cats := XYCategories(d, n)
	bars, lines := 0, 0
	for _, s := range d.Series {
		if s.Kind == pm.XYBar {
			bars++
		} else {
			lines++
		}
	}
	gw := max(widest(m, cats...)+1, 2*bars+1, 3)
	x0 := ax + 2
	right := x0 + n*gw
	g.run(cell{ax, floor}, cell{right, floor})
	centre := func(i int) int { return x0 + i*gw + gw/2 }
	for i, c := range cats {
		g.lines[cell{centre(i), floor}] |= south
		_, w, _ := capLabel(c, RoleLabel, m)
		g.label(centre(i)-w/2, floor+1, c, m)
	}
	if d.XAxis.Title != "" {
		_, w, _ := capLabel(d.XAxis.Title, RoleLabel, m)
		g.label(x0+(n*gw-w)/2, floor+2, d.XAxis.Title, m)
	}

	// the bars: series j of the bars a column of group i, side by side about its centre
	frac := func(v float64) float64 { return min(max((v-lo)/(hi-lo), 0), 1) }
	b := 0
	for _, s := range d.Series {
		if s.Kind != pm.XYBar {
			continue
		}
		glyph := xyBarGlyphs[b%len(xyBarGlyphs)]
		for i, v := range s.Values {
			x := centre(i) - (2*bars-1)/2 + 2*b
			// the first series' bars rise in eighths (▁ … ▇ atop █); the others' glyphs have no
			// eighths, so they rise in whole cells, rounded
			eighths := int(math.Round(frac(v) * xyPlotH * 8))
			if glyph != "█" {
				eighths = int(math.Round(frac(v)*xyPlotH)) * 8
			}
			for r := 0; r < eighths/8; r++ {
				g.fills[cell{x, floor - 1 - r}] = glyph
			}
			if rest := eighths % 8; rest > 0 {
				g.fills[cell{x, floor - 1 - eighths/8}] = xyEighths[rest-1]
			}
		}
		b++
	}

	// the lines: a mark a point at its group's centre, over any bar, joined to the next by dots on
	// the cells nothing else is drawn in
	l := 0
	for _, s := range d.Series {
		if s.Kind != pm.XYLine {
			continue
		}
		glyph := xyLineGlyphs[l%len(xyLineGlyphs)]
		pts := make([]cell, len(s.Values))
		for i, v := range s.Values {
			pts[i] = cell{centre(i), floor - 1 - int(math.Round(frac(v)*(xyPlotH-1)))}
		}
		for i := 1; i < len(pts); i++ {
			for _, c := range xySteps(pts[i-1], pts[i]) {
				if _, filled := g.fills[c]; !filled && g.marks[c] == "" {
					g.marks[c] = xyLineDot
				}
			}
		}
		for _, c := range pts {
			g.fills[c] = glyph
		}
		l++
	}

	// the key: each series' glyph and its name, when there is more than one or any is named
	named := false
	for _, s := range d.Series {
		named = named || s.Name != ""
	}
	if len(d.Series) > 1 || named {
		b, l = 0, 0
		for i, s := range d.Series {
			name := s.Name
			if name == "" {
				name = s.Kind.String() + " " + strconv.Itoa(i+1)
			}
			var glyph string
			if s.Kind == pm.XYBar {
				glyph, b = xyBarGlyphs[b%len(xyBarGlyphs)], b+1
			} else {
				glyph, l = xyLineGlyphs[l%len(xyLineGlyphs)], l+1
			}
			g.legend(legendRow{swatch: glyph, label: name})
		}
	}
	return g.finish(m)
}

// XYCategories are the x axis's n labels, as both renderers write them (gui/mermaid's too): its categories, else its range's values at each point,
// else the points numbered from 1.
func XYCategories(d *pm.XYChartDiagram, n int) []string {
	if len(d.XAxis.Categories) > 0 {
		return d.XAxis.Categories
	}
	out := make([]string, n)
	for i := range out {
		switch {
		case d.XAxis.Ranged && n > 1:
			out[i] = XYNumber(d.XAxis.Min + float64(i)*(d.XAxis.Max-d.XAxis.Min)/float64(n-1))
		case d.XAxis.Ranged:
			out[i] = XYNumber(d.XAxis.Min)
		default:
			out[i] = strconv.Itoa(i + 1)
		}
	}
	return out
}

// XYNumber is a value as an axis writes it, in both renderers: a whole number as one, else to three decimals at most,
// its trailing zeros dropped.
func XYNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}

// xySteps are the cells strictly between a and b on the straight line from one to the other.
func xySteps(a, b cell) []cell {
	dx, dy := b.x-a.x, b.y-a.y
	n := max(dx, -dx, dy, -dy)
	var out []cell
	for k := 1; k < n; k++ {
		out = append(out, cell{a.x + int(math.Round(float64(dx*k)/float64(n))), a.y + int(math.Round(float64(dy*k)/float64(n)))})
	}
	return out
}
