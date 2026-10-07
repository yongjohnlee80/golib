package gui

import (
	"image/color"

	"github.com/yongjohnlee80/golib/tui"
)

// resolve turns a cell's resolved colour into the colour drawn. fg says which default applies to
// CellColorDefault, the terminal's own colours, which a window takes from its Theme.
func (t *Theme) resolve(c tui.CellColor, fg bool) color.NRGBA {
	switch c.Kind {
	case tui.CellColorANSI, tui.CellColorANSI256:
		return t.indexed(c.Index)
	case tui.CellColorRGB:
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
	}
	if fg {
		return t.FG
	}
	return t.BG
}

// indexed is xterm's 256-colour palette: the theme's sixteen, a 6×6×6 cube, a 24-step grey ramp.
func (t *Theme) indexed(i uint8) color.NRGBA {
	switch {
	case i < 16:
		return t.ANSI[i]
	case i < 232:
		i -= 16
		level := func(v uint8) uint8 {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return color.NRGBA{R: level(i / 36), G: level(i / 6 % 6), B: level(i % 6), A: 0xff}
	default:
		v := 8 + (i-232)*10
		return color.NRGBA{R: v, G: v, B: v, A: 0xff}
	}
}

// probed reports c as tui's Capabilities report a terminal's default colour.
func probed(c color.NRGBA) tui.ProbedColor {
	return tui.ProbedColor{R: c.R, G: c.G, B: c.B, Known: true}
}
