package widget

import (
	"encoding/binary"
	"hash/fnv"
	"image/color"

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
// Supports theme semantic tokens, adaptive light/dark pairs, direct 24-bit RGB, and palette
// indices, which pal answers: the window's theme (gui.PaletteOf), so a view draws a palette
// colour as the cells beside it are drawn. A nil pal is the default theme's.
func colorOf(c style.Color, th *style.Theme, dark bool, pal gui.Palette) (color.NRGBA, bool) {
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
	if pal == nil {
		pal = gui.PaletteOf(nil)
	}
	if n, ok := c.ANSIIndex(); ok && n < 16 {
		return pal.Indexed(uint8(n)), true
	}
	if n, ok := c.ANSI256Index(); ok {
		return pal.Indexed(uint8(n)), true
	}
	return color.NRGBA{}, false
}

// openColors is the colours of the first cell of the view's rows [0, rows) that nothing covers
// (rows < 0: all of them), and whether one is open. The cells are the composed screen: a raised
// panel over a view's corner would lend the view its colours (gui.OpenCellColors).
func openColors(c gui.Canvas, rows int) (fg, bg color.NRGBA, ok bool) {
	cols, all := gui.CanvasCells(c)
	cols, all = max(cols, 1), max(all, 1)
	if rows < 0 || rows > all {
		rows = all
	}
	return gui.OpenCellColors(c, gui.CellRect{W: cols, H: rows})
}

// isDark reports whether c is a dark background, as tui/style judges it (style.Dark).
func isDark(c color.NRGBA) bool { return style.Dark(c.R, c.G, c.B) }

// mix is a tint of b toward a by t in [0, 1], always opaque: gui.Mix (the one blend) with its alpha
// made whole, since a theme's colours may be unset (zero) and a tint is always drawn.
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	c := gui.Mix(b, a, t)
	c.A = 0xff
	return c
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

// defaultAccent is the accent a view draws with when no tui theme names one.
var defaultAccent = color.NRGBA{R: 0x5c, G: 0x9c, B: 0xf5, A: 0xff}

// pageLook is a view's look, read from the window: text and ground, the text size, and the accent
// and muted text. key hashes all of it with the font generation, so a change in any of them (a
// family change included) is a new key, and the view restyles.
type pageLook struct {
	fg, bg, accent, muted color.NRGBA
	px                    float32
	key                   uint64
}

// readLook reads the look of a view drawn on c: the text and ground from the cells under it that
// nothing covers (prevFg and prevBg when it is wholly covered, black on white when there are none),
// the text size (10 at least), and the accent and muted text from th when it names them.
func readLook(c gui.Canvas, th *style.Theme, prevFg, prevBg color.NRGBA) pageLook {
	fg, bg, ok := openColors(c, -1)
	if !ok {
		fg, bg = prevFg, prevBg // wholly covered: the look it had
	}
	if fg.A == 0 && bg.A == 0 {
		fg, bg = color.NRGBA{A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	lk := pageLook{fg: fg, bg: bg, px: max(c.TextSize(), 10), accent: defaultAccent, muted: mix(fg, bg, 0.55)}
	if th != nil {
		dark, pal := isDark(bg), gui.PaletteOf(c)
		if col, ok := colorOf(th.Color(style.TokenAccent), th, dark, pal); ok {
			lk.accent = col
		}
		if col, ok := colorOf(th.Color(style.TokenTextMuted), th, dark, pal); ok {
			lk.muted = col
		}
	}
	h := fnv.New64a()
	for _, col := range []color.NRGBA{lk.fg, lk.bg, lk.accent, lk.muted} {
		_, _ = h.Write([]byte{col.R, col.G, col.B, col.A})
	}
	_ = binary.Write(h, binary.LittleEndian, lk.px)
	_ = binary.Write(h, binary.LittleEndian, c.Fonts().Gen)
	lk.key = h.Sum64()
	return lk
}
