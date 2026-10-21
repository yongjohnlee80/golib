// Package sgr is the text attributes' SGR codes (CSI … m), written once: vt parses a program's
// pen with them and term emits a cell's with them. It needs only style, so vt, which sits below
// tui, can read it.
package sgr

import "github.com/yongjohnlee80/golib/tui/style"

// Attrs are the attributes in the order term emits them: each one's code to set it, and the code
// that ends it (22 ends both bold and faint).
var Attrs = []struct {
	On, Off int
	Attr    style.AttrMask
}{
	{1, 22, style.AttrBold}, {2, 22, style.AttrFaint}, {3, 23, style.AttrItalic},
	{4, 24, style.AttrUnderline}, {5, 25, style.AttrBlink}, {7, 27, style.AttrReverse},
	{8, 28, style.AttrInvisible}, {9, 29, style.AttrStrikethrough},
}

// Read-only aliases: 6 is rapid blink, 21 double underline; each is read as the attribute.
var aliases = map[int]style.AttrMask{6: style.AttrBlink, 21: style.AttrUnderline}

// Set is the attribute code turns on.
func Set(code int) (style.AttrMask, bool) {
	if a, ok := aliases[code]; ok {
		return a, true
	}
	for _, a := range Attrs {
		if a.On == code {
			return a.Attr, true
		}
	}
	return 0, false
}

// Clear is the attributes code turns off: 22 is bold and faint together.
func Clear(code int) (style.AttrMask, bool) {
	var m style.AttrMask
	for _, a := range Attrs {
		if a.Off == code {
			m |= a.Attr
		}
	}
	return m, m != 0
}
