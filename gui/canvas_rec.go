package gui

import (
	"image/color"

	"gioui.org/font/gofont"
	"gioui.org/text"
)

// PaintCall is one drawing call a RecordingCanvas received. Rect is in the recording canvas's
// root coordinates (a Sub's offset applied); transforms are recorded as calls, not applied.
type PaintCall struct {
	Op         string // "FillRect", "FillRRect", "StrokeRRect", "FillEllipse", "FillPath", "StrokePath", "DrawText", "DrawImage", "PushTransform", "PushOpacity", "Pop", "WindowMoveArea", "PaintCells"
	Rect       Rect
	Radius     float32
	Width      float32 // a stroke's
	Brush      Brush
	Text       *TextLayout
	Cells      CellRect // PaintCells's, in the root's cells
	GlyphsOnly bool
	Opacity    float32
}

// RecordingCanvas is a Canvas that records what is drawn on it instead of drawing, for testing a
// View without a window or a GPU.
type RecordingCanvas struct {
	Calls []PaintCall
	// Colors answers CellColors; nil answers zero colours.
	Colors func(col, row int) (fg, bg color.NRGBA)
	// Content answers CellText; nil answers "".
	Content func(col, row int) string
	// Around answers Backdrop.
	Around color.NRGBA
	// TextPx answers TextSize; 0: the cell's height over the default font's line height.
	TextPx float32

	root   *RecordingCanvas
	off    Point
	size   Size
	cell   Size
	shaper *TextShaper
}

// NewRecordingCanvas is a canvas of size, over cells of cell size, at scale 1.
func NewRecordingCanvas(size, cell Size) *RecordingCanvas {
	rc := &RecordingCanvas{size: size, cell: cell,
		shaper: &TextShaper{s: text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), scale: 1,
			fonts: defaultFonts()}}
	rc.root = rc
	return rc
}

func (c *RecordingCanvas) rec(p PaintCall) {
	p.Rect.X += c.off.X
	p.Rect.Y += c.off.Y
	c.root.Calls = append(c.root.Calls, p)
}

// Sub records into the same log, offset by r.
func (c *RecordingCanvas) Sub(r Rect) Canvas {
	return &RecordingCanvas{root: c.root, off: Pt(c.off.X+r.X, c.off.Y+r.Y), size: r.Size(), cell: c.cell, shaper: c.shaper, Colors: c.Colors, Content: c.Content, Around: c.Around}
}

func (c *RecordingCanvas) Size() Size        { return c.size }
func (c *RecordingCanvas) Scale() float32    { return 1 }
func (c *RecordingCanvas) Text() *TextShaper { return c.shaper }

// Fonts are the recording's families: the defaults, unless SetFonts set others.
func (c *RecordingCanvas) Fonts() Fonts { return c.root.shaper.fonts }

// SetFonts gives the recording other families, as a window's change of font would: views painted
// on it afterwards shape in them and see the new generation.
func (c *RecordingCanvas) SetFonts(f Fonts) { c.root.shaper.fonts = f }
func (c *RecordingCanvas) CellSize() Size   { return c.cell }

// TextSize answers TextPx when set, else the cell's height at the line height the default cell
// font gives (about 1.2 times its size).
func (c *RecordingCanvas) TextSize() float32 {
	if c.root.TextPx > 0 {
		return c.root.TextPx
	}
	return c.cell.H / 1.2
}

func (c *RecordingCanvas) FillRect(r Rect, b Brush) {
	c.rec(PaintCall{Op: "FillRect", Rect: r, Brush: b})
}
func (c *RecordingCanvas) FillRRect(r Rect, radius float32, b Brush) {
	c.rec(PaintCall{Op: "FillRRect", Rect: r, Radius: radius, Brush: b})
}
func (c *RecordingCanvas) StrokeRRect(r Rect, radius, width float32, b Brush) {
	c.rec(PaintCall{Op: "StrokeRRect", Rect: r, Radius: radius, Width: width, Brush: b})
}
func (c *RecordingCanvas) FillEllipse(r Rect, b Brush) {
	c.rec(PaintCall{Op: "FillEllipse", Rect: r, Brush: b})
}
func (c *RecordingCanvas) FillPath(_ *Path, b Brush) { c.rec(PaintCall{Op: "FillPath", Brush: b}) }
func (c *RecordingCanvas) StrokePath(_ *Path, width float32, b Brush) {
	c.rec(PaintCall{Op: "StrokePath", Width: width, Brush: b})
}
func (c *RecordingCanvas) DrawText(t *TextLayout, at Point, b Brush) {
	var w, h float32
	if t != nil {
		w, h = t.Width, t.Height
	}
	c.rec(PaintCall{Op: "DrawText", Rect: Rect{X: at.X, Y: at.Y, W: w, H: h}, Text: t, Brush: b})
}
func (c *RecordingCanvas) DrawImage(_ *Image, dst, _ Rect) {
	c.rec(PaintCall{Op: "DrawImage", Rect: dst})
}
func (c *RecordingCanvas) PushTransform(Affine)  { c.rec(PaintCall{Op: "PushTransform"}) }
func (c *RecordingCanvas) PushOpacity(a float32) { c.rec(PaintCall{Op: "PushOpacity", Opacity: a}) }
func (c *RecordingCanvas) Pop()                  { c.rec(PaintCall{Op: "Pop"}) }
func (c *RecordingCanvas) WindowMoveArea(r Rect) { c.rec(PaintCall{Op: "WindowMoveArea", Rect: r}) }

func (c *RecordingCanvas) CellColors(col, row int) (fg, bg color.NRGBA) {
	if c.Colors == nil {
		return color.NRGBA{}, color.NRGBA{}
	}
	return c.Colors(col, row)
}

func (c *RecordingCanvas) Backdrop() color.NRGBA { return c.Around }

func (c *RecordingCanvas) CellText(col, row int) string {
	if c.Content == nil {
		return ""
	}
	return c.Content(col, row)
}

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

var _ Canvas = (*RecordingCanvas)(nil)
