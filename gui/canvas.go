package gui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// View is the pixel side of a native widget. The backend calls Paint on tui's loop goroutine,
// inside Flush, so a view reads its component's state with the component's one owner.
type View interface {
	Paint(c Canvas) // c is the component's cell rect in logical px, (0,0) at its top-left
}

// ViewFunc is a View that is a function.
type ViewFunc func(c Canvas)

// Paint calls f.
func (f ViewFunc) Paint(c Canvas) { f(c) }

// Canvas is the pixel twin of tui.Surface: pre-clipped, (0,0) at the widget's top-left, in
// logical pixels.
type Canvas interface {
	// Sub is the part of the canvas at r, clipped to it, with (0,0) at r's top-left. It starts
	// with no transform of its own; r is in this canvas's untransformed coordinates.
	Sub(r Rect) Canvas
	Size() Size

	FillRect(r Rect, b Brush)
	FillRRect(r Rect, radius float32, b Brush)
	StrokeRRect(r Rect, radius, width float32, b Brush)
	FillEllipse(r Rect, b Brush)
	FillPath(p *Path, b Brush)
	StrokePath(p *Path, width float32, b Brush)
	// DrawText draws t with the top-left of its first line at at.
	DrawText(t *TextLayout, at Point, b Brush)
	// DrawImage draws the src part of img (image pixels; the zero Rect is all of it) into dst.
	DrawImage(img *Image, dst, src Rect)

	PushTransform(m Affine) // applied to what is drawn until the matching Pop
	PushOpacity(a float32)  // multiplies the opacity until the matching Pop
	Pop()

	// WindowMoveArea makes r a handle that moves the window when dragged.
	WindowMoveArea(r Rect)

	Text() *TextShaper // the window's shaper: measure with this, never another
	Scale() float32    // device pixels per logical pixel

	// The cells under the view, as tui painted them: a native style that changes the look and not
	// the geometry draws its text and marks at these cells. Column and row are the view's own
	// (0, 0 at its top-left cell), whatever Sub or transform is current.

	CellSize() Size // one cell, in logical pixels
	// TextSize is the size of the window's text (the cells' font), in logical pixels: what a
	// view sizes its own text by, so it reads at the size of everything around it.
	TextSize() float32
	// Fonts are the window's families now, and their generation: a view keeping shaped text keys
	// it on Fonts().Gen, so a change of family shapes it again.
	Fonts() Fonts
	CellColors(col, row int) (fg, bg color.NRGBA) // a cell's colours as drawn; zero outside the grid
	CellText(col, row int) string                 // a cell's grapheme cluster; "" outside the grid or for a wide cell's second half
	// Backdrop is the colour around the view: the most common background of the cells bordering
	// it, or the page's where it meets the grid's edge. A rounded shape fills its rect with it
	// first, so its corners show what surrounds the widget, not the widget's own square cells.
	Backdrop() color.NRGBA
	// PaintCells draws the cells in r as the grid draws them, over what the view has drawn.
	// glyphsOnly leaves out their backgrounds, so the view's own fill shows behind the text.
	PaintCells(r CellRect, glyphsOnly bool)
}

// Image is a decoded image a Canvas can draw.
type Image struct {
	op   paint.ImageOp
	size image.Point
}

// NewImage prepares img for drawing. Make it once and keep it: preparing uploads it again.
func NewImage(img image.Image) *Image {
	return &Image{op: paint.NewImageOp(img), size: img.Bounds().Size()}
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

	xform Affine
	alpha float32
	stack []canvasState
}

type canvasState struct {
	xform Affine
	alpha float32
}

// newGioCanvas is the canvas over the cells of rect r, origin at its top-left cell.
func newGioCanvas(ops *op.Ops, r *renderer, g *grid, m metrics, cells CellRect) *gioCanvas {
	dev := m.cellRect(cells.X, cells.Y, cells.W, cells.H)
	return &gioCanvas{
		ops: ops, r: r, g: g, m: m,
		origin: f32.Pt(float32(dev.Min.X), float32(dev.Min.Y)),
		size:   Size{W: float32(dev.Dx()) / m.scale, H: float32(dev.Dy()) / m.scale},
		clip:   dev,
		cellX:  cells.X, cellY: cells.Y, view: cells,
		alpha: 1,
	}
}

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
		alpha: c.alpha,
	}
}

func (c *gioCanvas) Size() Size     { return c.size }
func (c *gioCanvas) Scale() float32 { return c.m.scale }
func (c *gioCanvas) Text() *TextShaper {
	return &TextShaper{s: c.r.shaper, scale: c.m.scale, fonts: c.r.fonts}
}
func (c *gioCanvas) Fonts() Fonts      { return c.r.fonts }
func (c *gioCanvas) TextSize() float32 { return c.m.ppem / c.m.scale }

func (c *gioCanvas) CellSize() Size {
	return Size{W: float32(c.m.cell.X) / c.m.scale, H: float32(c.m.cell.Y) / c.m.scale}
}

func (c *gioCanvas) PushTransform(m Affine) {
	c.stack = append(c.stack, canvasState{c.xform, c.alpha})
	c.xform = c.xform.Mul(m)
}

