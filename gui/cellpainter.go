package gui

import (
	"image"
	"math"
	"slices"

	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/unit"

	"github.com/yongjohnlee80/golib/tui"
)

// CELL PAINTER — a grid of tui cells drawn in a native view at a text size of its own, with the
// window's own cell code: a second renderer (newRenderer, as CanvasStream runs one) whose metrics
// come from measure at that size, drawing through recordRow and drawSpan (backgrounds, box drawing,
// glyph batching, wide and colour glyphs) and drawCursor. Its glyph cache, shape cache and row macros
// are its own; the window's are never touched. The window's typeface, face table and theme are
// read at each Paint, so a font, face or theme change reaches it.

// CellPainter draws cells for one view. Loop goroutine, inside the view's Paint. On a
// RecordingCanvas it records a PaintTerminalCells call (the frame's size, the caret's cell) instead.
type CellPainter struct {
	r       *renderer
	fm      fontMetrics
	fontGen uint64 // the window's font generation r and fm were made for
	made    bool
	g       grid
	rows    []op.CallOp
}

// CellCaret is the caret a CellPainter draws over its cells: where, its shape, and whether.
type CellCaret struct {
	X, Y  int
	Shape tui.CursorShape
	Show  bool
}

// NewCellPainter is a painter with nothing drawn yet.
func NewCellPainter() *CellPainter { return &CellPainter{} }

// cellHost is the optional Canvas capability a CellPainter draws through: the window's canvas
// measures and paints cells; a RecordingCanvas records the call. A canvas embedding either has it.
type cellHost interface {
	cellMetrics(p *CellPainter, px float32) (cell Size, baseline float32)
	// paintCells is only called with cols, rows > 0 and cells holding cols×rows.
	paintCells(p *CellPainter, px float32, cells []tui.Cell, cols, rows int, caret CellCaret)
}

// Cell is the cell, in logical pixels, of text size px (logical) on c, and the glyphs' baseline in
// it: as the window's cell is measured from its text size. Zero on a canvas with no cells.
func (p *CellPainter) Cell(c Canvas, px float32) (cell Size, baseline float32) {
	h, ok := c.(cellHost)
	if !ok {
		return Size{}, 0
	}
	return h.cellMetrics(p, px)
}

// Paint draws cells (cols×rows, row-major, as tui.Context.RenderOffscreen gives them) at c's
// origin at text size px, clipped to c, and the caret over them. A canvas with no cells, or cells
// short of cols×rows, draws nothing.
func (p *CellPainter) Paint(c Canvas, px float32, cells []tui.Cell, cols, rows int, caret CellCaret) {
	h, ok := c.(cellHost)
	if !ok || cols <= 0 || rows <= 0 || len(cells) < cols*rows {
		return
	}
	h.paintCells(p, px, cells, cols, rows, caret)
}

// cellMetrics measures the window's cell at px.
func (gc *gioCanvas) cellMetrics(p *CellPainter, px float32) (Size, float32) {
	p.renew(gc)
	m := p.metrics(gc, px, 0, 0)
	return Size{W: float32(m.cell.X) / m.scale, H: float32(m.cell.Y) / m.scale}, float32(m.baseline) / m.scale
}

// paintCells draws the cells with p's own renderer, re-recording only the rows that changed.
func (gc *gioCanvas) paintCells(p *CellPainter, px float32, cells []tui.Cell, cols, rows int, caret CellCaret) {
	p.renew(gc)
	r := p.r
	r.faces, r.fonts = gc.r.faces, gc.r.fonts
	m := p.metrics(gc, px, cols, rows)
	if !r.m.sameCells(m) || r.theme != gc.r.theme || p.g.w != cols || p.g.h != rows {
		if r.m.ppem != m.ppem {
			clear(r.glyphs)
			r.paths.clear()
		}
		r.m, r.theme = m, gc.r.theme
		p.g.resize(cols, rows)
		p.g.touchAll()
		p.rows = make([]op.CallOp, rows)
	}
	for y := range rows {
		row := cells[y*cols : (y+1)*cols]
		if !slices.Equal(p.g.cells[y*cols:(y+1)*cols], row) {
			copy(p.g.cells[y*cols:], row)
			p.g.dirty[y] = true
		}
		if p.g.dirty[y] {
			p.rows[y] = r.recordRow(&p.g, y)
			p.g.dirty[y] = false
		}
	}
	cl := clip.Rect(gc.clip).Push(gc.ops)
	for _, row := range p.rows {
		row.Add(gc.ops)
	}
	if caret.Show {
		r.drawCursor(gc.ops, &p.g, cursorState{visible: true, x: caret.X, y: caret.Y, shape: caret.Shape}, true)
	}
	cl.Pop()
	r.paths.endFrame()
}

// renew makes the painter's renderer, and measures the face, again when the window's font changed.
func (p *CellPainter) renew(gc *gioCanvas) {
	if p.made && p.fontGen == gc.m.fontGen && p.r.typeface == gc.r.typeface {
		return
	}
	p.r = newRenderer(gc.r.shaper, string(gc.r.typeface), gc.r.theme)
	p.fm = measureFont(gc.r.shaper, string(gc.r.typeface))
	p.fontGen, p.made = gc.m.fontGen, true
	p.g = grid{}
}

// metrics is the window's metrics at text size px for a grid of cols×rows, its origin the view's:
// one pane, no layout.
func (p *CellPainter) metrics(gc *gioCanvas, px float32, cols, rows int) metrics {
	scale := gc.m.scale
	if scale <= 0 {
		scale = 1
	}
	m := measure(p.fm, unit.Sp(px), 0, gc.m.window, unit.Metric{PxPerDp: scale, PxPerSp: scale})
	m.origin = image.Pt(int(math.Round(float64(gc.origin.X))), int(math.Round(float64(gc.origin.Y))))
	m.fontGen = gc.m.fontGen
	m.grid = tui.Size{W: cols, H: rows}
	return m
}

// headlessCell is a typical monospace cell at text size px, for a RecordingCanvas, which has no
// font: 0.6 em wide, 1.2 em tall, the baseline at 0.96 em.
func headlessCell(px float32) (Size, float32) {
	return Size{W: float32(math.Round(float64(px * 0.6))), H: float32(math.Ceil(float64(px * 1.2)))}, px * 0.96
}

// cellMetrics is a headless cell: a RecordingCanvas has no font.
func (c *RecordingCanvas) cellMetrics(_ *CellPainter, px float32) (Size, float32) {
	return headlessCell(px)
}

// paintCells records the frame, not its cells: Rect is the caret's cell, when shown.
func (c *RecordingCanvas) paintCells(_ *CellPainter, px float32, _ []tui.Cell, cols, rows int, caret CellCaret) {
	call := PaintCall{Op: "PaintTerminalCells", Cells: CellRect{W: cols, H: rows}}
	if caret.Show {
		cell, _ := headlessCell(px)
		call.Rect = Rect{X: float32(caret.X) * cell.W, Y: float32(caret.Y) * cell.H, W: cell.W, H: cell.H}
	}
	c.rec(call)
}
