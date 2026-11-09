package gui

import (
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// frame is one macro of a frame with stream slots: what the view drew before the slot and after
// it, joined, and the slot itself drawing nothing (the caller wanted a single macro).
func TestFrameJoinsTheSegmentsAroundAStreamSlot(t *testing.T) {
	hw, err := headless.NewWindow(64, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	st := b.NewStream()
	defer st.Close()
	var rel int
	st.Push(memFrame(t, 4, 4, [4]byte{0, 0xc0, 0, 255}, &rel)) // green, never drawn by frame
	red, blue := color.NRGBA{R: 0xc0, A: 255}, color.NRGBA{B: 0xc0, A: 255}
	slots := 0
	view := ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: 64, H: 64}, Brush{Color: red})
		if DrawStream(c, st, Rect{W: 32, H: 32}) {
			slots++
		}
		c.FillRect(Rect{X: 40, Y: 40, W: 8, H: 8}, Brush{Color: blue}) // after the slot
	})
	g := &grid{}
	g.resize(8, 4)
	b.render.m = m
	call := b.render.frame(g, m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}, nil, true)
	if slots != 1 {
		t.Fatalf("%d stream slots taken; want 1, so frame joins segments", slots)
	}
	ops := new(op.Ops)
	call.Add(ops)
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
	if c := at(10, 10); c.R < 0xa0 || c.G > 0x20 {
		t.Errorf("(10,10) %v; want the red drawn before the slot, and no stream picture", c)
	}
	if c := at(44, 44); c.B < 0xa0 || c.R > 0x20 {
		t.Errorf("(44,44) %v; want the blue drawn after the slot: the segment after it was joined", c)
	}
}
