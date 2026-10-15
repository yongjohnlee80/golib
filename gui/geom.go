package gui

import (
	"image/color"

	"github.com/yongjohnlee80/gio/f32"
)

// Coordinate Spaces in GUI Architecture:
//
//	Logical Coordinates (dp)          Physical Coordinates (px)
//	  Size{W, H float32}      * Scale    -> image.Point (Device Pixels)
//	  Point{X, Y float32}     * Scale    -> f32.Point (GPU Rasterizer)
//	  Rect{X, Y, W, H float32}* Scale    -> clip.Rect / image.Rectangle
//
// Size, Point, and Rect represent 2D geometric quantities expressed in device-independent
// logical pixels (dp).
//
// These structures define the primary layout and vector painting coordinates utilized by
// [Canvas], [View], and native embedded widgets. They are translated to device pixels during
// rasterization based on the window display scale ([Canvas.Scale]).
type (
	// Size specifies a 2D extent with floating-point width and height in logical pixels.
	Size struct{ W, H float32 }

	// Point specifies a 2D position (X, Y) in logical pixels.
	Point struct{ X, Y float32 }

	// Rect specifies a 2D bounding rectangle in logical pixels.
	Rect struct{ X, Y, W, H float32 }
)

// Pt is the point (x, y).
func Pt(x, y float32) Point { return Point{X: x, Y: y} }

// Size is r's size.
func (r Rect) Size() Size { return Size{W: r.W, H: r.H} }

// Inset is r shrunk by d on every side; a negative d grows it. It never goes below zero size.
func (r Rect) Inset(d float32) Rect {
	return Rect{X: r.X + d, Y: r.Y + d, W: max(r.W-2*d, 0), H: max(r.H-2*d, 0)}
}

// Affine is a 2D affine transform, as PushTransform takes it.
type Affine = f32.Affine2D

// Brush is what a shape is filled or stroked with: a solid colour, or a linear gradient.
type Brush struct {
	Color    color.NRGBA
	Gradient *LinearGradient // nil: solid Color
}

// LinearGradient blends From's colour at From to To's colour at To, in the canvas's coordinates.
type LinearGradient struct {
	From, To           Point
	FromColor, ToColor color.NRGBA
}

// Solid is a brush of one colour.
func Solid(c color.NRGBA) Brush { return Brush{Color: c} }

// CellRect is a rectangle of cells, in the cells under a canvas.
type CellRect struct{ X, Y, W, H int }
