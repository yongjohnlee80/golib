package gui

import (
	"image/color"

	"github.com/yongjohnlee80/golib/tui"
)

// ANSI 256-Color Palette Resolution:
//
//	Indices:
//	  0  .. 15  : System ANSI Palette (Configured via Theme.ANSI)
//	 16  .. 231 : 6x6x6 RGB Color Cube (levels: 0, 95, 135, 175, 215, 255)
//	232  .. 255 : Grayscale Ramp (24 steps from dark gray to near-white)
//
// resolve translates a cell's symbolic or indexed [tui.CellColor] into a concrete physical
// [color.NRGBA] ready for GPU rasterization. When the cell specifies default colors, fg selects
// between Theme.FG and Theme.BG.
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

// indexed maps an 8-bit color index to the canonical xterm 256-color palette.
// Indices 0-15 map to Theme.ANSI; indices 16-231 map to the standard 6x6x6 RGB color cube;
// indices 232-255 map to a linear 24-step monochrome ramp.
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
