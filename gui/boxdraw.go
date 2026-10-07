package gui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Box-drawing and block characters are drawn from the cell's own geometry, not the font, as
// kitty and Ghostty do: a font's glyphs rarely meet the next cell exactly, and a border drawn
// from them shows seams at any scale but the font's own. Characters not in these tables fall
// back to the font.

// arms is a line character's four arms, up, right, down, left: 0 none, 1 light, 2 heavy, 3 double.
type arms [4]uint8

const (
	none uint8 = iota
	light
	heavy
	double
)

var lineArms = map[rune]arms{
	'─': {0, 1, 0, 1}, '━': {0, 2, 0, 2}, '│': {1, 0, 1, 0}, '┃': {2, 0, 2, 0},
	// dashed lines (U+2504–U+250B) are drawn solid
	'┄': {0, 1, 0, 1}, '┅': {0, 2, 0, 2}, '┆': {1, 0, 1, 0}, '┇': {2, 0, 2, 0},
	'┈': {0, 1, 0, 1}, '┉': {0, 2, 0, 2}, '┊': {1, 0, 1, 0}, '┋': {2, 0, 2, 0},
	'┌': {0, 1, 1, 0}, '┍': {0, 2, 1, 0}, '┎': {0, 1, 2, 0}, '┏': {0, 2, 2, 0},
	'┐': {0, 0, 1, 1}, '┑': {0, 0, 1, 2}, '┒': {0, 0, 2, 1}, '┓': {0, 0, 2, 2},
	'└': {1, 1, 0, 0}, '┕': {1, 2, 0, 0}, '┖': {2, 1, 0, 0}, '┗': {2, 2, 0, 0},
	'┘': {1, 0, 0, 1}, '┙': {1, 0, 0, 2}, '┚': {2, 0, 0, 1}, '┛': {2, 0, 0, 2},
	'├': {1, 1, 1, 0}, '┝': {1, 2, 1, 0}, '┞': {2, 1, 1, 0}, '┟': {1, 1, 2, 0},
	'┠': {2, 1, 2, 0}, '┡': {2, 2, 1, 0}, '┢': {1, 2, 2, 0}, '┣': {2, 2, 2, 0},
	'┤': {1, 0, 1, 1}, '┥': {1, 0, 1, 2}, '┦': {2, 0, 1, 1}, '┧': {1, 0, 2, 1},
	'┨': {2, 0, 2, 1}, '┩': {2, 0, 1, 2}, '┪': {1, 0, 2, 2}, '┫': {2, 0, 2, 2},
	'┬': {0, 1, 1, 1}, '┭': {0, 1, 1, 2}, '┮': {0, 2, 1, 1}, '┯': {0, 2, 1, 2},
	'┰': {0, 1, 2, 1}, '┱': {0, 1, 2, 2}, '┲': {0, 2, 2, 1}, '┳': {0, 2, 2, 2},
	'┴': {1, 1, 0, 1}, '┵': {1, 1, 0, 2}, '┶': {1, 2, 0, 1}, '┷': {1, 2, 0, 2},
	'┸': {2, 1, 0, 1}, '┹': {2, 1, 0, 2}, '┺': {2, 2, 0, 1}, '┻': {2, 2, 0, 2},
	'┼': {1, 1, 1, 1}, '╋': {2, 2, 2, 2},
	'═': {0, 3, 0, 3}, '║': {3, 0, 3, 0},
	'╔': {0, 3, 3, 0}, '╗': {0, 0, 3, 3}, '╚': {3, 3, 0, 0}, '╝': {3, 0, 0, 3},
	'╠': {3, 3, 3, 0}, '╣': {3, 0, 3, 3}, '╦': {0, 3, 3, 3}, '╩': {3, 3, 0, 3}, '╬': {3, 3, 3, 3},
	'╴': {0, 0, 0, 1}, '╵': {1, 0, 0, 0}, '╶': {0, 1, 0, 0}, '╷': {0, 0, 1, 0},
	'╸': {0, 0, 0, 2}, '╹': {2, 0, 0, 0}, '╺': {0, 2, 0, 0}, '╻': {0, 0, 2, 0},
}

// arcs are the rounded corners: the two arms each joins.
var arcArms = map[rune][2]int{ // arm indexes: 0 up, 1 right, 2 down, 3 left
	'╭': {1, 2}, '╮': {3, 2}, '╯': {3, 0}, '╰': {1, 0},
}

// drawable reports whether r is drawn from geometry rather than the font.
func drawable(r rune) bool {
	if _, ok := lineArms[r]; ok {
		return true
	}
	if _, ok := arcArms[r]; ok {
		return true
	}
	return r >= 0x2580 && r <= 0x2595
}

// drawBox draws r over cell rect in c. It reports false for a rune it does not draw.
func drawBox(ops *op.Ops, r rune, rect image.Rectangle, c color.NRGBA, scale float32) bool {
	lw := max(1, int(scale+0.5)) // a light line: one device-independent pixel
	if a, ok := lineArms[r]; ok {
		drawArms(ops, a, rect, c, lw)
		return true
	}
	if a, ok := arcArms[r]; ok {
		drawArc(ops, a, rect, c, lw)
		return true
	}
	return drawBlock(ops, r, rect, c)
}

// drawArms draws a line character's arms. Lines are centred on the cell's middle. A vertical
// arm runs to the far edge of the horizontal line it meets, and a horizontal arm to the far edge
// of the vertical one, so corners and junctions are filled exactly, with no stub past the join.
// A double arm is two light lines a light line apart.
func drawArms(ops *op.Ops, a arms, rect image.Rectangle, c color.NRGBA, lw int) {
	for _, r := range armRects(a, rect, lw) {
		fillRect(ops, r, c)
	}
}

