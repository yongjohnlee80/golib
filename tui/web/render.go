package web

import (
	"strconv"

	"github.com/yongjohnlee80/golib/tui"
)

// Metrics are the client's MEASURED font metrics.
//
// The client measures and the server never guesses. Sizes are in device pixels
// rather than `ch` units, because `ch` is itself font-relative and so begs the
// very question being asked — "how wide is a cell in this font?".
type Metrics struct {
	// CellW and CellH are the measured advance and line box, in px.
	CellW, CellH float64
}

// valid reports whether metrics are usable. Zero or negative sizes would
// collapse the grid, so they are refused rather than defaulted: a client that
// could not measure must say so and be told to retry, not be handed a layout
// built on a guess.
func (m Metrics) valid() bool { return m.CellW > 0 && m.CellH > 0 }

// wireColors are a cell's colours and attributes as the client is sent them. Reverse video is
// applied here, as an fg/bg exchange (a CSS filter would invert whatever the theme painted behind
// the cell), and cleared from the mask, so a client cannot apply it twice and undo it. A default
// colour swapped to the other side becomes the theme's own (var(--bg), var(--fg)), so
// default-on-default reverse is still seen.
func wireColors(a tui.CellAttrs) (fg, bg string, mask uint16) {
	f, b := wireColor{c: a.FG}, wireColor{c: a.BG}
	if a.Mask&tui.AttrReverse != 0 {
		f, b = b, f
		if f.c.Kind == tui.CellColorDefault {
			f.token = "bg"
		}
		if b.c.Kind == tui.CellColorDefault {
			b.token = "fg"
		}
	}
	return f.css(), b.css(), uint16(a.Mask &^ tui.AttrReverse)
}

// wireColor is a colour as the client is sent it: a resolved tui colour, or one of the theme's
// own custom properties. It is web's, so no value outside tui's CellColorKind is ever forged.
type wireColor struct {
	c     tui.CellColor
	token string // "fg" or "bg": the theme's colour, by its custom property
}

func (w wireColor) css() string {
	if w.token != "" {
		return "var(--" + w.token + ")"
	}
	return cssColor(w.c)
}

// cssColor renders one resolved color, or "" for the terminal default (which the
// stylesheet already supplies).
func cssColor(c tui.CellColor) string {
	switch c.Kind {
	case tui.CellColorDefault:
		return ""
	case tui.CellColorANSI, tui.CellColorANSI256:
		return "var(--a" + strconv.Itoa(int(c.Index)) + ")"
	case tui.CellColorRGB:
		return "#" + hex2(c.R) + hex2(c.G) + hex2(c.B)
	}
	return ""
}

const hexDigits = "0123456789abcdef"

func hex2(v uint8) string {
	return string([]byte{hexDigits[v>>4], hexDigits[v&0x0f]})
}
