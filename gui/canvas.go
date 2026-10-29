package gui

import (
	"image"
	"image/color"
	"math"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/io/system"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/op/paint"

	"github.com/yongjohnlee80/golib/tui"
)

// Vector painting and native widget rendering surface.
//
// While the base terminal renderer maps characters to a 2D grid of text cells, native graphical
// components (such as vector diagrams, custom buttons, rich text document views, and image canvases)
// paint directly into sub-pixel resolution via [Canvas].
//
// # Architectural Model: The Pixel Twin of tui.Surface
//
// [Canvas] serves as the vector graphics counterpart to [tui.Surface]:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                       tui.App Screen                        │
//	│                                                             │
//	│  [ Terminal Character Grid ]       [ Native Vector View ]   │
//	│  - Discrete cell coordinates       - Device-independent dp  │
//	│  - tui.Surface.SetCell(...)        - Canvas.FillRRect(...)  │
//	│                                    - Canvas.DrawText(...)   │
//	└─────────────────────────────────────────────────────────────┘
//
// Coordinates and Scaling:
//   - All coordinates and dimensions passed to [Canvas] methods are expressed in logical
//     device-independent pixels (dp), with origin (0, 0) positioned at the component's top-left.
//   - Drawing operations are clipped to the component's assigned layout bounds.
//   - Transformations (translation, scaling, rotation) and opacity modifiers are pushed and
//     popped using [Canvas.PushTransform], [Canvas.PushOpacity], and [Canvas.Pop].
//   - Native views can inspect underlying cell contents or paint them selectively via [Canvas.PaintCells].

// View represents the pixel-rendering interface implemented by native GUI components.
//
// Concurrency & Lifecycle:
//   - The GUI backend invokes [View.Paint] exclusively on the TUI event loop goroutine
//     during [Backend.Flush].
//   - This guarantees that views read their component state synchronously alongside the
//     owning event loop, eliminating cross-thread race conditions.
type View interface {
	// Paint renders the component into the provided vector canvas.
	// The canvas origin (0, 0) corresponds to the component's top-left pixel.
	Paint(c Canvas)
}

// ViewFunc wraps a standalone painting function into a [View].
type ViewFunc func(c Canvas)

// Paint invokes f(c).
func (f ViewFunc) Paint(c Canvas) { f(c) }

// Canvas provides immediate-mode 2D vector drawing operations in logical pixels: a [Drawer], the
// [TextEnv] it draws text with, the [CellView] of the grid under the view, and the [WindowChrome]
// it may mark. A function that only draws asks for the part it uses.
type Canvas interface {
	// Sub returns a sub-canvas bounded by rect r, pre-clipped to r, with (0,0) mapped to r.Min.
	Sub(r Rect) Canvas

	Drawer
	TextEnv
	CellView
	WindowChrome
}

// Drawer is a canvas's drawing: shapes, paths, text and images in logical pixels, under the
// transform and opacity pushed on it. A diagram's or a chart's painting needs this, and a TextEnv.
type Drawer interface {
	// Size returns the canvas dimensions in logical pixels.
	Size() Size

	// FillRect paints a filled rectangle using brush b.
	FillRect(r Rect, b Brush)

	// FillRRect paints a filled rounded rectangle with corner radius using brush b.
	FillRRect(r Rect, radius float32, b Brush)

	// StrokeRRect strokes the outline of a rounded rectangle with specified radius and line width.
	StrokeRRect(r Rect, radius, width float32, b Brush)

	// FillEllipse paints a filled ellipse bounded by rect r.
	FillEllipse(r Rect, b Brush)

	// FillPath paints the interior of vector path p using brush b.
	FillPath(p *Path, b Brush)

	// StrokePath strokes the outline of vector path p with line width using brush b.
	StrokePath(p *Path, width float32, b Brush)

	// DrawText renders shaped text layout t with its top-left origin at position at.
	DrawText(t *TextLayout, at Point, b Brush)

	// DrawImage draws the src sub-rectangle of img into destination rectangle dst.
	// If src is the zero Rect, the entire image is drawn.
	DrawImage(img *Image, dst, src Rect)

	// PushTransform applies an affine transformation matrix (translation, scale, rotation)
	// to all subsequent drawing operations until the matching Pop.
	PushTransform(m Affine)

	// PushOpacity multiplies the alpha channel opacity by a (0.0 to 1.0) until the matching Pop.
	PushOpacity(a float32)

	// Pop restores the most recently saved transformation or opacity state.
	Pop()
}

