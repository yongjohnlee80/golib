package vegalite

// The geometry and font a chart is laid out in: plain values, each the same shape as gui's (a
// gui.Rect converts to a Rect and back, a gui.Font to a Font), so gui/vegalite paints a chart laid
// out here without this package importing gui and, through it, gio.

// Size is a width and a height, in logical pixels.
type Size struct{ W, H float32 }

// Point is a position.
type Point struct{ X, Y float32 }

// Pt is the point (x, y).
func Pt(x, y float32) Point { return Point{X: x, Y: y} }

// Rect is a box: its top-left corner and its size.
type Rect struct{ X, Y, W, H float32 }

// Font is a label's font: its family ("" is the proportional face, sans-serif in an SVG), its
// size in logical pixels, its weight and its slant.
type Font struct {
	Family string
	Size   float32
	Bold   bool
	Italic bool
}

// LineHeight is a line's height over its font's size, gui's.
const LineHeight = 1.2

// Measure is s's width in f, in logical pixels: a label's, as it will be drawn. A window measures
// with its shaper (gui/vegalite wraps a gui.Measurer); a build without one uses [ApproxMeasure].
type Measure func(s string, f Font) float32
