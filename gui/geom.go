package gui

import (
	"image/color"

	"gioui.org/f32"
)

// Size, Point and Rect are logical pixels (device-independent), the units a View paints in.
type (
	Size  struct{ W, H float32 }
	Point struct{ X, Y float32 }
	Rect  struct{ X, Y, W, H float32 }
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
