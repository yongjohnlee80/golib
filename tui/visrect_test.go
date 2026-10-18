package tui

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// A clip is a box at screen rows 2..4 holding a child placed two rows above it and six rows tall,
// as a ScrollView places its scrolled child: rows 0..5 on the screen, of which 2..4 show
// (ADR 1792358868 §4.2).
func clipped(child Component, w int) *box {
	clip := &box{rects: []Rect{{Y: -2, W: w, H: 6}}}
	clip.Add(child)
	root := &box{rects: []Rect{{Y: 2, W: w, H: 3}}}
	root.Add(clip)
	return root
}

// A cursor on a row its ancestors cut off is hidden, not drawn over whatever is there; on a row
// that shows it is drawn where it is.
func TestACursorOutsideWhatShowsIsHidden(t *testing.T) {
	input := &cursorProbe{}
	input.name = "input"
	input.accepts.Store(true)
	input.cx, input.cy = 2, 3 // screen row 3: shows
	input.report.Store(true)
	h := startApp(t, clipped(input, 10), 10, 8)
	h.onLoop(func() { input.ctx.RequestFocus() })
	waitFor(t, "the cursor drawn", func() bool {
		x, y, vis := h.tb.CursorPos()
		return vis && x == 2 && y == 3
	})

	h.onLoop(func() {
		input.cy = 1 // screen row 1: above the clip
		input.ctx.MarkDirty()
	})
	waitFor(t, "the cursor hidden", func() bool {
		_, _, vis := h.tb.CursorPos()
		return !vis
	})
}

// A native view partly cut off is placed over the part that shows, with its whole rect in Full; one
// wholly shown has no Full; one wholly cut off is not placed.
func TestANativeViewPartlyCutOffKeepsItsWholeRect(t *testing.T) {
	_, nb := startNative(t, clipped(reporterBox{&box{view: "graph", report: true}}, 8), 12, 8, nil)
	p := placements(t, nb, 1)[0]
	if p.X != 0 || p.Y != 2 || p.Cols != 8 || p.Rows != 3 || p.Full != (Rect{W: 8, H: 6}) {
		t.Fatalf("placement %+v; want rows 2..4 shown of Full {0 0 8 6}", p)
	}

	whole := &box{rects: []Rect{{X: 1, Y: 1, W: 4, H: 2}}}
	whole.Add(reporterBox{&box{view: "whole", report: true}})
	_, nb = startNative(t, whole, 8, 4, nil)
	if p := placements(t, nb, 1)[0]; p.Full != (Rect{}) || p.Y != 1 || p.Rows != 2 {
		t.Fatalf("a wholly shown view: %+v; want no Full", p)
	}

	gone := &box{rects: []Rect{{Y: 2, W: 8, H: 2}}}
	inner := &box{rects: []Rect{{Y: -5, W: 8, H: 3}}} // rows -3..-1 inside a clip at 2..3: none shows
	inner.Add(reporterBox{&box{view: "gone", report: true}})
	gone.Add(inner)
	h, nb := startNative(t, gone, 8, 4, nil)
	h.sync()
	h.sync()
	if ps := nb.last(); len(ps) != 0 {
		t.Fatalf("a wholly cut-off view was placed: %+v", ps)
	}
}

// An image partly cut off shows the same part of its picture: rows 2..4 of 0..5 are the second to
// the fifth sixth of it, through Clip when the reporter set one and the PNG's own size when not.
func TestAnImagePartlyCutOffIsCropped(t *testing.T) {
	c := &imaged{probe: &probe{name: "img"}, img: Image{ID: 91, PNG: []byte("png"), Version: 1, Clip: Rect{W: 40, H: 60}}, show: true}
	h := startApp(t, clipped(c, 4), 4, 8)
	h.waitImages(map[uint32]ImagePlacement{91: {
		Image: Image{ID: 91, PNG: []byte("png"), Version: 1, Clip: Rect{Y: 20, W: 40, H: 30}},
		Y:     2, Cols: 4, Rows: 3,
	}})

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 12))); err != nil {
		t.Fatal(err)
	}
	h.onLoop(func() {
		c.img = Image{ID: 91, PNG: buf.Bytes(), Version: 2}
		c.ctx.MarkDirty()
	})
	h.waitImages(map[uint32]ImagePlacement{91: {
		Image: Image{ID: 91, PNG: buf.Bytes(), Version: 2, Clip: Rect{Y: 4, W: 8, H: 6}},
		Y:     2, Cols: 4, Rows: 3,
	}})
}

func TestCropTo(t *testing.T) {
	src := Rect{X: 10, Y: 20, W: 100, H: 50}
	whole := Rect{X: 0, Y: -5, W: 10, H: 10}
	if got, want := cropTo(src, whole, Rect{X: 2, Y: 0, W: 6, H: 5}), (Rect{X: 30, Y: 45, W: 60, H: 25}); got != want {
		t.Fatalf("cropTo = %+v; want %+v", got, want)
	}
	if got := cropTo(src, Rect{}, Rect{}); got != (Rect{}) {
		t.Fatalf("an empty whole crops to %+v; want nothing", got)
	}
}