// TextEnv is the text a canvas draws with: the window's shaper, its scale, its text size and its
// fonts.
type TextEnv interface {
	// Text returns the window's text shaper for measuring fonts and shaping layouts.
	Text() *TextShaper

	// Scale returns the display scaling factor (physical device pixels per logical pixel).
	Scale() float32

	// TextSize is the size of the window's text (the cells' font), in logical pixels: what a
	// view sizes its own text by, so it reads at the size of everything around it.
	TextSize() float32
	// Fonts are the window's families now, and their generation: a view keeping shaped text keys
	// it on Fonts().Gen, so a change of family shapes it again.
	Fonts() Fonts
}

// CellView is the grid under a view: its cells' size, colours and text, the colour around the
// view, and the cells drawn as the grid draws them. A canvas with no grid (an SVG export, a stream)
// answers zeros and draws none.
type CellView interface {
	// CellSize returns the physical dimensions of a single character cell in logical pixels.
	CellSize() Size
	// CellColors is a cell's colours as composed on screen; zero outside the grid. A cell another
	// layer covers (a raised panel, a dialog) answers that layer's colours: a view taking its own
	// colours from the cells reads one nothing covers ([CellCovered], [OpenCellColors]).
	CellColors(col, row int) (fg, bg color.NRGBA)
	CellText(col, row int) string // a cell's grapheme cluster; "" outside the grid or for a wide cell's second half
	// Backdrop is the colour around the view: the most common background of the cells bordering
	// it, or the page's where it meets the grid's edge. A rounded shape fills its rect with it
	// first, so its corners show what surrounds the widget, not the widget's own square cells.
	Backdrop() color.NRGBA
	// PaintCells draws the cells in r as the grid draws them, over what the view has drawn.
	// glyphsOnly leaves out their backgrounds, so the view's own fill shows behind the text.
	PaintCells(r CellRect, glyphsOnly bool)
}

// WindowChrome is what a view marks of the window itself.
type WindowChrome interface {
	// WindowMoveArea designates rect r as an interactive handle that drags the OS window.
	WindowMoveArea(r Rect)
}

// CellCoverer is a [Canvas] that knows which of its view's cells something painted after the view
// covers: a dialog, or a floating panel raised over it. The host draws those cells over the view
// afterwards, so their colours are the covering layer's, not the view's. A Canvas need not
// implement it; [CellCovered] asks.
type CellCoverer interface {
	// CellCovered reports whether the view's cell at (col, row) is covered by a later layer.
	CellCovered(col, row int) bool
}

// CellCovered reports whether c's cell at (col, row) is covered by something painted after the
// view; false where c cannot tell (it is not a [CellCoverer]).
func CellCovered(c Canvas, col, row int) bool {
	cc, ok := c.(CellCoverer)
	return ok && cc.CellCovered(col, row)
}

// Palette is a [Canvas] that draws in a window's theme: Indexed is that theme's colour for a
// 256-colour index ([Theme.Indexed]). A view resolving a palette colour asks [PaletteOf], so a
// theme set with WithTheme colours the view as it colours the cells beside it.
type Palette interface {
	Indexed(i uint8) color.NRGBA
}

// PaletteOf is the palette c draws with: c's own when it is a [Palette], else the default theme's.
func PaletteOf(c Canvas) Palette {
	if p, ok := c.(Palette); ok {
		return p
	}
	return &defaultPalette
}

