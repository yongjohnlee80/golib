package gui

import (
	"image/color"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/text"
)

// Test Recording Architecture for Canvas:
//
//	+------------------+     Draw Calls      +--------------------+
//	| Custom View /    | ------------------> | RecordingCanvas    |
//	| Embedded Widget  |                     | (Root or Sub)      |
//	+------------------+                     +--------------------+
//	                                                   |
//	                                                   v
//	                                         +--------------------+
//	                                         | Calls []PaintCall  |
//	                                         | (Inspectable trace |
//	                                         |  for Unit Tests)   |
//	                                         +--------------------+
//
// PaintCall records an individual drawing command received by a [RecordingCanvas].
// Rect is specified in root canvas coordinates (with any sub-canvas offsets pre-applied), as the
// view gave it. Drawn is where a window draws it: the transforms pushed (a fitted diagram's
// scale, a turned label) applied too, a turned rect's bounding box; Alpha is the opacity it is
// drawn at, the pushed opacities multiplied.
type PaintCall struct {
	Op         string // Name of the operation: "FillRect", "FillRRect", "StrokeRRect", "DrawText", etc.
	Rect       Rect
	Radius     float32
	Width      float32 // Stroke width
	Brush      Brush
	Text       *TextLayout
	Cells      CellRect // Grid cells specified in PaintCells
	GlyphsOnly bool
	Opacity    float32
	Drawn      Rect
	Alpha      float32
}

// RecordingCanvas implements [Canvas] by recording all invoked operations into an inspectable
// slice of [PaintCall] values. It allows native vector views and UI widgets to be rigorously
// tested in headless unit tests without requiring a window or GPU environment.
type RecordingCanvas struct {
	Calls []PaintCall
	// Colors answers CellColors; nil answers zero colours.
	Colors func(col, row int) (fg, bg color.NRGBA)
	// Content answers CellText; nil answers "".
	Content func(col, row int) string
	// Covers answers CellCovered: the cells a later layer covers. nil covers none.
	Covers func(col, row int) bool
	// Around answers Backdrop.
	Around color.NRGBA
	// TextPx answers TextSize; 0: the cell's height over the default font's line height.
	TextPx float32
	// Theme answers Indexed (Palette); nil: the default theme's.
	Theme *Theme

	root   *RecordingCanvas
	off    Point
	size   Size
	cell   Size
	shaper *TextShaper
	ps     paintState // the transform and opacity pushed, as a window's canvas keeps them
}

// NewRecordingCanvas is a canvas of size, over cells of cell size, at scale 1.
func NewRecordingCanvas(size, cell Size) *RecordingCanvas {
	rc := &RecordingCanvas{size: size, cell: cell,
		shaper: &TextShaper{s: text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), scale: 1,
			fonts: defaultFonts()}, ps: opaque()}
	rc.root = rc
	return rc
}

// rec records a paint call into the root log with the current offset applied, and where and at
// what opacity a window draws it.
func (c *RecordingCanvas) rec(p PaintCall) {
	p.Drawn = c.drawn(p.Rect)
	p.Alpha = c.ps.alpha
	p.Rect.X += c.off.X
	p.Rect.Y += c.off.Y
	c.root.Calls = append(c.root.Calls, p)
}

// drawn is r as a window draws it: through the pushed transform, then the sub-canvas offset; the
// bounding box of its corners when the transform turns it.
func (c *RecordingCanvas) drawn(r Rect) Rect {
	m := c.ps.xform
	pts := [4]f32.Point{m.Transform(f32.Pt(r.X, r.Y)), m.Transform(f32.Pt(r.X+r.W, r.Y)),
		m.Transform(f32.Pt(r.X, r.Y+r.H)), m.Transform(f32.Pt(r.X+r.W, r.Y+r.H))}
	x0, y0, x1, y1 := pts[0].X, pts[0].Y, pts[0].X, pts[0].Y
	for _, q := range pts[1:] {
		x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
	}
	return Rect{X: x0 + c.off.X, Y: y0 + c.off.Y, W: x1 - x0, H: y1 - y0}
}

// Sub records into the same log, offset by r.
func (c *RecordingCanvas) Sub(r Rect) Canvas {
	return &RecordingCanvas{root: c.root, off: Pt(c.off.X+r.X, c.off.Y+r.Y), size: r.Size(), cell: c.cell, shaper: c.shaper, Colors: c.Colors, Content: c.Content, Covers: c.Covers, Around: c.Around, ps: c.ps.within()}
}

// Size returns the logical bounds of the recording canvas.
func (c *RecordingCanvas) Size() Size { return c.size }

// Scale returns the display scale factor (fixed at 1 for test recordings).
func (c *RecordingCanvas) Scale() float32 { return 1 }

// Text returns the test text shaper.
func (c *RecordingCanvas) Text() *TextShaper { return c.shaper }

// Fonts are the recording's families: the defaults, unless SetFonts set others.
func (c *RecordingCanvas) Fonts() Fonts { return c.root.shaper.fonts }

// SetFonts gives the recording other families, as a window's change of font would: views painted
// on it afterwards shape in them and see the new generation.
func (c *RecordingCanvas) SetFonts(f Fonts) { c.root.shaper.fonts = f }

// CellSize returns the character cell dimensions.
func (c *RecordingCanvas) CellSize() Size { return c.cell }

