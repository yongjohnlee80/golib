package widget

import (
	"math"

	"github.com/yongjohnlee80/golib/tui"
)

// float_anchor.go: Anchor, the value that says where a Float's content goes, its constructors and
// the fraction arithmetic. The Float widget is float.go.

// Anchor positions a Float's content within the overlay area.
type Anchor struct {
	align      tui.Align
	atRect     bool
	rect       tui.Rect
	atFraction bool
	frac       [4]float64 // x, y, w, h: percent of the overlay area
	live       *liveRect  // AtLive's; a pointer, so Anchors still compare
}

// liveRect is AtLive's rectangle, asked for at each placement.
type liveRect struct{ rect func() (tui.Rect, bool) }

// resolve is the rectangle a live anchor names now, inside area; ok false when it names none.
func (a Anchor) resolve(area tui.Rect) (tui.Rect, bool) {
	if a.live == nil {
		return tui.Rect{}, false
	}
	r, ok := a.live.rect()
	if !ok || r.W <= 0 || r.H <= 0 {
		return tui.Rect{}, false
	}
	return windowBoundsInArea(r, area), true
}

// The alignment anchors. Center is the Float default.
var (
	Center      = Anchor{align: tui.AlignCenter}
	TopLeft     = Anchor{align: tui.AlignTopLeft}
	Top         = Anchor{align: tui.AlignTop}
	TopRight    = Anchor{align: tui.AlignTopRight}
	Left        = Anchor{align: tui.AlignLeft}
	Right       = Anchor{align: tui.AlignRight}
	BottomLeft  = Anchor{align: tui.AlignBottomLeft}
	Bottom      = Anchor{align: tui.AlignBottom}
	BottomRight = Anchor{align: tui.AlignBottomRight}
)

// AtRect anchors the float at an explicit overlay-relative rectangle.
func AtRect(r tui.Rect) Anchor { return Anchor{atRect: true, rect: r} }

// AtLive anchors the float at the overlay-relative rectangle rect reports each time the float is
// placed, so it follows something another layout places in the same pass: a reserved strip of a
// Window (a reserving Drawer). rect runs inside Layout and must only read. While it reports false,
// or an empty rectangle, the float is placed as otherwise places it, whatever kind of anchor that is
// (an edge, AtRect, AtFraction). A rectangle rect reports wins over otherwise wherever the float is
// placed or asked where it is. A live-anchored float is docked, not floating: it is where the layout
// put its rectangle, or where otherwise does.
func AtLive(rect func() (tui.Rect, bool), otherwise Anchor) Anchor {
	otherwise.live = &liveRect{rect: rect}
	return otherwise
}

// AtFraction anchors the float at a rectangle given in percent of the overlay area: x and y its
// top-left corner, w and h its size, each 0 to 100, fractions of a percent included. Unlike AtRect
// it follows a resize of the area: a panel a user placed at the right third stays at the right
// third.
func AtFraction(x, y, w, h float64) Anchor {
	return Anchor{atFraction: true, frac: [4]float64{clampFrac(x), clampFrac(y), clampFrac(w), clampFrac(h)}}
}

// clampFrac is a percentage within 0 to 100; NaN is 0.
func clampFrac(p float64) float64 {
	if !(p > 0) {
		return 0
	}
	return min(p, 100)
}

// FractionRect is where AtFraction(x, y, w, h) puts a float in area: rounded to the nearest cell, at
// least three cells each way, and inside area.
func FractionRect(x, y, w, h float64, area tui.Rect) tui.Rect {
	return fractionRect([4]float64{x, y, w, h}, area)
}

func fractionRect(frac [4]float64, area tui.Rect) tui.Rect {
	pct := func(p float64, of int) int { return int(math.Round(float64(of) * p / 100)) }
	r := tui.Rect{X: area.X + pct(frac[0], area.W), Y: area.Y + pct(frac[1], area.H),
		W: max(pct(frac[2], area.W), minFloatCells), H: max(pct(frac[3], area.H), minFloatCells)}
	return windowBoundsInArea(r, area)
}

// FractionOf is r as AtFraction's percentages of area, to a hundredth of a percent: the inverse of
// where AtFraction puts it, so a rectangle read back and placed again lands on the same cells in an
// area under 10 000 cells across. A one-cell move is never rounded away. Zero when area is empty.
func FractionOf(r, area tui.Rect) (x, y, w, h float64) {
	if area.W <= 0 || area.H <= 0 {
		return 0, 0, 0, 0
	}
	pct := func(v, of int) float64 { return clampFrac(math.Round(float64(v)*10000/float64(of)) / 100) }
	return pct(r.X-area.X, area.W), pct(r.Y-area.Y, area.H), pct(r.W, area.W), pct(r.H, area.H)
}

// minFloatCells is the smallest a fractional float is either way: a border and a cell inside.
const minFloatCells = 3