// defaultPalette is DefaultTheme, for a canvas that knows no window's theme.
var defaultPalette = DefaultTheme()

// OpenCellColors is the colours of the first cell of r, row by row, that nothing covers, and
// whether r has one. A view takes its colours from it rather than from a fixed cell, which a
// raised panel may cover: the panel's colours would become the view's. With none open the view is
// wholly covered where r lies, and the host paints the covering cells over whatever it draws.
func OpenCellColors(c Canvas, r CellRect) (fg, bg color.NRGBA, ok bool) {
	for row := r.Y; row < r.Y+r.H; row++ {
		for col := r.X; col < r.X+r.W; col++ {
			if !CellCovered(c, col, row) {
				fg, bg = c.CellColors(col, row)
				return fg, bg, true
			}
		}
	}
	return color.NRGBA{}, color.NRGBA{}, false
}

// Image is a decoded image a Canvas can draw.
type Image struct {
	op   paint.ImageOp
	size image.Point
	src  image.Image // as given: what an SVGCanvas writes as a data: URI
}

// NewImage prepares img for drawing. Make it once and keep it: preparing uploads it again.
func NewImage(img image.Image) *Image {
	return &Image{op: paint.NewImageOp(img), size: img.Bounds().Size(), src: img}
}

// Size is the image's size in pixels.
func (i *Image) Size() Size { return Size{W: float32(i.size.X), H: float32(i.size.Y)} }

// gioCanvas writes a view's drawing into the frame's ops. Every draw pushes the canvas's clip (in
// device pixels, so no transform moves it), the opacity, then one transform that takes logical
// coordinates to the device: the canvas's origin and scale after the view's own transforms.
type gioCanvas struct {
	ops    *op.Ops
	r      *renderer
	g      *grid
	m      metrics
	origin f32.Point       // device pixels of (0, 0)
	size   Size            // logical
	clip   image.Rectangle // device pixels
	cellX  int             // the view's top-left cell
	cellY  int
	view   CellRect // the view's cells, for Backdrop

	covered []tui.Rect // grid cells inside the view painted after it (NativePlacement.Covered)

	paintState
}

// paintState is a canvas's transform and opacity, and the stack its Pushes save them on: the
// same for the window's canvas and an SVGCanvas.
type paintState struct {
	xform Affine  // the Pushes' transforms, composed; the zero Affine is none
	alpha float32 // the Pushes' opacities, multiplied, and its parent's when it was made
	stack []savedPaint
}

// savedPaint is what a Push saves and its Pop restores.
type savedPaint struct {
	xform Affine
	alpha float32
}

// opaque is the state of a new canvas: no transform, full opacity.
func opaque() paintState { return paintState{alpha: 1} }

// within is the state of a sub-canvas made from p: its opacity, no transform of its own.
func (p *paintState) within() paintState { return paintState{alpha: p.alpha} }

// PushTransform applies m to what the canvas draws until the matching Pop.
func (p *paintState) PushTransform(m Affine) {
	p.stack = append(p.stack, savedPaint{p.xform, p.alpha})
	p.xform = p.xform.Mul(m)
}

// PushOpacity multiplies what the canvas draws until the matching Pop by a, clamped to [0, 1].
func (p *paintState) PushOpacity(a float32) {
	p.stack = append(p.stack, savedPaint{p.xform, p.alpha})
	p.alpha *= min(max(a, 0), 1)
}

// Pop restores the state the latest Push saved; with none, it does nothing.
func (p *paintState) Pop() {
	if n := len(p.stack); n > 0 {
		p.xform, p.alpha = p.stack[n-1].xform, p.stack[n-1].alpha
		p.stack = p.stack[:n-1]
	}
}

