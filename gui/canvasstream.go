package gui

import (
	"image"
	"sync/atomic"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
)

// CanvasStream feeds a view with pictures drawn by a Canvas on the producer's goroutine: a game
// loop's world (sprites, tiles, shapes, text) at the display's pace, with no tui pass. Make it
// with Backend.NewCanvasStream; draw it in a View with DrawCanvasStream.
//
// One producer at a time: a Push while another is recording, a re-entrant one included, does not
// call draw and completes Busy. No lock is held while draw runs, so Close and Stop never wait on
// it.
type CanvasStream struct {
	s         stream
	recording atomic.Bool

	// The producer's own text shaping, rebuilt when the window's font changes. Used only by the
	// Push holding recording.
	r       *renderer
	fontGen uint64
}

// NewCanvasStream is a canvas stream drawn in this window, with its fonts and scale.
func (b *Backend) NewCanvasStream() *CanvasStream {
	cs := &CanvasStream{}
	cs.s.init(b)
	return cs
}

// Push records one picture of size (dp) with draw, on the calling goroutine, and hands it to the
// window. draw gets a Canvas like a View's Paint, at the window's current scale, with the
// stream's own text shaper; it has no cells behind it (the cell methods return zero values).
// Images are gui.Image values made once and reused: their textures are cached.
func (cs *CanvasStream) Push(size Size, draw func(c Canvas)) *Receipt {
	if !cs.recording.CompareAndSwap(false, true) {
		return receiptOf(Busy)
	}
	defer cs.recording.Store(false)
	cs.s.mu.Lock()
	closed := cs.s.closed
	cs.s.mu.Unlock()
	if closed {
		return receiptOf(Closed)
	}
	m := cs.snapshot()
	dev := image.Rect(0, 0, int(size.W*m.scale+0.5), int(size.H*m.scale+0.5))
	ops := new(op.Ops)
	rec := op.Record(ops)
	draw(&gioCanvas{ops: ops, r: cs.r, m: m, size: size, clip: dev, alpha: 1})
	return cs.s.push(&canvasPayload{call: rec.Stop(), size: size, scale: m.scale}, nil)
}

// snapshot is the window's scale and font for one recording (1 and the default font before the
// window has drawn), and the stream's shaper, rebuilt on a font change.
func (cs *CanvasStream) snapshot() metrics {
	b := cs.s.b
	m := metrics{scale: 1}
	if cur := b.metrics.Load(); cur != nil && cur.scale > 0 {
		m.scale, m.ppem, m.cell, m.baseline = cur.scale, cur.ppem, cur.cell, cur.baseline
	}
	fs := b.font.Load()
	if cs.r == nil || fs.gen != cs.fontGen {
		cs.r = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection())), fs.typeface, b.cfg.theme)
		cs.r.fonts = fs.fonts
		cs.fontGen = fs.gen
	}
	return m
}

// Close drops the stream's pictures; a pending push completes Closed. A draw in progress finishes
// and its picture completes Closed.
func (cs *CanvasStream) Close() { cs.s.close() }

// canvasPayload is one recorded picture: device pixels at the scale it was recorded at.
type canvasPayload struct {
	call  op.CallOp
	size  Size
	scale float32
}

func (p *canvasPayload) add(ops *op.Ops, dst Rect, _ func(error)) {
	if p.size.W <= 0 || p.size.H <= 0 {
		return
	}
	// Recorded in device pixels at p.scale; the slot's coordinates are dp. A scale change since
	// recording is this transform, not a new recording.
	sx := dst.W / (p.size.W * p.scale)
	sy := dst.H / (p.size.H * p.scale)
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx, sy)).Offset(f32.Pt(dst.X, dst.Y))).Push(ops)
	p.call.Add(ops)
	tr.Pop()
}

func (p *canvasPayload) drop() {}

// DrawCanvasStream records a slot in c where the window draws cs's newest picture, scaled into
// dst, at every vsync after a push. false: c can't, or cs belongs to another window.
func DrawCanvasStream(c Canvas, cs *CanvasStream, dst Rect) bool {
	d, ok := c.(streamDrawer)
	return ok && cs != nil && d.drawStream(&cs.s, dst)
}
