package gui

import (
	"fmt"
	"image"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/errs"
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

// Snapshot draws what the Backend's last Flush published, as the window presents it (present):
// the theme's background, the published frame (its cells, and its native views composited over
// them) and its stream parts, on a headless GPU window, and reads it back: 8x16 pixel cells at
// scale 1. Its error, on a machine with no headless GPU, is ErrNoGPU.
func (h *HeadlessFrames) Snapshot() (*image.RGBA, error) {
	h.B.mu.Lock()
	f := h.B.latest
	h.B.mu.Unlock()
	if f == nil {
		return nil, fmt.Errorf("gui: HeadlessFrames.Snapshot: no frame published yet")
	}
	hw, err := headless.NewWindow(h.m.window.X, h.m.window.Y)
	if err != nil {
		return nil, errs.WrapCause(ErrNoGPU, err, "gui: HeadlessFrames.Snapshot")
	}
	defer hw.Release()
	ops := new(op.Ops)
	fillRect(ops, image.Rectangle{Max: h.m.window}, h.B.cfg.theme.BG)
	f.draw.Add(ops)
	for _, p := range f.parts {
		p.slot.add(ops)
		p.seg.Add(ops)
	}
	if err := hw.Frame(ops); err != nil {
		return nil, fmt.Errorf("gui: HeadlessFrames.Snapshot: drawing: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, h.m.window.X, h.m.window.Y))
	if err := hw.Screenshot(img); err != nil {
		return nil, fmt.Errorf("gui: HeadlessFrames.Snapshot: reading back: %w", err)
	}
	return img, nil
}
