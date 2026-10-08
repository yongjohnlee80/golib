package style

import "math"

// LEGIBLE COLOURS — a colour raised to a contrast ratio against a background, for content whose
// colours were written for another background: a terminal program's, written for a dark
// terminal, on a light theme's page.

// Legible is fg raised to ratio (WCAG's contrast ratio, 1 to 21) against bg: taken toward black on
// a light bg, or white on a dark one, in small steps until it reads, its hue kept. fg comes back
// as it was written when it already reads, or when either colour is not a concrete one (a token,
// the terminal's default). An ANSI or 256-colour index is read as xterm's.
func Legible(fg, bg Color, ratio float64) Color {
	if ratio <= 1 {
		return fg
	}
	f, ok := rgbOf(fg)
	if !ok {
		return fg
	}
	b, ok := rgbOf(bg)
	if !ok {
		return fg
	}
	if r, g, bl, moved := raiseContrast(f, b, ratio); moved {
		return RGB(r, g, bl)
	}
	return fg
}

// ContrastRatio is WCAG's contrast ratio of two concrete colours; ok is false when either is not
// one.
func ContrastRatio(a, b Color) (ratio float64, ok bool) {
	x, ok1 := rgbOf(a)
	y, ok2 := rgbOf(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	return contrastRatio(luminance(x), luminance(y)), true
}

// rgbOf is a colour's RGB: an RGB colour's own, an ANSI or 256-colour index's in xterm's palette.
func rgbOf(c Color) ([3]uint8, bool) {
	if r, g, b, ok := c.RGBValues(); ok {
		return [3]uint8{r, g, b}, true
	}
	if i, ok := c.ANSIIndex(); ok {
		return xterm16[i], true
	}
	if i, ok := c.ANSI256Index(); ok {
		if i < 16 {
			return xterm16[i], true
		}
		if i < 232 {
			i -= 16
			level := func(v int) uint8 {
				if v == 0 {
					return 0
				}
				return uint8(55 + v*40)
			}
			return [3]uint8{level(i / 36), level(i / 6 % 6), level(i % 6)}, true
		}
		v := uint8(8 + (i-232)*10)
		return [3]uint8{v, v, v}, true
	}
	return [3]uint8{}, false
}

// xterm16 is xterm's sixteen ANSI colours.
var xterm16 = [16][3]uint8{
	{0x00, 0x00, 0x00}, {0xcd, 0x00, 0x00}, {0x00, 0xcd, 0x00}, {0xcd, 0xcd, 0x00},
	{0x00, 0x00, 0xee}, {0xcd, 0x00, 0xcd}, {0x00, 0xcd, 0xcd}, {0xe5, 0xe5, 0xe5},
	{0x7f, 0x7f, 0x7f}, {0xff, 0x00, 0x00}, {0x00, 0xff, 0x00}, {0xff, 0xff, 0x00},
	{0x5c, 0x5c, 0xff}, {0xff, 0x00, 0xff}, {0x00, 0xff, 0xff}, {0xff, 0xff, 0xff},
}

// raiseContrast takes f toward black (on a light b) or white (on a dark b) in small steps until
// it reads at ratio against b; moved is false when it already did.
func raiseContrast(f, b [3]uint8, ratio float64) (r, g, bl uint8, moved bool) {
	lb := luminance(b)
	if contrastRatio(luminance(f), lb) >= ratio {
		return f[0], f[1], f[2], false
	}
	toward := 0.0 // black, for a light background
	if lb < 0.18 {
		toward = 255
	}
	c := [3]float64{float64(f[0]), float64(f[1]), float64(f[2])}
	for range 40 {
		for i := range c {
			c[i] += (toward - c[i]) * 0.08
		}
		q := [3]uint8{uint8(math.Round(c[0])), uint8(math.Round(c[1])), uint8(math.Round(c[2]))}
		if contrastRatio(luminance(q), lb) >= ratio {
			return q[0], q[1], q[2], true
		}
	}
	return uint8(math.Round(c[0])), uint8(math.Round(c[1])), uint8(math.Round(c[2])), true
}

// luminance is WCAG's relative luminance.
func luminance(c [3]uint8) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

// contrastRatio is WCAG's contrast ratio of two luminances.
func contrastRatio(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}
