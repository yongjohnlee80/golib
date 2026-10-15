package gui

import (
	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
)

// Vector Path Geometry and Bézier Curves:
//
//	MoveTo (M)       LineTo (L)           QuadTo (Q)             CubeTo (C)
//	  (x, y)          --------->            .--.                  .----.
//	 (Start)         (Endpoint)            /    \                /      \
//	                                     (ctrl)  (to)        (ctrl0)  (ctrl1) (to)
//
// Path represents a reusable, resolution-independent 2D vector path composed of linear
// segments and Bézier curves. It can be constructed via [Path.MoveTo], [Path.LineTo],
// [Path.QuadTo], [Path.CubeTo], and [Path.Close], and rasterized across [Canvas.FillPath]
// and [Canvas.StrokePath] without reallocating geometry buffers.
type Path struct {
	cmds []pathCmd
}

// pathCmd represents a single path instruction and its associated coordinate operands.
type pathCmd struct {
	kind    byte // Command operator: 'M' (move), 'L' (line), 'Q' (quadratic), 'C' (cubic), 'Z' (close)
	pts     [3]Point
	npoints int
}

// MoveTo starts a new sub-path at p.
func (p *Path) MoveTo(to Point) *Path { return p.add('M', to) }

// LineTo draws a straight line to p.
func (p *Path) LineTo(to Point) *Path { return p.add('L', to) }

// QuadTo draws a quadratic Bézier curve through ctrl to p.
func (p *Path) QuadTo(ctrl, to Point) *Path { return p.add('Q', ctrl, to) }

// CubeTo draws a cubic Bézier curve through ctrl0 and ctrl1 to p.
func (p *Path) CubeTo(ctrl0, ctrl1, to Point) *Path { return p.add('C', ctrl0, ctrl1, to) }

// Close closes the current sub-path with a line to its start.
func (p *Path) Close() *Path { return p.add('Z') }

// add appends a command of kind with points pts to the path.
func (p *Path) add(kind byte, pts ...Point) *Path {
	c := pathCmd{kind: kind, npoints: len(pts)}
	copy(c.pts[:], pts)
	p.cmds = append(p.cmds, c)
	return p
}

// Segments is how many commands the path holds: moves, lines, curves and closes.
func (p *Path) Segments() int { return len(p.cmds) }

// Bounds is the smallest rectangle holding every point and control point of the path; the zero
// Rect for an empty one. A curve lies inside its control points' hull, so this holds the path.
func (p *Path) Bounds() Rect {
	first := true
	var x0, y0, x1, y1 float32
	for _, c := range p.cmds {
		for _, q := range c.pts[:c.npoints] {
			if first {
				x0, y0, x1, y1, first = q.X, q.Y, q.X, q.Y, false
				continue
			}
			x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
		}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// fp converts a Point into Gio's f32.Point.
func fp(p Point) f32.Point { return f32.Pt(p.X, p.Y) }

// spec records the path into ops.
func (p *Path) spec(ops *op.Ops) clip.PathSpec {
	var cp clip.Path
	cp.Begin(ops)
	for _, c := range p.cmds {
		switch c.kind {
		case 'M':
			cp.MoveTo(fp(c.pts[0]))
		case 'L':
			cp.LineTo(fp(c.pts[0]))
		case 'Q':
			cp.QuadTo(fp(c.pts[0]), fp(c.pts[1]))
		case 'C':
			cp.CubeTo(fp(c.pts[0]), fp(c.pts[1]), fp(c.pts[2]))
		case 'Z':
			cp.Close()
		}
	}
	return cp.End()
}

// kappa places a cubic's control points so it approximates a quarter circle.
const kappa = 0.5522847498

// rrectPath is r with corners of the given radius, clamped to half the shorter side.
func rrectPath(r Rect, radius float32) *Path {
	rad := min(max(radius, 0), r.W/2, r.H/2)
	k := rad * (1 - kappa)
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
	p := new(Path)
	p.MoveTo(Pt(x0+rad, y0)).LineTo(Pt(x1-rad, y0))
	p.CubeTo(Pt(x1-k, y0), Pt(x1, y0+k), Pt(x1, y0+rad)).LineTo(Pt(x1, y1-rad))
	p.CubeTo(Pt(x1, y1-k), Pt(x1-k, y1), Pt(x1-rad, y1)).LineTo(Pt(x0+rad, y1))
	p.CubeTo(Pt(x0+k, y1), Pt(x0, y1-k), Pt(x0, y1-rad)).LineTo(Pt(x0, y0+rad))
	p.CubeTo(Pt(x0, y0+k), Pt(x0+k, y0), Pt(x0+rad, y0))
	return p.Close()
}

// ellipsePath is the ellipse inscribed in r.
func ellipsePath(r Rect) *Path {
	rx, ry := r.W/2, r.H/2
	cx, cy := r.X+rx, r.Y+ry
	kx, ky := rx*kappa, ry*kappa
	p := new(Path)
	p.MoveTo(Pt(cx+rx, cy))
	p.CubeTo(Pt(cx+rx, cy+ky), Pt(cx+kx, cy+ry), Pt(cx, cy+ry))
	p.CubeTo(Pt(cx-kx, cy+ry), Pt(cx-rx, cy+ky), Pt(cx-rx, cy))
	p.CubeTo(Pt(cx-rx, cy-ky), Pt(cx-kx, cy-ry), Pt(cx, cy-ry))
	p.CubeTo(Pt(cx+kx, cy-ry), Pt(cx+rx, cy-ky), Pt(cx+rx, cy))
	return p.Close()
}