// viewOrigin is the device origin of a view placed at cell (x, y), whose top-left is at in device
// pixels, and whose own rect is full when only part of it shows (NativePlacement.Full): full's
// top-left then, so the view lays out as it would unclipped. The canvas and the IME caret both
// place the view by it.
func (m metrics) viewOrigin(at image.Point, x, y int, full tui.Rect) image.Point {
	if full.Empty() {
		return at
	}
	return at.Add(image.Pt((full.X-x)*m.cell.X, (full.Y-y)*m.cell.Y))
}

// newGioCanvas is the canvas over the cells of rect r, origin at its top-left cell; covered are the
// grid cells inside it that are painted after it.
//
// In a Panes window the rect is anchored (metrics.anchored): a view inside one band is clipped to
// its pane, so a Pixels pane's canvas is exactly the pane; one crossing bands is an overlay.
//
// full is the view's whole rect when only part of it shows (NativePlacement.Full: a ScrollView's
// child half scrolled out), zero otherwise. Then the origin and the logical size are full's, so the
// view lays out as it would unclipped, and the clip is the part that shows, cells: three things kept
// apart. full is anchored through the same pane as cells.
func newGioCanvas(ops *op.Ops, r *renderer, g *grid, m metrics, cells CellRect, full tui.Rect, covered []tui.Rect) *gioCanvas {
	dev, within, _ := m.anchored(tui.Rect{X: cells.X, Y: cells.Y, W: cells.W, H: cells.H})
	clip := dev
	if m.layout != nil {
		clip = dev.Intersect(within)
	}
	// The origin stays the placement's own top-left even when its pane cuts it: a pointer at a
	// device point reconstructs the same local position the canvas draws at. The size runs from
	// that origin to the visible edge, so a Pixels pane's canvas is exactly its pane.
	origin := m.viewOrigin(dev.Min, cells.X, cells.Y, full)
	size := image.Pt(max(clip.Max.X-dev.Min.X, 0), max(clip.Max.Y-dev.Min.Y, 0))
	cellX, cellY := cells.X, cells.Y
	if !full.Empty() {
		size = image.Pt(full.W*m.cell.X, full.H*m.cell.Y)
		cellX, cellY = full.X, full.Y
	}
	if clip.Empty() {
		size = image.Point{}
	}
	return &gioCanvas{
		ops: ops, r: r, g: g, m: m,
		origin: f32.Pt(float32(origin.X), float32(origin.Y)),
		size:   Size{W: float32(size.X) / m.scale, H: float32(size.Y) / m.scale},
		clip:   clip,
		cellX:  cellX, cellY: cellY, view: cells,
		covered:    covered,
		paintState: opaque(),
	}
}

// Sub creates a child sub-canvas clipped to the specified rectangle.
func (c *gioCanvas) Sub(r Rect) Canvas {
	o := f32.Pt(c.origin.X+r.X*c.m.scale, c.origin.Y+r.Y*c.m.scale)
	dev := image.Rect(
		int(math.Floor(float64(o.X))), int(math.Floor(float64(o.Y))),
		int(math.Ceil(float64(o.X+r.W*c.m.scale))), int(math.Ceil(float64(o.Y+r.H*c.m.scale))),
	).Intersect(c.clip)
	return &gioCanvas{
		ops: c.ops, r: c.r, g: c.g, m: c.m,
		origin: o, size: Size{W: r.W, H: r.H}, clip: dev,
		cellX: c.cellX, cellY: c.cellY, view: c.view,
		covered:    c.covered,
		paintState: c.within(),
	}
}

// Size returns the logical dimensions of the canvas.
func (c *gioCanvas) Size() Size { return c.size }

// Scale returns the display DPI scale factor.
func (c *gioCanvas) Scale() float32 { return c.m.scale }

// Text returns a text shaper configured for the canvas's scale and font registry.
func (c *gioCanvas) Text() *TextShaper {
	return &TextShaper{s: c.r.shaper, paths: c.r.paths, scale: c.m.scale, fonts: c.r.fonts}
}

// Fonts returns the canvas font family registry.
func (c *gioCanvas) Fonts() Fonts { return c.r.fonts }

