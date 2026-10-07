package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

func cell(s string) tui.Cell { return tui.Cell{Content: s, Width: 1} }

func full(w, h int) []tui.CellUpdate {
	var out []tui.CellUpdate
	for y := range h {
		for x := range w {
			out = append(out, tui.CellUpdate{X: x, Y: y, Cell: cell("x")})
		}
	}
	return out
}

func TestGridApply(t *testing.T) {
	var g grid
	g.resize(4, 2)
	for y := range g.h {
		g.dirty[y] = false
	}
	g.apply([]tui.CellUpdate{{X: 1, Y: 1, Cell: cell("a")}, {X: 9, Y: 9, Cell: cell("b")}})
	if g.at(1, 1) != cell("a") {
		t.Fatalf("at(1,1) = %+v", g.at(1, 1))
	}
	if g.dirty[0] || !g.dirty[1] {
		t.Fatalf("dirty = %v; want only row 1", g.dirty)
	}
	if g.at(9, 9) != blank {
		t.Fatal("a cell outside the grid was not dropped")
	}
}

func TestFullRepaint(t *testing.T) {
	size := tui.Size{W: 3, H: 2}
	if !fullRepaint(full(3, 2), size) {
		t.Fatal("a row-major repaint of every cell was not recognised")
	}
	// The same number of cells for a different shape is not a repaint of this grid.
	if fullRepaint(full(2, 3), size) {
		t.Fatal("a 2×3 repaint was taken for a 3×2 one")
	}
	if fullRepaint(full(3, 2)[:5], size) {
		t.Fatal("a partial diff was taken for a repaint")
	}
	if fullRepaint(nil, tui.Size{}) {
		t.Fatal("an empty grid has no repaint")
	}
}

// Layout and paint agree after a resize: until the App's first frame at the new grid size
// arrives, frames are drawn with the metrics they were laid out for.
func TestPaintMetricsFollowTheApp(t *testing.T) {
	small := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: 3, H: 2}}
	large := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: 4, H: 2}}
	b := &Backend{}
	b.metrics.Store(&small)
	if m := b.paintMetrics(full(3, 2)); m != small {
		t.Fatalf("first frame drew with %+v", m)
	}
	b.grid.resize(3, 2)

	b.metrics.Store(&large) // the window grew; the App has not seen the ResizeEvent yet
	if m := b.paintMetrics([]tui.CellUpdate{{X: 0, Y: 0, Cell: cell("z")}}); m != small {
		t.Fatalf("a frame laid out for 3×2 drew with %+v", m.grid)
	}
	if b.grid.w != 3 {
		t.Fatal("the grid resized before the App did")
	}
	if m := b.paintMetrics(full(4, 2)); m != large || b.grid.w != 4 {
		t.Fatalf("the App's first 4×2 frame drew with %+v on a %d-wide grid", m.grid, b.grid.w)
	}
}
