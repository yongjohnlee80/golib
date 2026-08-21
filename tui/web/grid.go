package web

import "github.com/yongjohnlee80/golib/tui"

// grid is a server-side cell grid: the backend's model of what the client
// should be showing (ADR-0009 §2.1).
//
// Two of these exist per session — the CURRENT server truth and the last
// baseline the client ACKNOWLEDGED — because the frame that goes on the wire is
// the difference between them. Deriving the frame from two grids rather than
// merging diff lists is what makes the aggregate cumulative by construction
// instead of by careful bookkeeping (§2.4).
type grid struct {
	w, h  int
	cells []tui.Cell
}

// blank is the cell an unwritten position holds. A space rather than the zero
// Cell, so a diff against a freshly resized grid describes something the
// renderer can actually emit.
var blank = tui.Cell{Content: " ", Width: 1}

func newGrid(w, h int) *grid {
	g := &grid{}
	g.resize(w, h)
	return g
}

// resize reshapes the grid, preserving the overlapping region.
//
// Content is preserved because a resize is not a repaint: the App will redraw
// what it wants, and discarding the overlap would make a resize emit a
// full-screen diff of blanks first.
func (g *grid) resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	if w == g.w && h == g.h {
		return
	}
	next := make([]tui.Cell, w*h)
	for i := range next {
		next[i] = blank
	}
	copyW, copyH := min(w, g.w), min(h, g.h)
	for y := range copyH {
		copy(next[y*w:y*w+copyW], g.cells[y*g.w:y*g.w+copyW])
	}
	g.w, g.h, g.cells = w, h, next
}

// set writes one cell, ignoring out-of-range positions.
//
// Out of range is IGNORED rather than an error: a frame in flight can arrive
// after a shrink, and dropping the cell is correct — the client no longer has
// that position — while failing the Flush would take down the App loop for a
// race it cannot avoid.
func (g *grid) set(x, y int, c tui.Cell) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return
	}
	g.cells[y*g.w+x] = c
}

func (g *grid) at(x, y int) tui.Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return blank
	}
	return g.cells[y*g.w+x]
}

// clone returns an independent copy, for snapshotting a baseline.
func (g *grid) clone() *grid {
	out := &grid{w: g.w, h: g.h, cells: make([]tui.Cell, len(g.cells))}
	copy(out.cells, g.cells)
	return out
}

// sameShape reports whether two grids can be diffed against each other.
func (g *grid) sameShape(o *grid) bool { return g.w == o.w && g.h == o.h }

// diff returns the cells where g differs from base, row-major.
//
// Row-major order matches ADR-0003 §2.2's contract for a frame diff, and it
// means the renderer can walk updates in the order it paints.
func (g *grid) diff(base *grid) []tui.CellUpdate {
	if !g.sameShape(base) {
		// Shapes disagree, so every position is new. Callers should have asked
		// for a full snapshot instead; returning one is the safe answer rather
		// than a diff against coordinates that do not correspond.
		return g.snapshot()
	}
	var out []tui.CellUpdate
	for y := range g.h {
		row := y * g.w
		for x := range g.w {
			if g.cells[row+x] != base.cells[row+x] {
				out = append(out, tui.CellUpdate{X: x, Y: y, Cell: g.cells[row+x]})
			}
		}
	}
	return out
}

// snapshot returns every cell as an update: the full resync payload.
func (g *grid) snapshot() []tui.CellUpdate {
	out := make([]tui.CellUpdate, 0, len(g.cells))
	for y := range g.h {
		row := y * g.w
		for x := range g.w {
			out = append(out, tui.CellUpdate{X: x, Y: y, Cell: g.cells[row+x]})
		}
	}
	return out
}

// apply writes a set of updates into the grid.
func (g *grid) apply(updates []tui.CellUpdate) {
	for _, u := range updates {
		g.set(u.X, u.Y, u.Cell)
	}
}

// equal reports whether two grids hold identical content. Used by tests to
// assert the client converged on the server's state.
func (g *grid) equal(o *grid) bool {
	if !g.sameShape(o) {
		return false
	}
	for i := range g.cells {
		if g.cells[i] != o.cells[i] {
			return false
		}
	}
	return true
}
