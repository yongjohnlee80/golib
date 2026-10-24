package gui

import (
	"image"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// HeadlessFrames publishes a Backend's frames without a window, for the external tests that
// drive real gui/widget views: each Flush applies the diff and runs the real publishFrame, with
// 8×16 cells at scale 1.
type HeadlessFrames struct {
	B *Backend
	m metrics
}

// CursorCells is one of a published frame's cursor areas, in cells.
type CursorCells struct {
	X, Y, W, H int
	Cursor     pointer.Cursor
}

func NewHeadlessFrames(cols, rows int) *HeadlessFrames {
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	m := metrics{window: image.Pt(cols*8, rows*16), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: cols, H: rows}}
	b.grid.resize(cols, rows)
	b.metrics.Store(&m)
	return &HeadlessFrames{B: b, m: m}
}

// Flush is the Backend's Flush, headless: the frame it publishes, and that frame's cursor areas.
func (h *HeadlessFrames) Flush(diff []tui.CellUpdate) []CursorCells {
	h.B.grid.apply(diff)
	h.B.publishFrame(h.m)
	h.B.mu.Lock()
	defer h.B.mu.Unlock()
	var out []CursorCells
	for _, a := range h.B.latest.cursors {
		r := a.rect.Sub(h.m.origin)
		out = append(out, CursorCells{X: r.Min.X / h.m.cell.X, Y: r.Min.Y / h.m.cell.Y,
			W: r.Dx() / h.m.cell.X, H: r.Dy() / h.m.cell.Y, Cursor: a.cursor})
	}
	return out
}
