package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ShapeKind is what a Shape draws.
type ShapeKind uint8

const (
	// ShapeRect is a rectangle, its corners rounded by the Shape's radius.
	ShapeRect ShapeKind = iota
	// ShapeEllipse is the ellipse inscribed in the Shape's rectangle.
	ShapeEllipse
)

// Shape is a filled and stroked rectangle or ellipse over its cells, in pixels: Qt's Rectangle
// (color, radius, border) and an ellipse. On a terminal it draws its outline in box-drawing
// characters, rounded for a rounded rectangle and an ellipse, with its fill as the inner cells'
// background.
type Shape struct {
	gui.Base
	ctx         *tui.Context
	kind        ShapeKind
	fill        color.NRGBA
	stroke      color.NRGBA
	strokeWidth float32
	radius      float32
	size        tui.Size // the cells it asks for; zero takes what the layout gives
}

// ShapeOption sets up a Shape under construction.
type ShapeOption func(*Shape)

// WithFillColor fills the shape with c.
func WithFillColor(c color.NRGBA) ShapeOption { return func(s *Shape) { s.fill = c } }

// WithStroke outlines the shape in c, width dp wide, inside its rectangle.
func WithStroke(c color.NRGBA, width float32) ShapeOption {
	return func(s *Shape) { s.stroke, s.strokeWidth = c, max(width, 0) }
}

// WithRadius rounds a rectangle's corners by r dp.
func WithRadius(r float32) ShapeOption { return func(s *Shape) { s.radius = max(r, 0) } }

// WithShapeSize asks for w x h cells; without it the shape takes what its layout gives.
func WithShapeSize(w, h int) ShapeOption {
	return func(s *Shape) { s.size = tui.Size{W: max(w, 0), H: max(h, 0)} }
}

// NewShape is a shape of kind.
func NewShape(kind ShapeKind, opts ...ShapeOption) *Shape {
	s := &Shape{kind: kind}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	s.Bind(s)
	return s
}

// SetFillColor changes the fill.
func (s *Shape) SetFillColor(c color.NRGBA) { s.fill = c; s.dirty() }

// SetStroke changes the outline.
func (s *Shape) SetStroke(c color.NRGBA, width float32) {
	s.stroke, s.strokeWidth = c, max(width, 0)
	s.dirty()
}

// SetRadius changes a rectangle's corner radius.
func (s *Shape) SetRadius(r float32) { s.radius = max(r, 0); s.dirty() }

func (s *Shape) dirty() {
	if s.ctx != nil {
		s.ctx.MarkDirty()
	}
}

// Init keeps the shape's Context.
func (s *Shape) Init(ctx *tui.Context) { s.ctx = ctx }

// Layout is the asked size, or all the room when none was asked (none on an unbounded axis).
func (s *Shape) Layout(c tui.Constraints) tui.Size {
	sz := s.size
	if sz.W == 0 && c.MaxW != tui.Unbounded {
		sz.W = c.MaxW
	}
	if sz.H == 0 && c.MaxH != tui.Unbounded {
		sz.H = c.MaxH
	}
	return c.Constrain(sz)
}

// Render draws the terminal's outline (RenderTUI) where native views aren't painted.
func (s *Shape) Render(surf tui.Surface) { s.RenderFallback(s.ctx, surf) }

// HandleEvent consumes nothing.
func (*Shape) HandleEvent(tui.Event) bool { return false }

// Paint fills and strokes the shape over the canvas, the stroke inside its rectangle.
func (s *Shape) Paint(c gui.Canvas) {
	sz := c.Size()
	r := gui.Rect{W: sz.W, H: sz.H}
	if s.stroke.A > 0 && s.strokeWidth > 0 {
		h := s.strokeWidth / 2
		r = gui.Rect{X: h, Y: h, W: max(sz.W-s.strokeWidth, 0), H: max(sz.H-s.strokeWidth, 0)}
	}
	switch s.kind {
	case ShapeEllipse:
		if s.fill.A > 0 {
			c.FillEllipse(r, gui.Solid(s.fill))
		}
		if s.stroke.A > 0 && s.strokeWidth > 0 {
			c.StrokePath(new(gui.Path).Ellipse(r), s.strokeWidth, gui.Solid(s.stroke))
		}
	default:
		if s.fill.A > 0 {
			if s.radius > 0 {
				c.FillRRect(r, s.radius, gui.Solid(s.fill))
			} else {
				c.FillRect(r, gui.Solid(s.fill))
			}
		}
		if s.stroke.A > 0 && s.strokeWidth > 0 {
			c.StrokeRRect(r, s.radius, s.strokeWidth, gui.Solid(s.stroke))
		}
	}
}

// RenderTUI is the shape's outline in box-drawing characters (rounded for a rounded rectangle and an
// ellipse), in its stroke colour, over its fill as the cells' background.
func (s *Shape) RenderTUI(surf tui.Surface) {
	sz := surf.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	st := style.New()
	if s.fill.A > 0 {
		st = st.Background(style.RGB(s.fill.R, s.fill.G, s.fill.B))
	}
	surf.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", st)
	if sz.W < 2 || sz.H < 2 {
		return
	}
	line := st
	if s.stroke.A > 0 {
		line = line.Foreground(style.RGB(s.stroke.R, s.stroke.G, s.stroke.B))
	}
	tl, tr, bl, br := "┌", "┐", "└", "┘"
	if s.kind == ShapeEllipse || s.radius > 0 {
		tl, tr, bl, br = "╭", "╮", "╰", "╯"
	}
	for x := 1; x < sz.W-1; x++ {
		surf.SetCell(x, 0, "─", line)
		surf.SetCell(x, sz.H-1, "─", line)
	}
	for y := 1; y < sz.H-1; y++ {
		surf.SetCell(0, y, "│", line)
		surf.SetCell(sz.W-1, y, "│", line)
	}
	surf.SetCell(0, 0, tl, line)
	surf.SetCell(sz.W-1, 0, tr, line)
	surf.SetCell(0, sz.H-1, bl, line)
	surf.SetCell(sz.W-1, sz.H-1, br, line)
}

var (
	_ tui.NativeReporter = (*Shape)(nil)
	_ gui.TUIMock        = (*Shape)(nil)
)
