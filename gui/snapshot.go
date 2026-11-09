package gui

import (
	"fmt"
	"image"
	"math"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// Snapshot renders v in a size dp view at a whole scale (device pixels per dp, 1 when not positive)
// on a headless GPU, through the window's own path (a native view placed over the grid), and returns
// the pixels: size × scale, rounded up. Its text is golib's Go fonts, never the system's, so a snapshot
// is the same on every machine with the same GPU driver.
//
// For tests that assert what a widget draws in pixels: a golden, or a colour at a point. A machine
// with no headless GPU returns an error that is ErrNoGPU, which a test skips on; any other error is
// the drawing or its read-back failing, which a test must fail on.
//
// Each call makes a headless window of its own. A test taking several snapshots of one size uses
// a Snapshotter instead: EGL refuses some of many windows made in a row.
func Snapshot(v View, size Size, scale int) (*image.RGBA, error) {
	s, err := NewSnapshotter(size, scale)
	if err != nil {
		return nil, err
	}
	defer s.Release()
	return s.Snap(v)
}

// Snapshotter takes snapshots of one size and scale on one headless window: what Snapshot does, for
// a test that takes many. Release it when done. It is not safe for concurrent use.
type Snapshotter struct {
	hw    *headless.Window
	size  Size
	scale int
}

// NewSnapshotter is a Snapshotter of size dp at a whole scale (1 when not positive). Its error, on a
// machine with no headless GPU, is ErrNoGPU.
func NewSnapshotter(size Size, scale int) (*Snapshotter, error) {
	scale = max(scale, 1)
	w, h := snapshotPixels(size, scale)
	hw, err := headless.NewWindow(w, h)
	if err != nil {
		return nil, errs.WrapCause(ErrNoGPU, err, "gui: Snapshot")
	}
	return &Snapshotter{hw: hw, size: size, scale: scale}, nil
}

// Snap renders v as Snapshot does, on the Snapshotter's window. An error is the drawing or its
// read-back failing.
func (s *Snapshotter) Snap(v View) (*image.RGBA, error) { return snapshotOn(s.hw, v, s.size, s.scale) }

// Release frees the window.
func (s *Snapshotter) Release() { s.hw.Release() }

// ErrNoGPU is Snapshot's error on a machine with no headless GPU to draw on.
var ErrNoGPU = errs.Sentinel(errs.ErrUnsupported, "gui: no headless GPU")

// snapshotTarget is what a snapshot draws on and reads back: a headless window, or a test's own.
type snapshotTarget interface {
	Frame(*op.Ops) error
	Screenshot(*image.RGBA) error
}

// snapshotOn is Snapshot on t, a headless window of the snapshot's pixel size: a test drawing many
// snapshots of one size reuses one window (EGL refuses some of many made in a row).
func snapshotOn(t snapshotTarget, v View, size Size, scale int) (*image.RGBA, error) {
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
	if err := t.Frame(ops); err != nil {
		return nil, fmt.Errorf("gui: Snapshot: drawing: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := t.Screenshot(img); err != nil {
		return nil, fmt.Errorf("gui: Snapshot: reading back: %w", err)
	}
	return img, nil
}

// snapshotPixels is a snapshot's size in device pixels: size's whole dp at scale.
func snapshotPixels(size Size, scale int) (w, h int) {
	return int(math.Ceil(float64(size.W))) * scale, int(math.Ceil(float64(size.H))) * scale
}
