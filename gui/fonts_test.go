package gui

import (
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

func TestFallbackChain(t *testing.T) {
	cases := map[string]string{
		"Go Mono":                    "Go Mono, monospace, emoji",
		"CaskaydiaMono NFM, Go Mono": "CaskaydiaMono NFM, Go Mono, monospace, emoji",
		"Go Mono, Monospace":         "Go Mono, Monospace, emoji",
		`"Iosevka Term", 'emoji'`:    `"Iosevka Term", 'emoji', monospace`,
		"":                           "monospace, emoji",
		"Go Mono, monospace, emoji":  "Go Mono, monospace, emoji",
	}
	for in, want := range cases {
		if got := fallbackChain(in); got != want {
			t.Errorf("fallbackChain(%q) = %q; want %q", in, got, want)
		}
	}
}

// glyphless reports whether s shapes, in typeface, to a glyph the font does not define. Gio packs
// a glyph id as face<<48 | ppem<<32 | glyph (gioui.org v0.10.3); glyph 0 is .notdef, the box.
func glyphless(s *text.Shaper, typeface, str string) bool {
	s.LayoutString(text.Parameters{Font: fontOf(typeface), PxPerEm: fixed.I(16), MaxWidth: 1 << 20}, str)
	missing := false
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		if g.Runes > 0 && uint64(g.ID)&0xffff == 0 {
			missing = true
		}
	}
	return missing
}

// The cell font reaches the system's fonts for what Go Mono lacks: the menus' submenu marker,
// symbols, Hangul and colour emoji draw as glyphs, not boxes. A character no installed font has
// (a bare CI runner) is skipped rather than failed: the chain cannot invent a font.
func TestCellFontFallsBackToSystemFonts(t *testing.T) {
	s := text.NewShaper(text.WithCollection(gofont.Collection()))
	chain := fallbackChain(defaultConfig().typeface)
	for _, str := range []string{"▸", "▾", "✓", "⚠️", "한", "😀"} {
		if glyphless(s, "monospace, emoji", str) {
			t.Logf("%q: no installed font has it; skipped", str)
			continue
		}
		if glyphless(s, chain, str) {
			t.Errorf("%q draws as a box in %q, though an installed font has it", str, chain)
		}
	}
}
