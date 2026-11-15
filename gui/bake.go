package gui

import (
	"errors"
	"image"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/paint"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/gio/unit"
	"github.com/yongjohnlee80/golib/errs"
)

// PictureBaker draws a view once into an image, on a headless GPU, as its window would draw it:
// in the window's fonts, at the scale the caller paints at, transparent where the view paints
// nothing. A picture the window would otherwise fill as vector paths every frame (a chart, a
// diagram) is then painted as one image. Make it with Backend.NewPictureBaker.
//
// A bake is two steps, so a view is painted only on its loop (View's contract): Record paints the
// view into a recording on the calling goroutine, the view's loop, and does no GPU work; Draw puts
// the recording on the GPU and reads it back, off the loops. Bake is the two at once, for a caller
// that owns its view on its own goroutine. A machine with no headless GPU answers ErrNoGPU, once
// and for every call after: the caller draws the view as it would have. On Windows gio has no
// headless GPU.
//
// The baker keeps one headless window, as large as the largest bake yet, and lets it go (its GPU
// context, framebuffer and read-back buffer) after BakeIdle with no bake: a page's pictures are
// drawn when it is first shown, and the window would otherwise hold its memory for as long as the
// program runs.
type PictureBaker struct {
	b *Backend

	closed atomic.Bool
	noGPU  atomic.Bool // the headless GPU failed: every Record and Draw answers ErrNoGPU

	rmu     sync.Mutex // the recording side: the view's loop
	r       *renderer  // the window's fonts, rebuilt on a font change
	fontGen uint64

	wmu      sync.Mutex  // the drawing side: off the loops
	win      *bakeWindow // the headless window; nil before the first draw and after an idle spell
	made     bool        // a headless window was made: a later failure is not "no GPU"
	idle     time.Duration
	draws    uint64 // draws so far: the idle timer lets the window go if none came since
	timer    *time.Timer
	err      error    // the headless GPU's failure
	released []func() // OnRelease's
}

// bakeWindow is the headless window a bake draws in, of a whole number of bakeBucket pixels a
// side, and the buffer its pixels are read back into.
type bakeWindow struct {
	hw    *headless.Window
	w, h  int
	whole *image.RGBA
}

const bakeBucket = 256 // a headless window's sides are whole multiples of it

// BakeIdle is how long a baker keeps its headless window with no bake.
const BakeIdle = 30 * time.Second

// Baked is a view drawn into an image: Image is Pixels device pixels (RGBA, the same pixels),
// drawn at Scale (device pixels per dp) in the window's fonts of generation FontGen (Fonts.Gen).
type Baked struct {
	Image   *Image
	RGBA    *image.RGBA
	Pixels  image.Point
	Scale   float32
	FontGen uint64
}

// A Recording is a view painted for a bake (Record), for Draw to put on the GPU.
type Recording struct {
	ops     *op.Ops
	px      image.Point
	scale   float32
	fontGen uint64
}

// ErrBakerClosed is a Record or a Draw after Release.
var ErrBakerClosed = errors.New("gui: the picture baker is released")

// NewPictureBaker is a baker for this window's pictures.
func (b *Backend) NewPictureBaker() *PictureBaker { return &PictureBaker{b: b, idle: BakeIdle} }

// BakedPixels is the device-pixel size a bake of size dp at scale is: each side rounded up.
func BakedPixels(size Size, scale float32) image.Point {
	return image.Pt(int(math.Ceil(float64(size.W*scale))), int(math.Ceil(float64(size.H*scale))))
}

