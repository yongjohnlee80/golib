package gui

import (
	"image/color"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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
		return t.Indexed(c.Index)
	case tui.CellColorRGB:
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
	}
	if fg {
		return t.FG
	}
	return t.BG
}

// Indexed is the theme's colour for 256-colour index i: its sixteen ANSI colours (0–15), then
// xterm's cube and grey ramp (style.XtermRGB), which no theme overrides.
func (t *Theme) Indexed(i uint8) color.NRGBA {
	if i < 16 {
		return t.ANSI[i]
	}
	return xtermNRGBA(i)
}

// xtermNRGBA is xterm's default colour for index i, opaque.
func xtermNRGBA(i uint8) color.NRGBA {
	c := style.XtermRGB(i)
	return color.NRGBA{R: c[0], G: c[1], B: c[2], A: 0xff}
}

// probed reports c as tui's Capabilities report a terminal's default colour.
func probed(c color.NRGBA) tui.ProbedColor {
	return tui.ProbedColor{R: c.R, G: c.G, B: c.B, Known: true}
}