// TextSize answers TextPx when set, else the cell's height at the line height the default cell
// font gives (about 1.2 times its size).
func (c *RecordingCanvas) TextSize() float32 {
	if c.root.TextPx > 0 {
		return c.root.TextPx
	}
	return c.cell.H / LineHeight
}

// FillRect records a filled rectangle operation.
func (c *RecordingCanvas) FillRect(r Rect, b Brush) {
	c.rec(PaintCall{Op: "FillRect", Rect: r, Brush: b})
}

// FillRRect records a filled rounded rectangle operation.
func (c *RecordingCanvas) FillRRect(r Rect, radius float32, b Brush) {
	c.rec(PaintCall{Op: "FillRRect", Rect: r, Radius: radius, Brush: b})
}

// StrokeRRect records a stroked rounded rectangle operation.
func (c *RecordingCanvas) StrokeRRect(r Rect, radius, width float32, b Brush) {
	c.rec(PaintCall{Op: "StrokeRRect", Rect: r, Radius: radius, Width: width, Brush: b})
}

// FillEllipse records a filled ellipse operation.
func (c *RecordingCanvas) FillEllipse(r Rect, b Brush) {
	c.rec(PaintCall{Op: "FillEllipse", Rect: r, Brush: b})
}

// FillPath records a filled vector path operation, its Rect the path's bounds.
func (c *RecordingCanvas) FillPath(p *Path, b Brush) {
	var r Rect
	if p != nil {
		r = p.Bounds()
	}
	c.rec(PaintCall{Op: "FillPath", Rect: r, Brush: b})
}

// StrokePath records a stroked vector path operation, its Rect the path's bounds.
func (c *RecordingCanvas) StrokePath(p *Path, width float32, b Brush) {
	var r Rect
	if p != nil {
		r = p.Bounds()
	}
	c.rec(PaintCall{Op: "StrokePath", Rect: r, Width: width, Brush: b})
}

// DrawText records a text draw operation.
func (c *RecordingCanvas) DrawText(t *TextLayout, at Point, b Brush) {
	var w, h float32
	if t != nil {
		w, h = t.Width, t.Height
	}
	c.rec(PaintCall{Op: "DrawText", Rect: Rect{X: at.X, Y: at.Y, W: w, H: h}, Text: t, Brush: b})
}

// DrawImage records an image draw operation.
func (c *RecordingCanvas) DrawImage(_ *Image, dst, _ Rect) {
	c.rec(PaintCall{Op: "DrawImage", Rect: dst})
}

// PushTransform records pushing an affine transform, and applies it to what is drawn after it
// (Drawn) until the matching Pop.
func (c *RecordingCanvas) PushTransform(m Affine) {
	c.rec(PaintCall{Op: "PushTransform"})
	c.ps.PushTransform(m)
}

// PushOpacity records pushing an opacity layer, and applies it (Alpha) until the matching Pop.
func (c *RecordingCanvas) PushOpacity(a float32) {
	c.rec(PaintCall{Op: "PushOpacity", Opacity: a})
	c.ps.PushOpacity(a)
}

// Pop records popping the top canvas layer, and restores the state its Push saved.
func (c *RecordingCanvas) Pop() {
	c.rec(PaintCall{Op: "Pop"})
	c.ps.Pop()
}

// WindowMoveArea records registering a window drag area.
func (c *RecordingCanvas) WindowMoveArea(r Rect) { c.rec(PaintCall{Op: "WindowMoveArea", Rect: r}) }

// CellColors returns the simulated cell foreground and background colors.
func (c *RecordingCanvas) CellColors(col, row int) (fg, bg color.NRGBA) {
	if c.Colors == nil {
		return color.NRGBA{}, color.NRGBA{}
	}
	return c.Colors(col, row)
}

// CellCovered answers Covers.
func (c *RecordingCanvas) CellCovered(col, row int) bool {
	return c.Covers != nil && c.Covers(col, row)
}

// Indexed is Theme's colour for 256-colour index i (Palette).
func (c *RecordingCanvas) Indexed(i uint8) color.NRGBA {
	if th := c.root.Theme; th != nil {
		return th.Indexed(i)
	}
	return defaultPalette.Indexed(i)
}

// Backdrop returns the simulated backdrop color.
func (c *RecordingCanvas) Backdrop() color.NRGBA { return c.Around }

// CellText returns the simulated text of a character cell.
func (c *RecordingCanvas) CellText(col, row int) string {
	if c.Content == nil {
		return ""
	}
	return c.Content(col, row)
}

// PaintCells records rasterizing a rectangle of cells.
func (c *RecordingCanvas) PaintCells(r CellRect, glyphsOnly bool) {
	c.rec(PaintCall{Op: "PaintCells", Cells: r, GlyphsOnly: glyphsOnly})
}

// Ops lists the recorded calls' Op names, in order.
func (c *RecordingCanvas) Ops() []string {
	out := make([]string, len(c.root.Calls))
	for i, p := range c.root.Calls {
		out[i] = p.Op
	}
	return out
}

var (
	_ Canvas      = (*RecordingCanvas)(nil)
	_ CellCoverer = (*RecordingCanvas)(nil)
	_ CellCoverer = (*gioCanvas)(nil)
	_ cellHost    = (*RecordingCanvas)(nil)
	_ cellHost    = (*gioCanvas)(nil)
)