// Record paints v of size dp at scale for a bake, on the calling goroutine, which owns v (its
// loop). Its image will be BakedPixels(size, scale).
func (pb *PictureBaker) Record(v View, size Size, scale float32) (*Recording, error) {
	switch {
	case pb.closed.Load():
		return nil, ErrBakerClosed
	case pb.noGPU.Load():
		return nil, pb.gpuErr()
	case scale <= 0:
		return nil, errors.New("gui: Bake: scale is not positive")
	}
	px := BakedPixels(size, scale)
	if px.X <= 0 || px.Y <= 0 {
		return nil, errors.New("gui: Bake: an empty picture")
	}
	pb.rmu.Lock()
	defer pb.rmu.Unlock()
	fs := pb.b.font.load()
	if pb.r == nil || fs.gen != pb.fontGen {
		// the window's shaping: golib's fonts and the system's beside them, the window's families
		pb.r = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection())), fs.typeface, pb.b.cfg.theme)
		pb.r.fonts = fs.fonts
		pb.fontGen = fs.gen
	}
	m := metrics{scale: scale}
	mm := measure(fs.fm, fs.size, 0, image.Point{}, unit.Metric{PxPerDp: scale, PxPerSp: scale})
	m.ppem, m.cell, m.baseline = mm.ppem, mm.cell, mm.baseline
	ops := new(op.Ops)
	v.Paint(&gioCanvas{ops: ops, r: pb.r, m: m, size: size, clip: image.Rectangle{Max: px}, paintState: opaque()})
	pb.r.paths.endFrame()
	return &Recording{ops: ops, px: px, scale: scale, fontGen: fs.gen}, nil
}

// Draw puts rec on the headless GPU and reads its pixels back, on the calling goroutine (off the
// loops: it waits on the GPU).
func (pb *PictureBaker) Draw(rec *Recording) (Baked, error) {
	pb.wmu.Lock()
	defer pb.wmu.Unlock()
	switch {
	case pb.closed.Load():
		return Baked{}, ErrBakerClosed
	case pb.err != nil:
		return Baked{}, pb.err
	case rec == nil:
		return Baked{}, errors.New("gui: Bake: no recording")
	}
	px := rec.px
	win, err := pb.window(px)
	if err != nil {
		return Baked{}, err
	}
	defer pb.armIdle()
	if err := win.hw.Frame(rec.ops); err != nil {
		return Baked{}, errs.Wrap(err, "gui: Bake: drawing")
	}
	// the whole window read back, then its corner kept: a read of the picture's size alone is the
	// window's at its GPU's origin, not where the view drew
	whole := win.whole
	if err := win.hw.Screenshot(whole); err != nil {
		return Baked{}, errs.Wrap(err, "gui: Bake: reading back")
	}
	img := image.NewRGBA(image.Rectangle{Max: px})
	for y := range px.Y {
		copy(img.Pix[y*img.Stride:y*img.Stride+4*px.X], whole.Pix[y*whole.Stride:y*whole.Stride+4*px.X])
	}
	return Baked{Image: NewImage(img), RGBA: img, Pixels: px, Scale: rec.scale, FontGen: rec.fontGen}, nil
}

// Bake draws v of size dp at scale into an image of BakedPixels(size, scale): Record, then Draw,
// on the calling goroutine, which owns v.
func (pb *PictureBaker) Bake(v View, size Size, scale float32) (Baked, error) {
	rec, err := pb.Record(v, size, scale)
	if err != nil {
		return Baked{}, err
	}
	return pb.Draw(rec)
}

// gpuErr is the headless GPU's failure, ErrNoGPU's.
func (pb *PictureBaker) gpuErr() error {
	pb.wmu.Lock()
	defer pb.wmu.Unlock()
	if pb.err != nil {
		return pb.err
	}
	return ErrNoGPU
}

// window is the headless window, made to hold px when it does not: as large as the largest bake
// yet, so a page's pictures share one. A first failure is ErrNoGPU's. Under pb.wmu.
func (pb *PictureBaker) window(px image.Point) (*bakeWindow, error) {
	if w := pb.win; w != nil && w.w >= px.X && w.h >= px.Y {
		return w, nil
	}
	bw, bh := bucketed(px.X), bucketed(px.Y)
	if w := pb.win; w != nil {
		bw, bh = max(bw, w.w), max(bh, w.h)
		w.hw.Release()
		pb.win = nil
	}
	hw, err := headless.NewWindow(bw, bh)
	if err != nil {
		if !pb.made {
			pb.err = errs.WrapCause(ErrNoGPU, err, "gui: Bake")
			pb.noGPU.Store(true)
			return nil, pb.err
		}
		return nil, errs.Wrap(err, "gui: Bake: a headless window")
	}
	pb.made = true
	pb.win = &bakeWindow{hw: hw, w: bw, h: bh, whole: image.NewRGBA(image.Rect(0, 0, bw, bh))}
	return pb.win, nil
}

