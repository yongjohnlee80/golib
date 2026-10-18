package gui

import (
	"image"
	"image/color"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/op/paint"
)

// Box-drawing and block characters (Unicode ranges U+2500–U+257F and U+2580–U+259F)
// are rendered using cell-relative vector geometry rather than font glyphs.
//
// # Why Font Glyphs Are Bypassed
//
// Traditional terminal emulators often draw box-drawing characters using font glyphs.
// However, font glyph metrics rarely align seamlessly with cell boundaries:
//
//	Font Glyph Approach (Prone to Gaps & Misalignment):
//	┌─────────┐┌─────────┐
//	│  ───    ││    ───  │  <-- Gaps and misalignments appear at non-integer scales
//	└─────────┘└─────────┘
//
//	Geometric Cell Approach (Pixel-Exact):
//	┌─────────┬─────────┐
//	│─────────┼─────────│  <-- Continuous lines snap to cell centers and edges exactly
//	└─────────┴─────────┘
//
// By calculating vector geometries directly from cell bounds:
//   - Horizontal and vertical lines span from cell edge to cell edge without fractional gaps.
//   - Intersections, T-junctions, and cross-pieces meet at cell centers with exact corner filling.
//   - Rounded corners (arcs) connect arm endpoints smoothly using quadratic Bézier curves.
//   - Block characters (half, quarter, eighth blocks, and shades) fill exact proportional fractions
//     of the cell rectangle.
//
// Any character not present in these geometric tables falls back to font shaping.
//
// # Line Weights and Arm Model
//
// A line character is parameterized by four directional arms: up, right, down, and left.
// Each arm carries a line weight:
//   - none (0): no line segment in this direction
//   - light (1): standard stroke width (1 device-independent pixel, scaled to display dpi)
//   - heavy (2): double-weight stroke width
//   - double (3): two parallel light strokes separated by a light stroke gap
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

// drawable reports whether rune r is supported by custom geometric rendering
// rather than standard font shaping.
//
// Returns true for supported box-drawing lines, rounded arcs, and block elements
// (U+2580 through U+2595).
func drawable(r rune) bool {
	if _, ok := lineArms[r]; ok {
		return true
	}
	if _, ok := arcArms[r]; ok {
		return true
	}
	return r >= 0x2580 && r <= 0x2595
}

// drawBox renders a geometric box-drawing rune r over the specified cell rectangle.
//
// Parameters:
//   - ops: Gio operation list recording vector drawing commands.
//   - r: the box-drawing or block character rune.
//   - rect: pixel boundary rectangle of the target cell.
//   - c: stroke or fill color.
//   - scale: display scaling factor (dpi multiplier).
//
// Returns true if the character was recognized and drawn geometrically, or false
// if the character must fall back to font glyph rasterization.
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

// drawArms renders the directional line segments of a box-drawing rune.
//
// Lines are centered on the cell's midpoint. Vertical arms extend fully to the outer
// edge of any horizontal line they meet, and horizontal arms extend to the outer edge
// of any vertical line, ensuring junctions and corners are solid rectangles with no
// gaps or overlapping stubs. Double arms render as two parallel light lines separated
// by a light stroke gap.
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

// drawArc renders a smooth quarter-circle rounded corner connecting two arms.
//
// Parameters:
//   - ops: Gio operation buffer receiving path clipping and fill operations.
//   - a: pair of arm directions being connected (e.g., [1, 2] for top-left arc connecting right and down).
//   - rect: cell boundary rectangle.
//   - c: stroke color.
//   - lw: stroke line width in pixels.
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

// drawBlock renders Unicode block elements (U+2580 through U+2595).
//
// Supports:
//   - Half blocks: upper half ('▀'), right half ('▐')
//   - Lower eighth blocks: U+2581 (' ') through U+2588 ('█')
//   - Left eighth blocks: U+2589 ('▉') through U+258F ('▏')
//   - Upper and right eighth blocks: U+2594 ('▔'), U+2595 ('▕')
//   - Shading blocks: light 25% ('░'), medium 50% ('▒'), dark 75% ('▓')
//
// Returns true if rune r is a handled block element, false otherwise.
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

// fillRect renders a solid rectangular fill in color c clipped to rect.
func fillRect(ops *op.Ops, rect image.Rectangle, c color.NRGBA) {
	countFill()
	st := clip.Rect(rect).Push(ops)
	paint.ColorOp{Color: c}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}
