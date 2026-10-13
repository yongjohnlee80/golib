package widget

import (
	"image/color"
	"math"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Widget Color Resolution Cascade:
//
//	[ style.Color ]
//	       |
//	       +---> 1. Semantic Theme Token (th.Color(tok))
//	       +---> 2. Adaptive Dark/Light Pair (dark leaf selection)
//	       +---> 3. Concrete 24-bit TrueColor RGB
//	       +---> 4. Indexed ANSI 16 / 256 Palette
//	       |
//	       v
//	color.NRGBA (GPU Rasterization Ready)
//
// colorOf resolves style.Color specifications into concrete GPU color.NRGBA values.
// Supports theme semantic tokens, adaptive light/dark pairs, direct 24-bit RGB, and ANSI indices.
func colorOf(c style.Color, th *style.Theme, dark bool) (color.NRGBA, bool) {
	if tok, ok := c.Token(); ok {
		if th == nil {
			return color.NRGBA{}, false
		}
		c = th.Color(tok)
	}
	if light, darkLeaf, ok := c.AdaptivePair(); ok {
		c = light
		if dark {
			c = darkLeaf
		}
	}
	if c.IsDefault() {
		return color.NRGBA{}, false
	}
	if r, g, b, ok := c.RGBValues(); ok {
		return color.NRGBA{R: r, G: g, B: b, A: 0xff}, true
	}
	pal := gui.DefaultTheme().ANSI
	if n, ok := c.ANSIIndex(); ok && n < 16 {
		return pal[n], true
	}
	if n, ok := c.ANSI256Index(); ok {
		if n < 16 {
			return pal[n], true
		}
		return ansi256(uint8(n)), true
	}
	return color.NRGBA{}, false
}

// ansi256 is xterm's colour n past the sixteen: the 6×6×6 cube, then the grey ramp.
func ansi256(n uint8) color.NRGBA {
	if n >= 232 {
		v := 8 + (n-232)*10
		return color.NRGBA{R: v, G: v, B: v, A: 0xff}
	}
	n -= 16
	level := func(v uint8) uint8 {
		if v == 0 {
			return 0
		}
		return 55 + v*40
	}
	return color.NRGBA{R: level(n / 36), G: level(n / 6 % 6), B: level(n % 6), A: 0xff}
}

// openColors is the colours of the first cell of the view's rows [0, rows) that nothing covers
// (rows < 0: all of them), and whether one is open. The cells are the composed screen: a raised
// panel over a view's corner would lend the view its colours (gui.OpenCellColors).
func openColors(c gui.Canvas, rows int) (fg, bg color.NRGBA, ok bool) {
	cols, all := 1, 1
	if cs := c.CellSize(); cs.W > 0 && cs.H > 0 {
		sz := c.Size()
		cols = max(int(math.Round(float64(sz.W/cs.W))), 1)
		all = max(int(math.Round(float64(sz.H/cs.H))), 1)
	}
	if rows < 0 || rows > all {
		rows = all
	}
	return gui.OpenCellColors(c, gui.CellRect{W: cols, H: rows})
}

// isDark reports whether c is a dark background, by relative luminance.
func isDark(c color.NRGBA) bool {
	return 0.2126*float64(c.R)+0.7152*float64(c.G)+0.0722*float64(c.B) < 0.5*255
}

// mix is a over b by t in [0, 1]: a tint of b toward a.
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float32(x)*t + float32(y)*(1-t)) }
	return color.NRGBA{R: f(a.R, b.R), G: f(a.G, b.G), B: f(a.B, b.B), A: 0xff}
}

// contrast is how far apart two colours read: the difference of their luminance.
func contrast(a, b color.NRGBA) float64 {
	l := func(c color.NRGBA) float64 { return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B) }
	d := l(a) - l(b)
	if d < 0 {
		return -d
	}
	return d
}
