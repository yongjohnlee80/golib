package mermaid

import (
	"context"
	"math"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { register(pm.Quadrant, layAs(LayQuadrant), nil) }

// A QUADRANT CHART IN CELLS: a box split in four by a cross at its middle, each quadrant's label
// centred on its first row no point is on, and each point a ● at its place with its label beside
// it. The y-axis's labels sit left of the box beside the half they name, the x-axis's under it,
// each centred on its half.
//
//	                ┌─────────────────────┬─────────────────────┐
//	                │   Need to promote   │  We should expand   │
//	High Engagement │       ● F           │                     │
//	                │                     │                     │
//	                ├─────────────────────┼─────────────────────┤
//	                │     Re-evaluate     │   May be improved   │
//	 Low Engagement │       ● B           │   ● D               │
//	                └─────────────────────┴─────────────────────┘
//	                       Low Reach             High Reach
//
// A point's label beside it is cut to fit, whole under the pointer (HitAt), and a footnote when no
// side of it has room. A point's own style (its radius, its colours) is not drawn: every point is
// a ● in the diagram's colours, so it reads in every theme.

// quadrantHalfW and quadrantHalfH are a quadrant's least room inside the box, in cells: a half's
// width (more for a wider label) and its height.
const (
	quadrantHalfW = 21
	quadrantHalfH = 5
)

// quadrantPoint is a point's mark.
const quadrantPoint = "●"

// LayQuadrant lays a quadrant chart out in cells.
func LayQuadrant(ctx context.Context, d *pm.QuadrantDiagram, m Measurer) (*Grid, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := newGrid()
	top := g.title(d.Title, m)

	// the box's left edge: past the y-axis's labels and a gap
	left := 0
	if yw := widest(m, d.YAxis.Low, d.YAxis.High); yw > 0 {
		left = yw + 1
	}
	half := max(quadrantHalfW, widest(m, d.Quadrants[:]...)+2, widest(m, d.XAxis.Low, d.XAxis.High)+2)
	x0, x1 := left, left+2*half+2
	xm := left + half + 1
	y0, y1 := top, top+2*quadrantHalfH+2
	ym := top + quadrantHalfH + 1

	// the box and the cross
	g.run(cell{x0, y0}, cell{x1, y0})
	g.run(cell{x1, y0}, cell{x1, y1})
	g.run(cell{x1, y1}, cell{x0, y1})
	g.run(cell{x0, y1}, cell{x0, y0})
	g.run(cell{xm, y0}, cell{xm, y1})
	g.run(cell{x0, ym}, cell{x1, ym})

	inBox := func(c cell) bool { return c.x > x0 && c.x < x1 && c.y > y0 && c.y < y1 }
	free := func(x, y, w int) bool {
		for i := range w {
			c := cell{x + i, y}
			_, line := g.lines[c]
			if !inBox(c) || line || g.marks[c] != "" {
				return false
			}
			for _, t := range g.texts {
				if c.y == t.y && c.x >= t.x && c.x < t.x+t.w {
					return false
				}
			}
		}
		return true
	}

	// the points' marks first: a label is drawn over a mark, so what is placed after keeps clear
	at := make([]cell, len(d.Points))
	for i, p := range d.Points {
		at[i] = quadrantCell(p, x0, x1, y0, y1)
		g.marks[at[i]] = quadrantPoint
	}

	// each quadrant's label, centred on its first row no point is on: quadrant-1 top right, 2 top
	// left, 3 bottom left, 4 bottom right (none clear: it is a footnote)
	for i, q := range quadrantRegions(x0, xm, x1, y0, ym) {
		s := d.Quadrants[i]
		if s == "" {
			continue
		}
		_, w, _ := capLabel(s, RoleLabel, m)
		placed := false
		for y := q.y; y < q.y+quadrantHalfH && !placed; y++ {
			if x := q.x + (q.w-w)/2; free(x, y, w) {
				g.label(x, y, s, m)
				placed = true
			}
		}
		if !placed {
			mk := g.footnote(s)
			g.label(q.x, q.y, mk, m)
		}
	}

	// each point's label beside it: on its row, right then left, a cell clear of it; else the row
	// above or below, starting beside it (a point on the cross has only those); else a footnote,
	// its mark in the first of those places it fits
	beside := func(c cell, w int) (cell, bool) {
		for _, at := range []struct{ from, y, room, x int }{ // the cells that must be free, and where the label starts
			{c.x + 1, c.y, w + 1, c.x + 2}, {c.x - 1 - w, c.y, w + 1, c.x - 1 - w},
			{c.x + 1, c.y - 1, w, c.x + 1}, {c.x + 1, c.y + 1, w, c.x + 1}, {c.x - w, c.y - 1, w, c.x - w}, {c.x - w, c.y + 1, w, c.x - w},
		} {
			if free(at.from, at.y, at.room) {
				return cell{at.x, at.y}, true
			}
		}
		return cell{}, false
	}
	var notes []int
	for i, p := range d.Points {
		_, w, _ := capLabel(p.Label, RoleLabel, m)
		if c, ok := beside(at[i], w); ok {
			g.label(c.x, c.y, p.Label, m)
		} else {
			notes = append(notes, i)
		}
	}
	for _, i := range notes {
		mk := g.footnote(d.Points[i].Label)
		if c, ok := beside(at[i], m.StringWidth(mk)); ok {
			g.label(c.x, c.y, mk, m)
		}
	}

	// the axes' labels: the y-axis's beside its halves, right-aligned to the box; the x-axis's
	// under the box, each centred on its half (one alone centred on the whole)
	for _, a := range []struct {
		s   string
		row int
	}{{d.YAxis.High, y0 + (ym-y0)/2}, {d.YAxis.Low, ym + (y1-ym)/2}} {
		if a.s != "" {
			_, w, _ := capLabel(a.s, RoleLabel, m)
			g.label(x0-1-w, a.row, a.s, m)
		}
	}
	centre := func(s string, from, to int) {
		_, w, _ := capLabel(s, RoleLabel, m)
		g.label(from+(to-from+1-w)/2, y1+1, s, m)
	}
	switch {
	case d.XAxis.High != "":
		centre(d.XAxis.Low, x0+1, xm-1)
		centre(d.XAxis.High, xm+1, x1-1)
	case d.XAxis.Low != "":
		centre(d.XAxis.Low, x0+1, x1-1)
	}
	return g.finish(m)
}

// quadrantRegion is a quadrant's top row inside the box: its first column, its width, its row.
type quadrantRegion struct{ x, w, y int }

// quadrantRegions are the four quadrants' top rows, in Mermaid's numbering: quadrant-1 top right,
// quadrant-2 top left, quadrant-3 bottom left, quadrant-4 bottom right.
func quadrantRegions(x0, xm, x1, y0, ym int) [4]quadrantRegion {
	left := quadrantRegion{x: x0 + 1, w: xm - x0 - 1}
	right := quadrantRegion{x: xm + 1, w: x1 - xm - 1}
	tl, tr, bl, br := left, right, left, right
	tl.y, tr.y, bl.y, br.y = y0+1, y0+1, ym+1, ym+1
	return [4]quadrantRegion{tr, tl, bl, br}
}

// quadrantCell is the cell a point lies in: x from the box's first inner column (0) to its last
// (1), y from its last inner row (0) to its first (1). A point at 0.5 lies on the cross.
func quadrantCell(p pm.QuadrantPoint, x0, x1, y0, y1 int) cell {
	w, h := x1-x0-2, y1-y0-2 // the inner span, less one: the last cell's offset
	return cell{x0 + 1 + int(math.Round(p.X*float64(w))), y1 - 1 - int(math.Round(p.Y*float64(h)))}
}