func (c *gioCanvas) PushOpacity(a float32) {
	c.stack = append(c.stack, canvasState{c.xform, c.alpha})
	c.alpha *= min(max(a, 0), 1)
}

func (c *gioCanvas) Pop() {
	if n := len(c.stack); n > 0 {
		c.xform, c.alpha = c.stack[n-1].xform, c.stack[n-1].alpha
		c.stack = c.stack[:n-1]
	}
}

// begin pushes the clip, the opacity and the logical-to-device transform; end pops them.
func (c *gioCanvas) begin() (end func()) {
	cl := clip.Rect(c.clip).Push(c.ops)
	var opacity paint.OpacityStack
	faded := c.alpha < 1
	if faded {
		opacity = paint.PushOpacity(c.ops, c.alpha)
	}
	s := c.m.scale
	base := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(c.origin)
	tr := op.Affine(base.Mul(c.xform)).Push(c.ops)
	return func() {
		tr.Pop()
		if faded {
			opacity.Pop()
		}
		cl.Pop()
	}
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

func (c *gioCanvas) FillPath(p *Path, b Brush) {
	end := c.begin()
	st := clip.Outline{Path: p.spec(c.ops)}.Op().Push(c.ops)
	c.fill(b)
	st.Pop()
	end()
}

func (c *gioCanvas) StrokePath(p *Path, width float32, b Brush) {
	end := c.begin()
	st := clip.Stroke{Path: p.spec(c.ops), Width: width}.Op().Push(c.ops)
	c.fill(b)
	st.Pop()
	end()
}

func (c *gioCanvas) FillRect(r Rect, b Brush)                  { c.FillPath(rrectPath(r, 0), b) }
func (c *gioCanvas) FillRRect(r Rect, radius float32, b Brush) { c.FillPath(rrectPath(r, radius), b) }
func (c *gioCanvas) FillEllipse(r Rect, b Brush)               { c.FillPath(ellipsePath(r), b) }
func (c *gioCanvas) StrokeRRect(r Rect, radius, width float32, b Brush) {
	c.StrokePath(rrectPath(r, radius), width, b)
}

func (c *gioCanvas) DrawText(t *TextLayout, at Point, b Brush) {
	if t == nil || len(t.lines) == 0 {
		return
	}
	end := c.begin()
	// The glyphs are shaped in device pixels: undo the scale, from the first line's dot.
	inv := 1 / t.scale
	dot := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(inv, inv)).Offset(f32.Pt(at.X, at.Y+t.Ascent))).Push(c.ops)
	for _, ln := range t.lines {
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

func (c *gioCanvas) DrawImage(img *Image, dst, src Rect) {
	if img == nil || dst.W <= 0 || dst.H <= 0 {
		return
	}
	if src.W <= 0 || src.H <= 0 {
		src = Rect{W: float32(img.size.X), H: float32(img.size.Y)}
	}
	end := c.begin()
	st := clip.Outline{Path: rrectPath(dst, 0).spec(c.ops)}.Op().Push(c.ops)
	tr := op.Affine(f32.Affine2D{}.
		Offset(f32.Pt(-src.X, -src.Y)).
		Scale(f32.Point{}, f32.Pt(dst.W/src.W, dst.H/src.H)).
		Offset(f32.Pt(dst.X, dst.Y))).Push(c.ops)
	img.op.Add(c.ops)
	paint.PaintOp{}.Add(c.ops)
	tr.Pop()
	st.Pop()
	end()
}

func (c *gioCanvas) WindowMoveArea(r Rect) {
	s := c.m.scale
	dev := image.Rect(
		int(c.origin.X+r.X*s), int(c.origin.Y+r.Y*s),
		int(c.origin.X+(r.X+r.W)*s), int(c.origin.Y+(r.Y+r.H)*s),
	).Intersect(c.clip)
	st := clip.Rect(dev).Push(c.ops)
	system.ActionInputOp(system.ActionMove).Add(c.ops)
	st.Pop()
}

func (c *gioCanvas) CellColors(col, row int) (fg, bg color.NRGBA) {
	x, y := c.cellX+col, c.cellY+row
	if x < 0 || y < 0 || x >= c.g.w || y >= c.g.h {
		return color.NRGBA{}, color.NRGBA{}
	}
	return c.r.colors(c.g.at(x, y).Attrs)
}

func (c *gioCanvas) Backdrop() color.NRGBA {
	return c.r.backdrop(c.g, c.view)
}

func (c *gioCanvas) CellText(col, row int) string {
	x, y := c.cellX+col, c.cellY+row
	if x < 0 || y < 0 || x >= c.g.w || y >= c.g.h {
		return ""
	}
	return c.g.at(x, y).Content
}

func (c *gioCanvas) PaintCells(r CellRect, glyphsOnly bool) {
	cl := clip.Rect(c.clip).Push(c.ops)
	var opacity paint.OpacityStack
	faded := c.alpha < 1
	if faded {
		opacity = paint.PushOpacity(c.ops, c.alpha)
	}
	c.r.paintCells(c.ops, c.g, CellRect{X: c.cellX + r.X, Y: c.cellY + r.Y, W: r.W, H: r.H}, glyphsOnly)
	if faded {
		opacity.Pop()
	}
	cl.Pop()
}
