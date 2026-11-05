package mermaid

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// pieChars are a slice's fill characters, one density each, cycled: neighbours differ, and so do
// the first and the last when the count wraps.
var pieChars = []string{"█", "▓", "▒", "░"}

// pieDiameter is the pie's circle, in cells.
const pieDiameter = 16

// LayPie lays a pie out in cells: its circle rastered in filled characters, one density a slice,
// its legend beside it — the label, its share, and its value when the header's showData asks — the
// title over all. A pie whose values sum to nothing has no angles: its legend alone, every share
// zero.
func LayPie(ctx context.Context, d *pm.PieDiagram, m Measurer) (*Grid, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := newGrid()
	total := 0.0
	for _, s := range d.Slices {
		total += s.Value
	}

	y := 0
	if d.Title != "" {
		g.label(0, 0, d.Title, m)
		y = 2
	}

	// the legend: each slice's own glyph before its label, as Mermaid's legend puts each slice's
	// colour square — the key from the pie to the row — then the share (and the value)
	labelW := 0
	for _, s := range d.Slices {
		labelW = max(labelW, m.StringWidth(s.Label))
	}
	for i, s := range d.Slices {
		share := 0.0
		if total > 0 {
			share = s.Value / total
		}
		row := sliceChar(i, len(d.Slices)) + " " + s.Label + strings.Repeat(" ", max(labelW-m.StringWidth(s.Label), 0)) + "  " + pctCell(share)
		if d.ShowData {
			row += "  " + strconv.FormatFloat(s.Value, 'f', -1, 64)
		}
		g.label(pieDiameter+2, y+i, row, m)
	}

	// the circle: one fill character a cell, the slice whose angle holds the cell's own angle
	if total > 0 {
		cum := make([]float64, len(d.Slices))
		var a float64
		for i, s := range d.Slices {
			a += s.Value / total
			cum[i] = a
		}
		cum[len(cum)-1] = 1 // the last slice absorbs the steps' rounding
		for cy := range pieDiameter {
			for cx := range pieDiameter {
				dx := (float64(cx)+0.5)/pieDiameter*2 - 1
				dy := (float64(cy)+0.5)/pieDiameter*2 - 1
				if dx*dx+dy*dy > 1 {
					continue
				}
				ang := math.Atan2(dx, -dy) // 0 at 12 o'clock, clockwise
				if ang < 0 {
					ang += 2 * math.Pi
				}
				g.fills[cell{x: cx, y: y + cy}] = sliceChar(angleSlice(cum, ang), len(d.Slices))
			}
		}
	}

	g.settle()
	if g.tooLarge() {
		return nil, layout.ErrTooLarge
	}
	return g, nil
}

// angleSlice is the slice whose share holds ang, a fraction of the circle from 12 o'clock.
func angleSlice(cum []float64, ang float64) int {
	f := ang / (2 * math.Pi)
	for i, c := range cum {
		if f < c {
			return i
		}
	}
	return len(cum) - 1
}

// sliceChar is slice i of n's fill character: the densities cycle, and the last never takes the
// first's when the count wraps.
func sliceChar(i, n int) string {
	c := pieChars[i%len(pieChars)]
	if n > 1 && i == n-1 && i%len(pieChars) == 0 {
		c = pieChars[2]
	}
	return c
}

// pctCell is share as Mermaid's percentages: whole when it is, one decimal when it is not.
func pctCell(share float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(share*100, 'f', 1, 64), ".0") + "%"
}