// TextSize returns the base monospace font size in logical pixels.
func (c *gioCanvas) TextSize() float32 { return c.m.ppem / c.m.scale }

// CellSize returns the logical size of a single character cell.
func (c *gioCanvas) CellSize() Size {
	return Size{W: float32(c.m.cell.X) / c.m.scale, H: float32(c.m.cell.Y) / c.m.scale}
}

// paintPush is what a draw pushes on its ops, in this order: the clip, the opacity below full
// alpha, and a transform. pop undoes them. It is a value, so a frame's draw allocates nothing.
type paintPush struct {
	cl          clip.Stack
	opacity     paint.OpacityStack
	tr          op.TransformStack
	faded, xfmd bool
}

// pushPaint pushes the clip r and, below full alpha, the opacity alpha.
func pushPaint(ops *op.Ops, r image.Rectangle, alpha float32) paintPush {
	p := paintPush{cl: clip.Rect(r).Push(ops), faded: alpha < 1}
	countClip()
	if p.faded {
		countTransform()
		p.opacity = paint.PushOpacity(ops, alpha)
	}
	return p
}

// transform pushes m after the clip and the opacity.
func (p *paintPush) transform(ops *op.Ops, m f32.Affine2D) {
	countTransform()
	p.tr, p.xfmd = op.Affine(m).Push(ops), true
}

// pop undoes the pushes, the last first.
func (p paintPush) pop() {
	if p.xfmd {
		p.tr.Pop()
	}
	if p.faded {
		p.opacity.Pop()
	}
	p.cl.Pop()
}

// begin pushes the clip, the opacity and the logical-to-device transform; end pops them.
func (c *gioCanvas) begin() (end func()) {
	p := pushPaint(c.ops, c.clip, c.alpha)
	s := c.m.scale
	base := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(c.origin)
	p.transform(c.ops, base.Mul(c.xform))
	return p.pop
}

// fill paints the current clip with b.
func (c *gioCanvas) fill(b Brush) {
	if g := b.Gradient; g != nil {
		paint.LinearGradientOp{Stop1: fp(g.From), Color1: g.FromColor, Stop2: fp(g.To), Color2: g.ToColor}.Add(c.ops)
	} else {
		paint.ColorOp{Color: b.Color}.Add(c.ops)
	}
	paint.PaintOp{}.Add(c.ops)
}

// FillPath rasterizes a vector path filled with the given brush.
func (c *gioCanvas) FillPath(p *Path, b Brush) {
	end := c.begin()
	st := clip.Outline{Path: p.spec(c.ops)}.Op().Push(c.ops)
	countClip()
	countFill()
	c.fill(b)
	st.Pop()
	end()
}

// StrokePath strokes the outline of a vector path with the specified width and brush.
func (c *gioCanvas) StrokePath(p *Path, width float32, b Brush) {
	end := c.begin()
	st := clip.Stroke{Path: p.spec(c.ops), Width: width}.Op().Push(c.ops)
	countClip()
	countFill()
	c.fill(b)
	st.Pop()
	end()
}

// FillRect fills an unrounded rectangle with the given brush. A rectangle the canvas's transform
// maps onto whole device pixels, without rotation or skew, is filled as one clip of those pixels:
// the path's shape, at a fraction of the work (no path to build and no stencil for gio). Its edge
// pixels may differ from the path's by a rasterizer's rounding of edge coverage, a level or two
// (measured: at most 2 of 255). Any other rectangle, a fractional edge's antialiasing included, is
// filled as the path it was.
func (c *gioCanvas) FillRect(r Rect, b Brush) {
	dev, ok := c.pixelRect(r)
	if !ok {
		c.FillPath(rrectPath(r, 0), b)
		return
	}
	p := pushPaint(c.ops, dev.Intersect(c.clip), c.alpha)
	if b.Gradient != nil { // its stops are in the canvas's coordinates
		s := c.m.scale
		p.transform(c.ops, f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(c.origin).Mul(c.xform))
	}
	countFill()
	c.fill(b)
	p.pop()
}

