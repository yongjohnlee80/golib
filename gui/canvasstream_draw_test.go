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

// DrawCanvasStream refuses what is not its own window's canvas: a nil stream, a recording canvas,
// and a stream of another window.
func TestDrawCanvasStreamRefusesWhatIsNotItsWindowsCanvas(t *testing.T) {
	b := NewBackend()
	cs := b.NewCanvasStream()
	defer cs.Close()
	rec := NewRecordingCanvas(Size{W: 10, H: 10}, Size{W: 8, H: 16})
	if DrawCanvasStream(rec, cs, Rect{W: 10, H: 10}) {
		t.Error("a recording canvas took a stream slot")
	}

	hw, err := headless.NewWindow(64, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	other := NewBackend().NewCanvasStream()
	defer other.Close()
	var nilOK, otherOK bool
	view := ViewFunc(func(c Canvas) {
		nilOK = DrawCanvasStream(c, nil, Rect{W: 8, H: 8})
		otherOK = DrawCanvasStream(c, other, Rect{W: 8, H: 8})
	})
	g := &grid{}
	g.resize(8, 4)
	b.render.m = m
	_, parts := b.render.frameParts(g, m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}, nil, true)
	if nilOK || otherOK || len(parts) != 0 {
		t.Fatalf("nil stream %v, another window's %v, %d slots; want both refused, no slot", nilOK, otherOK, len(parts))
	}
}

// A CanvasStream's picture is drawn into its slot scaled to dst, in paint order: over what the
// view drew before it, under what it drew after. The picture is 16x16 dp of green, the slot
// 32x32: it fills the slot, scaled.
func TestACanvasStreamSlotIsDrawnScaledInPaintOrder(t *testing.T) {
	hw, err := headless.NewWindow(64, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	b.metrics.Store(&m)
	cs := b.NewCanvasStream()
	defer cs.Close()
	green, red, blue := color.NRGBA{G: 0xc0, A: 255}, color.NRGBA{R: 0xc0, A: 255}, color.NRGBA{B: 0xc0, A: 255}
	cs.Push(Size{W: 16, H: 16}, func(c Canvas) { c.FillRect(Rect{W: 16, H: 16}, Brush{Color: green}) })
	view := ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: 64, H: 64}, Brush{Color: red})
		if !DrawCanvasStream(c, cs, Rect{W: 32, H: 32}) {
			t.Error("DrawCanvasStream refused the window's canvas")
		}
		c.FillRect(Rect{W: 8, H: 8}, Brush{Color: blue}) // after the slot: on top
	})
	g := &grid{}
	g.resize(8, 4)
	b.render.m = m
	draw, parts := b.render.frameParts(g, m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}, nil, true)
	if len(parts) != 1 {
		t.Fatalf("%d slots recorded; want 1", len(parts))
	}
	ops := new(op.Ops)
	draw.Add(ops)
	for _, p := range parts {
		p.slot.add(ops)
		p.seg.Add(ops)
	}
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
	if c := at(28, 28); c.G < 0xa0 || c.R > 0x20 {
		t.Errorf("(28,28) %v; want the picture's green, scaled to fill the 32x32 slot, over the red", c)
	}
	if c := at(4, 4); c.B < 0xa0 || c.G > 0x20 {
		t.Errorf("(4,4) %v; want the blue drawn after the slot, on top", c)
	}
	if c := at(40, 40); c.R < 0xa0 || c.G > 0x20 {
		t.Errorf("(40,40) %v; want the red outside the slot", c)
	}
}
