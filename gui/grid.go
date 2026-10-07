package gui

import "github.com/yongjohnlee80/golib/tui"

// blank is an empty cell in the default colours, what tui's buffer starts from.
var blank = tui.Cell{Content: " ", Width: 1}

// grid is the backend's own copy of the screen: what the App has flushed, cell by cell, and which
// rows changed since they were last drawn. Owned by tui's loop goroutine.
type grid struct {
	w, h  int
	cells []tui.Cell
	dirty []bool // per row
}

// resize sets the grid to w×h, blank and every row dirty. The App never diffs across a size
// change (tui/app.go renderFrame), so its next Flush repaints every cell anyway.
func (g *grid) resize(w, h int) {
	if w == g.w && h == g.h {
		return
	}
	g.w, g.h = w, h
	g.cells = make([]tui.Cell, w*h)
	for i := range g.cells {
		g.cells[i] = blank
	}
	g.dirty = make([]bool, h)
	g.touchAll()
}

// apply writes a frame's diff and marks its rows dirty. A cell outside the grid is dropped: it
// belongs to a size the window has already left.
//
// A width-2 update covers two columns: tui's diff emits the head and never its continuation
// (tui/buffer.go diff, rule 2), so the continuation is written here, as tui's buffer writes it,
// with the head's attributes. Replacing half of an old pair needs nothing more: the App's buffer
// dissolves the pair (clearOverlap) and its diff carries the freed half as a cell of its own.
func (g *grid) apply(diff []tui.CellUpdate) {
	for _, u := range diff {
		if u.X < 0 || u.Y < 0 || u.X >= g.w || u.Y >= g.h {
			continue
		}
		i := u.Y*g.w + u.X
		g.cells[i] = u.Cell
		if u.Cell.Width == 2 && u.X+1 < g.w {
			g.cells[i+1] = tui.Cell{Content: "", Width: 0, Attrs: u.Cell.Attrs}
		}
		g.dirty[u.Y] = true
	}
}

// at is the cell at (x, y); a blank outside the grid.
func (g *grid) at(x, y int) tui.Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return blank
	}
	return g.cells[y*g.w+x]
}

func (g *grid) touchAll() {
	for i := range g.dirty {
		g.dirty[i] = true
	}
}