// pixelRect is r in device pixels when the canvas's transform maps it exactly onto whole pixels,
// with no rotation or skew; ok is false otherwise, and for an empty r.
func (c *gioCanvas) pixelRect(r Rect) (image.Rectangle, bool) {
	if r.W <= 0 || r.H <= 0 {
		return image.Rectangle{}, false
	}
	s := c.m.scale
	m := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(c.origin).Mul(c.xform)
	if _, hx, _, hy, _, _ := m.Elems(); hx != 0 || hy != 0 {
		return image.Rectangle{}, false
	}
	a, b := m.Transform(f32.Pt(r.X, r.Y)), m.Transform(f32.Pt(r.X+r.W, r.Y+r.H))
	var px [4]int
	for i, v := range [4]float32{min(a.X, b.X), min(a.Y, b.Y), max(a.X, b.X), max(a.Y, b.Y)} {
		n := float32(math.Round(float64(v)))
		if d := v - n; d > 1.0/256 || d < -1.0/256 {
			return image.Rectangle{}, false
		}
		px[i] = int(n)
	}
	return image.Rect(px[0], px[1], px[2], px[3]), true
}

// FillRRect fills a rounded rectangle with the specified corner radius and brush.
func (c *gioCanvas) FillRRect(r Rect, radius float32, b Brush) { c.FillPath(rrectPath(r, radius), b) }

// FillEllipse fills an ellipse inscribed in the bounding rectangle with the brush.
func (c *gioCanvas) FillEllipse(r Rect, b Brush) { c.FillPath(ellipsePath(r), b) }

// StrokeRRect strokes the perimeter of a rounded rectangle with corner radius, stroke width, and brush.
func (c *gioCanvas) StrokeRRect(r Rect, radius, width float32, b Brush) {
	c.StrokePath(rrectPath(r, radius), width, b)
}

// DrawText draws shaped text layout glyphs starting at the given canvas point.
func (c *gioCanvas) DrawText(t *TextLayout, at Point, b Brush) {
	if t == nil || len(t.lines) == 0 {
		return
	}
	end := c.begin()
	// The glyphs are shaped in device pixels: undo the scale, from the first line's dot.
	inv := 1 / t.scale
	dot := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(inv, inv)).Offset(f32.Pt(at.X, at.Y+t.Ascent))).Push(c.ops)
	countTransform()
	for _, ln := range t.lines {
		countTransform()
		countGlyphPath()
		off := op.Affine(f32.Affine2D{}.Offset(f32.Pt(ln.x, ln.y))).Push(c.ops)
		st := clip.Outline{Path: ln.path}.Op().Push(c.ops)
		c.fill(b)
		st.Pop()
		ln.bitmaps.Add(c.ops)
		off.Pop()
	}
	dot.Pop()
	end()
}

// DrawImage renders a sub-rectangle of an image scaled into the destination rectangle.
func (c *gioCanvas) DrawImage(img *Image, dst, src Rect) {
	if img == nil || dst.W <= 0 || dst.H <= 0 {
		return
	}
	if src.W <= 0 || src.H <= 0 {
		src = Rect{W: float32(img.size.X), H: float32(img.size.Y)}
	}
	// dst on whole device pixels clips as those pixels, as FillRect does; any other dst clips as
	// its path, under the canvas's transform
	if dev, ok := c.pixelRect(dst); ok {
		p := pushPaint(c.ops, dev.Intersect(c.clip), c.alpha)
		s := c.m.scale
		p.transform(c.ops, f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(c.origin).Mul(c.xform))
		c.paintImage(img, src, dst)
		p.pop()
		return
	}
	end := c.begin()
	countClip()
	st := clip.Outline{Path: rrectPath(dst, 0).spec(c.ops)}.Op().Push(c.ops)
	c.paintImage(img, src, dst)
	st.Pop()
	end()
}

