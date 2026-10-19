package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/gio/unit"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// stubTermView is a TerminalView that is a View: what the native style paints a Terminal with.
type stubTermView struct{ painted int }

func (*stubTermView) Cells(w, h int) (int, int)                                    { return w, h }
func (*stubTermView) Pointer(e tui.MouseEvent) tui.MouseEvent                      { return e }
func (*stubTermView) Frame([]tui.Cell, int, int, tui.Point, tui.CursorShape, bool) {}
func (*stubTermView) Detached()                                                    {}
func (v *stubTermView) Paint(Canvas)                                               { v.painted++ }

// The native style paints a Terminal whose view is a View with that view, over its subtree; a
// Terminal with no view is left to its cells.
func TestNativeStylePaintsATerminalByItsView(t *testing.T) {
	term := widget.NewTerminal()
	app, sb := styledApp(t, term, 20, 6, NativeStyle())
	var id tui.NodeID
	v := &stubTermView{}
	placed := func() bool {
		for _, p := range sb.placements() {
			if p.Node == id {
				return true
			}
		}
		return false
	}
	onLoop(t, app, func() { id = term.NodeID() })
	if placed() {
		t.Fatal("a terminal with no view is drawn natively")
	}
	onLoop(t, app, func() { term.SetTerminalView(v) })
	waitUntil(t, "the terminal's native placement", placed)
	p := placementOf(t, sb, id)
	if p.Scope != tui.ScopeSubtree {
		t.Fatalf("scope %v, want the subtree", p.Scope)
	}
	paintOn(t, app, sb, p)
	if v.painted == 0 {
		t.Fatal("the view was not painted")
	}
	onLoop(t, app, func() { term.SetTerminalView(nil) })
	waitUntil(t, "the placement gone", func() bool { return !placed() })
}

// headlessGio is a window's canvas without a window: a renderer of Go Mono at 14 px and its metrics.
func headlessGio(t *testing.T) *gioCanvas {
	t.Helper()
	sh := text.NewShaper(text.WithCollection(gofont.Collection()))
	r := newRenderer(sh, "Go Mono", DefaultTheme())
	m := measure(measureFont(sh, "Go Mono"), unit.Sp(14), 0, image.Pt(400, 300), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	return &gioCanvas{ops: new(op.Ops), r: r, m: m, origin: f32.Pt(10, 20), clip: image.Rect(10, 20, 400, 300)}
}

// CellPainter at the window's own text size measures the window's cell; it draws through a renderer
// of its own (its glyphs cached apart from the window's), records only rows that changed, and makes
// its renderer again when the window's font changes.
func TestCellPainterIsTheWindowsCellCodeApart(t *testing.T) {
	gc := headlessGio(t)
	p := NewCellPainter()
	cell, base := p.Cell(gc, 14)
	if cell != (Size{W: float32(gc.m.cell.X), H: float32(gc.m.cell.Y)}) || base != float32(gc.m.baseline) {
		t.Fatalf("at the window's size: %v %v, want the window's cell %v %d", cell, base, gc.m.cell, gc.m.baseline)
	}
	if small, _ := p.Cell(gc, 7); small.W >= cell.W || small.H >= cell.H {
		t.Fatalf("at half size: %v, want a smaller cell than %v", small, cell)
	}
	cells := []tui.Cell{{Content: "a", Width: 1}, {Content: "界", Width: 2}, {Content: "", Width: 0}, {Content: "b", Width: 1},
		{Content: "─", Width: 1}, {Content: "─", Width: 1}, {Content: " ", Width: 1}, {Content: "c", Width: 1}}
	p.Paint(gc, 10, cells, 4, 2, CellCaret{X: 3, Y: 1, Show: true})
	if len(gc.r.glyphs) != 0 || len(p.r.glyphs) == 0 {
		t.Fatalf("glyphs cached: window %d, painter %d; want the painter's own", len(gc.r.glyphs), len(p.r.glyphs))
	}
	if p.r.m.origin != image.Pt(10, 20) || len(p.rows) != 2 {
		t.Fatalf("origin %v, %d rows", p.r.m.origin, len(p.rows))
	}
	first := p.rows[0]
	cells[7] = tui.Cell{Content: "d", Width: 1} // only row 1 changes
	p.Paint(gc, 10, cells, 4, 2, CellCaret{})
	if p.rows[0] != first {
		t.Fatal("an unchanged row was recorded again")
	}
	r := p.r
	gc.m.fontGen++
	p.Paint(gc, 10, cells, 4, 2, CellCaret{})
	if p.r == r {
		t.Fatal("a font change kept the old renderer")
	}
}