// armIdle lets the window go after pb.idle with no draw. Under pb.wmu.
func (pb *PictureBaker) armIdle() {
	pb.draws++
	n := pb.draws
	if pb.timer != nil {
		pb.timer.Stop()
	}
	pb.timer = time.AfterFunc(pb.idle, func() {
		pb.wmu.Lock()
		defer pb.wmu.Unlock()
		if pb.draws == n && pb.win != nil {
			pb.win.hw.Release()
			pb.win = nil
		}
	})
}

// bucketed is n rounded up to whole bakeBuckets.
func bucketed(n int) int { return (n + bakeBucket - 1) / bakeBucket * bakeBucket }

// OnRelease calls fn when the baker is released (its window closed): a cache of its images lets
// them go. After Release, fn is called at once.
func (pb *PictureBaker) OnRelease(fn func()) {
	pb.wmu.Lock()
	if !pb.closed.Load() {
		pb.released = append(pb.released, fn)
		pb.wmu.Unlock()
		return
	}
	pb.wmu.Unlock()
	fn()
}

// Release lets the baker's headless window go and tells OnRelease's; a Record or a Draw after it
// answers ErrBakerClosed.
func (pb *PictureBaker) Release() {
	pb.wmu.Lock()
	if pb.closed.Swap(true) {
		pb.wmu.Unlock()
		return
	}
	if pb.timer != nil {
		pb.timer.Stop()
	}
	if pb.win != nil {
		pb.win.hw.Release()
	}
	pb.win = nil
	fns := pb.released
	pb.released = nil
	pb.wmu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// BakerOf is the PictureBaker of the window c paints in; nil for a canvas of no window's (a
// recording, an SVG, a snapshot), where a picture is drawn as it is.
func BakerOf(c Canvas) *PictureBaker {
	if g, ok := c.(*gioCanvas); ok && g.r != nil {
		return g.r.baker
	}
	return nil
}

// DrawBaked paints b with its corner at at (dp), b's pixels its size at b.Scale. On a window's
// canvas at that scale with no rotation or scale of its own it is drawn pixel for pixel, its corner
// on the nearest whole device pixel, so its text and hairlines stay as sharp as they were drawn;
// on any other canvas it is drawn as DrawImage draws it, into its size.
func DrawBaked(c Canvas, b Baked, at Point) {
	if b.Image == nil || b.Scale <= 0 || b.Pixels.X <= 0 || b.Pixels.Y <= 0 {
		return
	}
	dst := Rect{X: at.X, Y: at.Y, W: float32(b.Pixels.X) / b.Scale, H: float32(b.Pixels.Y) / b.Scale}
	g, ok := c.(*gioCanvas)
	if !ok || g.m.scale != b.Scale {
		c.DrawImage(b.Image, dst, Rect{})
		return
	}
	m := g.deviceTransform()
	if sx, hx, _, hy, sy, _ := m.Elems(); hx != 0 || hy != 0 || sx != b.Scale || sy != b.Scale {
		c.DrawImage(b.Image, dst, Rect{})
		return
	}
	p := m.Transform(f32.Pt(at.X, at.Y))
	x, y := int(math.Round(float64(p.X))), int(math.Round(float64(p.Y)))
	countImage()
	pp := pushPaint(g.ops, image.Rect(x, y, x+b.Pixels.X, y+b.Pixels.Y).Intersect(g.clip), g.alpha)
	countTransform()
	tr := op.Offset(image.Pt(x, y)).Push(g.ops)
	b.Image.op.Add(g.ops)
	paint.PaintOp{}.Add(g.ops)
	tr.Pop()
	pp.pop()
}
