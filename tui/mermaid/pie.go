package mermaid

import (
	"context"
	"math"
	"strconv"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { register(pm.Pie, layAs(LayPie), nil) }

// pieChars are a slice's fill characters, one density each, cycled: neighbours differ, and so do
// the first and the last when the count wraps.
var pieChars = []string{"█", "▓", "▒", "░"}

// pieDiameter is the pie's circle, in cells.
const pieDiameter = 16

// LayPie lays a pie out in cells: its circle rastered in filled characters, one density a slice,
// its legend under the drawing — each row its slice's own glyph, its capped label, its share as a
// percentage, and its value when the header's showData asks — the title above. A pie whose
// values sum to nothing has no angles: its legend alone, every share zero.
func LayPie(ctx context.Context, d *pm.PieDiagram, m Measurer) (*Grid, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := newGrid()
	total := d.Total()
	y := 0
	if d.Title != "" {
		y = g.title(d.Title, m) + 1
	}

	// the legend: each row its slice's own glyph (the key from the pie to the row), its label, its
	// share, and its value when the header's showData asks
	for i, s := range d.Slices {
		row := legendRow{swatch: sliceChar(i, len(d.Slices)), label: s.Label, cols: []string{pctCell(s.Share(total))}}
		if d.ShowData {
			row.cols = append(row.cols, strconv.FormatFloat(s.Value, 'f', -1, 64))
		}
		g.legend(row)
	}

	// the circle: one fill character a cell, the slice whose angle holds the cell's own angle
	if total > 0 {
		cum := make([]float64, len(d.Slices))
		var a float64
		for i, s := range d.Slices {
			a += s.Share(total)
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

	return g.finish(m)
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

// pctCell is share as Mermaid's percentages: the model's one rounding (parse's Percent), shown
// whole when it is, one decimal when it is not.
func pctCell(share float64) string {
	return strconv.FormatFloat(pm.Percent(share), 'f', -1, 64) + "%"
}
