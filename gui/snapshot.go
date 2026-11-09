package gui

import (
	"image"
	"math"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// Snapshot renders v in a size dp view at a whole scale (device pixels per dp, 1 when not positive)
// on a headless GPU, through the window's own path (a native view placed over the grid), and returns
// the pixels: size × scale, rounded up. Its text is golib's Go fonts, never the system's, so a snapshot
// is the same on every machine with the same GPU driver. An error is no headless GPU here.
//
// For tests that assert what a widget draws in pixels: a golden, or a colour at a point.
func Snapshot(v View, size Size, scale int) (*image.RGBA, error) {
	scale = max(scale, 1)
	w, h := snapshotPixels(size, scale)
	hw, err := headless.NewWindow(w, h)
	if err != nil {
		return nil, err
	}
	defer hw.Release()
	return snapshotOn(hw, v, size, scale)
}

// snapshotOn is Snapshot on hw, a headless window of the snapshot's pixel size: a test drawing many
// snapshots of one size reuses one window (EGL refuses some of many made in a row).
func snapshotOn(hw *headless.Window, v View, size Size, scale int) (*image.RGBA, error) {
	w, h := snapshotPixels(size, scale)
	// one-dp cells, so the view's canvas is size, rounded up to whole dp
	cols, rows := int(math.Ceil(float64(size.W))), int(math.Ceil(float64(size.H)))
	m := metrics{window: image.Pt(w, h), cell: image.Pt(scale, scale), ppem: float32(14 * scale), baseline: scale,
		scale: float32(scale), grid: tui.Size{W: cols, H: rows}}
	r := newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	r.m = m
	g := &grid{}
	g.resize(cols, rows)
	call := r.frame(g, m, cursorState{}, []tui.NativePlacement{{View: v, Cols: cols, Rows: rows}}, nil, true)
	ops := new(op.Ops)
	call.Add(ops)
	if err := hw.Frame(ops); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := hw.Screenshot(img); err != nil {
		return nil, err
	}
	return img, nil
}

// snapshotPixels is a snapshot's size in device pixels: size's whole dp at scale.
func snapshotPixels(size Size, scale int) (w, h int) {
	return int(math.Ceil(float64(size.W))) * scale, int(math.Ceil(float64(size.H))) * scale
}
