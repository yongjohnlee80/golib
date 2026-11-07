package vegalite

import "unicode"

// ApproxMeasure measures without a shaper, for a build that has no fonts loaded: an exported
// page's chart, written as SVG, which the browser draws in its own sans-serif anyway. Each
// printable ASCII character takes Helvetica's advance (Arial's are the same), a wide East Asian
// character a whole em, a combining mark nothing, and anything else an average letter's; bold is
// a little wider. Labels are placed by these widths, so they land within a few percent of where
// a browser draws them.
func ApproxMeasure(s string, f Font) float32 {
	var em float32 // in thousandths of an em
	for _, r := range s {
		switch {
		case r >= ' ' && r <= '~':
			em += float32(helvetica[r-' '])
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == '​' || r == '‍':
		case wideRune(r):
			em += 1000
		default:
			em += 556
		}
	}
	w := em / 1000 * f.Size
	if f.Bold {
		w *= 1.06
	}
	return w
}

// helvetica are Helvetica's advances, in thousandths of an em, from ' ' to '~'.
var helvetica = [95]uint16{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, // ' ' to '/'
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, // '0' to '9'
	278, 278, 584, 584, 584, 556, 1015, // ':' to '@'
	667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, // 'A' to 'M'
	722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, // 'N' to 'Z'
	278, 278, 278, 469, 556, 333, // '[' to '`'
	556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, // 'a' to 'm'
	556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, // 'n' to 'z'
	334, 260, 334, 584, // '{' to '~'
}

// wideRune is a character a font draws a whole em wide: Hangul, CJK, kana, fullwidth forms.
func wideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f, // Hangul Jamo
		r >= 0x2e80 && r <= 0xa4cf && r != 0x303f, // CJK radicals to Yi
		r >= 0xac00 && r <= 0xd7a3,                // Hangul syllables
		r >= 0xf900 && r <= 0xfaff,                // CJK compatibility ideographs
		r >= 0xfe30 && r <= 0xfe4f,                // CJK compatibility forms
		r >= 0xff00 && r <= 0xff60,                // fullwidth forms
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1faff, // emoji
		r >= 0x20000 && r <= 0x3fffd: // CJK extensions
		return true
	}
	return false
}