// armRects is the rectangles drawArms fills.
func armRects(a arms, rect image.Rectangle, lw int) []image.Rectangle {
	var out []image.Rectangle
	cx := rect.Min.X + rect.Dx()/2
	cy := rect.Min.Y + rect.Dy()/2
	// span is the extent, across the line, of a line of weight w centred on c.
	span := func(w uint8, c int) (lo, hi int) {
		switch w {
		case heavy:
			return c - lw, c + lw
		case double:
			return c - lw - lw/2, c + lw - lw/2 + lw
		case light:
			return c - lw/2, c - lw/2 + lw
		}
		return c, c
	}
	vlo, vhi := span(max(a[0], a[2]), cx)    // the vertical line's extent across x
	hlo, hhi := span(max(a[1], a[3]), cy)    // the horizontal line's extent across y
	lines := func(w uint8, c int) [][2]int { // the [lo, hi) of each stroke of one arm
		if w == double {
			lo, _ := span(light, c-lw)
			lo2, _ := span(light, c+lw)
			return [][2]int{{lo, lo + lw}, {lo2, lo2 + lw}}
		}
		lo, hi := span(w, c)
		return [][2]int{{lo, hi}}
	}
	for i, w := range a {
		if w == none {
			continue
		}
		switch i {
		case 0: // up: from the top to the bottom of the horizontal line
			for _, l := range lines(w, cx) {
				out = append(out, image.Rect(l[0], rect.Min.Y, l[1], hhi))
			}
		case 2: // down: from the top of the horizontal line to the bottom
			for _, l := range lines(w, cx) {
				out = append(out, image.Rect(l[0], hlo, l[1], rect.Max.Y))
			}
		case 1: // right: from the left of the vertical line to the right edge
			for _, l := range lines(w, cy) {
				out = append(out, image.Rect(vlo, l[0], rect.Max.X, l[1]))
			}
		case 3: // left: from the left edge to the right of the vertical line
			for _, l := range lines(w, cy) {
				out = append(out, image.Rect(rect.Min.X, l[0], vhi, l[1]))
			}
		}
	}
	return out
}

// drawArc draws a rounded corner joining two light arms through a quarter circle.
func drawArc(ops *op.Ops, a [2]int, rect image.Rectangle, c color.NRGBA, lw int) {
	cx := float32(rect.Min.X) + float32(rect.Dx()/2) + 0.5*float32(lw%2)
	cy := float32(rect.Min.Y) + float32(rect.Dy()/2) + 0.5*float32(lw%2)
	rad := float32(min(rect.Dx(), rect.Dy())) / 2
	end := func(arm int) (edge, near f32.Point) {
		switch arm {
		case 0:
			return f32.Pt(cx, float32(rect.Min.Y)), f32.Pt(cx, cy-rad)
		case 1:
			return f32.Pt(float32(rect.Max.X), cy), f32.Pt(cx+rad, cy)
		case 2:
			return f32.Pt(cx, float32(rect.Max.Y)), f32.Pt(cx, cy+rad)
		default:
			return f32.Pt(float32(rect.Min.X), cy), f32.Pt(cx-rad, cy)
		}
	}
	e0, n0 := end(a[0])
	e1, n1 := end(a[1])
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(e0)
	p.LineTo(n0)
	p.QuadTo(f32.Pt(cx, cy), n1)
	p.LineTo(e1)
	st := clip.Stroke{Path: p.End(), Width: float32(lw)}.Op().Push(ops)
	paint.ColorOp{Color: c}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}

// drawBlock draws the block elements U+2580–U+2595: halves, eighths and shades.
func drawBlock(ops *op.Ops, r rune, rect image.Rectangle, c color.NRGBA) bool {
	w, h := rect.Dx(), rect.Dy()
	frac := func(n, of, total int) int { return (total*n + of/2) / of }
	switch {
	case r == '▀': // upper half
		fillRect(ops, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+h/2), c)
	case r >= '▁' && r <= '█': // lower n/8, U+2581–U+2588
		n := int(r-'▁') + 1
		fillRect(ops, image.Rect(rect.Min.X, rect.Max.Y-frac(n, 8, h), rect.Max.X, rect.Max.Y), c)
	case r >= '▉' && r <= '▏': // left n/8, U+2589 (7/8) down to U+258F (1/8)
		n := 8 - int(r-'▉') - 1
		fillRect(ops, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+frac(n, 8, w), rect.Max.Y), c)
	case r == '▐': // right half
		fillRect(ops, image.Rect(rect.Min.X+w/2, rect.Min.Y, rect.Max.X, rect.Max.Y), c)
	case r == '░' || r == '▒' || r == '▓': // shades: the colour at 25, 50, 75%
		shade := c
		shade.A = uint8(int(c.A) * (int(r-'░') + 1) / 4)
		fillRect(ops, rect, shade)
	case r == '▔': // upper 1/8
		fillRect(ops, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+frac(1, 8, h)), c)
	case r == '▕': // right 1/8
		fillRect(ops, image.Rect(rect.Max.X-frac(1, 8, w), rect.Min.Y, rect.Max.X, rect.Max.Y), c)
	default:
		return false
	}
	return true
}

// fillRect paints rect in c.
func fillRect(ops *op.Ops, rect image.Rectangle, c color.NRGBA) {
	st := clip.Rect(rect).Push(ops)
	paint.ColorOp{Color: c}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}