// paintImage paints img's src onto dst under the canvas's transform, inside the clip pushed.
func (c *gioCanvas) paintImage(img *Image, src, dst Rect) {
	countTransform()
	countImage()
	tr := op.Affine(imageAffine(src, dst)).Push(c.ops)
	img.op.Add(c.ops)
	paint.PaintOp{}.Add(c.ops)
	tr.Pop()
}

// imageAffine maps an image's src onto dst: src's corner to dst's, scaled to its size.
func imageAffine(src, dst Rect) f32.Affine2D {
	return f32.Affine2D{}.
		Offset(f32.Pt(-src.X, -src.Y)).
		Scale(f32.Point{}, f32.Pt(dst.W/src.W, dst.H/src.H)).
		Offset(f32.Pt(dst.X, dst.Y))
}

// drawStream records a slot for s, if this canvas is drawing its window's frame.
func (c *gioCanvas) drawStream(s *stream, dst Rect) bool {
	if s.b == nil || s.b.render != c.r || !c.r.cutter.recording(c.ops) {
		return false
	}
	scale := c.m.scale
	base := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(scale, scale)).Offset(c.origin)
	c.r.cutter.cut(streamSlot{s: s, clip: c.clip, alpha: c.alpha, xform: base.Mul(c.xform), dst: dst})
	return true
}

// WindowMoveArea registers an OS window drag-move interactive hit area.
func (c *gioCanvas) WindowMoveArea(r Rect) {
	s := c.m.scale
	dev := image.Rect(
		int(c.origin.X+r.X*s), int(c.origin.Y+r.Y*s),
		int(c.origin.X+(r.X+r.W)*s), int(c.origin.Y+(r.Y+r.H)*s),
	).Intersect(c.clip)
	st := clip.Rect(dev).Push(c.ops)
	countClip()
	system.ActionInputOp(system.ActionMove).Add(c.ops)
	st.Pop()
}

// Indexed is the window theme's colour for 256-colour index i (Palette).
func (c *gioCanvas) Indexed(i uint8) color.NRGBA { return c.r.theme.Indexed(i) }

// CellColors returns the foreground and background colors of the cell at grid (col, row).
func (c *gioCanvas) CellColors(col, row int) (fg, bg color.NRGBA) {
	x, y := c.cellX+col, c.cellY+row
	if c.g == nil || x < 0 || y < 0 || x >= c.g.w || y >= c.g.h {
		return color.NRGBA{}, color.NRGBA{}
	}
	return c.r.colors(c.g.at(x, y).Attrs)
}

// CellCovered reports whether the cell at grid (col, row) is one painted after the view.
func (c *gioCanvas) CellCovered(col, row int) bool {
	x, y := c.cellX+col, c.cellY+row
	for _, r := range c.covered {
		if r.Contains(x, y) {
			return true
		}
	}
	return false
}

// Backdrop returns the default background color of the canvas's host view.
func (c *gioCanvas) Backdrop() color.NRGBA {
	if c.g == nil {
		return color.NRGBA{} // a canvas stream's: no cells behind it
	}
	return c.r.backdrop(c.g, c.view)
}

// CellText returns the character content of the cell at grid (col, row).
func (c *gioCanvas) CellText(col, row int) string {
	x, y := c.cellX+col, c.cellY+row
	if c.g == nil || x < 0 || y < 0 || x >= c.g.w || y >= c.g.h {
		return ""
	}
	return c.g.at(x, y).Content
}

// PaintCells rasterizes a rectangle of character cells onto the canvas.
func (c *gioCanvas) PaintCells(r CellRect, glyphsOnly bool) {
	if c.g == nil {
		return // a canvas stream's: no cells to paint
	}
	p := pushPaint(c.ops, c.clip, c.alpha)
	c.r.paintCells(c.ops, c.g, CellRect{X: c.cellX + r.X, Y: c.cellY + r.Y, W: r.W, H: r.H}, glyphsOnly)
	p.pop()
}
