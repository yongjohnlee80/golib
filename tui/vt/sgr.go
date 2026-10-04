package vt

import (
	"github.com/yongjohnlee80/golib/tui/internal/vtparse"
	"github.com/yongjohnlee80/golib/tui/style"
)

// sgr applies Select Graphic Rendition to the pen. Colours take xterm's
// three forms: 38;5;n and 38;2;r;g;b as separate parameters, and the
// colon forms 38:5:n, 38:2:r:g:b and 38:2:cs:r:g:b in one.
func (s *Screen) sgr(a *vtparse.Action) {
	pen := &s.cur.pen
	if len(a.Params) == 0 {
		*pen = Cell{}
		return
	}
	for i := 0; i < len(a.Params); i++ {
		switch v := a.Param(i, 0); {
		case v == 0:
			*pen = Cell{}
		case v == 1:
			pen.Attr |= AttrBold
		case v == 2:
			pen.Attr |= AttrFaint
		case v == 3:
			pen.Attr |= AttrItalic
		case v == 4:
			// 4:0 is "no underline"; 4:1..4:5 are underline styles.
			if a.Sub(i, 1, 1) == 0 {
				pen.Attr &^= AttrUnderline
			} else {
				pen.Attr |= AttrUnderline
			}
		case v == 5 || v == 6:
			pen.Attr |= AttrBlink
		case v == 7:
			pen.Attr |= AttrReverse
		case v == 8:
			pen.Attr |= AttrInvisible
		case v == 9:
			pen.Attr |= AttrStrike
		case v == 21:
			pen.Attr |= AttrUnderline // double underline
		case v == 22:
			pen.Attr &^= AttrBold | AttrFaint
		case v == 23:
			pen.Attr &^= AttrItalic
		case v == 24:
			pen.Attr &^= AttrUnderline
		case v == 25:
			pen.Attr &^= AttrBlink
		case v == 27:
			pen.Attr &^= AttrReverse
		case v == 28:
			pen.Attr &^= AttrInvisible
		case v == 29:
			pen.Attr &^= AttrStrike
		case v >= 30 && v <= 37:
			pen.FG = style.ANSI(v - 30)
		case v == 38:
			c, n, ok := extColor(a, i)
			if ok {
				pen.FG = c
			}
			i += n
		case v == 39:
			pen.FG = style.Color{}
		case v >= 40 && v <= 47:
			pen.BG = style.ANSI(v - 40)
		case v == 48:
			c, n, ok := extColor(a, i)
			if ok {
				pen.BG = c
			}
			i += n
		case v == 49:
			pen.BG = style.Color{}
		case v == 58:
			_, n, _ := extColor(a, i) // underline colour: consumed, not kept
			i += n
		case v >= 90 && v <= 97:
			pen.FG = style.ANSI(v - 90 + 8)
		case v >= 100 && v <= 107:
			pen.BG = style.ANSI(v - 100 + 8)
		}
	}
}

// extColor reads an extended colour at parameter i: the colon form from
// i's own sub-parameters, else the semicolon form from the parameters after
// it. skip is how many parameters after i the colour used.
func extColor(a *vtparse.Action, i int) (c style.Color, skip int, ok bool) {
	if len(a.Params[i].Parts) > 1 {
		parts := len(a.Params[i].Parts)
		switch a.Sub(i, 1, -1) {
		case 5:
			return ansi256(a.Sub(i, 2, -1))
		case 2:
			// 38:2:r:g:b, or 38:2:cs:r:g:b with a colour-space id.
			first := 2
			if parts >= 6 {
				first = 3
			}
			return rgb(a.Sub(i, first, 0), a.Sub(i, first+1, 0), a.Sub(i, first+2, 0))
		}
		return style.Color{}, 0, false
	}
	switch a.Param(i+1, -1) {
	case 5:
		c, _, ok := ansi256(a.Param(i+2, -1))
		return c, min(2, len(a.Params)-1-i), ok
	case 2:
		c, _, ok := rgb(a.Param(i+2, 0), a.Param(i+3, 0), a.Param(i+4, 0))
		return c, min(4, len(a.Params)-1-i), ok
	}
	return style.Color{}, min(1, len(a.Params)-1-i), false
}

func ansi256(n int) (style.Color, int, bool) {
	if n < 0 || n > 255 {
		return style.Color{}, 0, false
	}
	return style.ANSI256(n), 0, true
}

func rgb(r, g, b int) (style.Color, int, bool) {
	clamp := func(v int) uint8 { return uint8(min(max(v, 0), 255)) }
	return style.RGB(clamp(r), clamp(g), clamp(b)), 0, true
}
