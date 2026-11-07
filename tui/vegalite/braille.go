package vegalite

// THE BRAILLE CANVAS — a line, point or area chart in a terminal's cells: each cell is a 2×4 grid
// of dots (U+2800–U+28FF), so a W×H room plots at 2W×4H. Dots are set by series; a cell takes the
// colour of the last series that set a dot in it (a cell holds one colour), and the drawing that
// reads a cell asks which series that was. A rule (a mark of its own, or an axis) is drawn over the
// dots as a whole cell, ─ or │.

// brailleBase is the blank Braille pattern; a dot's bit is added to it.
const brailleBase = 0x2800

// dotBits are the eight dots' bits by their place in a cell: [column][row], column 0 or 1, row 0
// (top) to 3 (bottom), as Unicode numbers them (dots 1-2-3-7 down the left, 4-5-6-8 down the right).
var dotBits = [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// canvas is a W×H cell room plotted in dots. Its dot space has x to the right and y downward,
// 0 ≤ x < 2W, 0 ≤ y < 4H; a dot outside it is dropped, never wrapped.
type canvas struct {
	w, h   int
	dots   []uint8 // a cell's dot bits, by y*w+x
	series []int   // the series that last set a dot in the cell, -1 for none
	rules  []rune  // a rule drawn over the cell (─ │ ┼), 0 for none
	ruleOf []int   // the rule's series
}

func newCanvas(w, h int) *canvas {
	w, h = max(w, 0), max(h, 0)
	c := &canvas{w: w, h: h, dots: make([]uint8, w*h), series: make([]int, w*h), rules: make([]rune, w*h), ruleOf: make([]int, w*h)}
	for i := range c.series {
		c.series[i], c.ruleOf[i] = -1, -1
	}
	return c
}

// dotsW and dotsH are the dot space's size.
func (c *canvas) dotsW() int { return 2 * c.w }
func (c *canvas) dotsH() int { return 4 * c.h }

// set sets dot (x, y) for series s.
func (c *canvas) set(x, y, s int) {
	if x < 0 || y < 0 || x >= c.dotsW() || y >= c.dotsH() {
		return
	}
	i := (y/4)*c.w + x/2
	c.dots[i] |= dotBits[x%2][y%4]
	c.series[i] = s
}

// line sets the dots from (x0, y0) to (x1, y1), both ends included, for series s (Bresenham).
func (c *canvas) line(x0, y0, x1, y1, s int) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	e := dx + dy
	for {
		c.set(x0, y0, s)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

// fillDown sets every dot of column x from y down to base, both included, for series s: an
// area's fill under its line.
func (c *canvas) fillDown(x, y, base, s int) {
	lo, hi := min(y, base), max(y, base)
	for yy := lo; yy <= hi; yy++ {
		c.set(x, yy, s)
	}
}

// rule draws a rule over cell (cx, cy): r is ─ or │, and a rule crossing one of the other kind
// makes ┼.
func (c *canvas) rule(cx, cy int, r rune, s int) {
	if cx < 0 || cy < 0 || cx >= c.w || cy >= c.h {
		return
	}
	i := cy*c.w + cx
	if old := c.rules[i]; old != 0 && old != r {
		r = '┼'
	}
	c.rules[i], c.ruleOf[i] = r, s
}

// at is cell (cx, cy) as drawn: its rune, and the series whose colour it wears (-1: none, a blank
// cell). A rule wins over the dots under it.
func (c *canvas) at(cx, cy int) (rune, int) {
	i := cy*c.w + cx
	if r := c.rules[i]; r != 0 {
		return r, c.ruleOf[i]
	}
	if c.dots[i] == 0 {
		return ' ', -1
	}
	return rune(brailleBase + int(c.dots[i])), c.series[i]
}

// rows are the canvas as text, a row a line: what a golden reads.
func (c *canvas) rows() []string {
	out := make([]string, c.h)
	for y := range c.h {
		rs := make([]rune, c.w)
		for x := range c.w {
			rs[x], _ = c.at(x, y)
		}
		out[y] = string(rs)
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
