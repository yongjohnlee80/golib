package gui

import (
	"strings"
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

// stubFontconfig answers fc-match and fc-list as a system with the given monospace and Nerd Font.
func stubFontconfig(t *testing.T, mono, nerd string) {
	t.Helper()
	old := fontconfig
	t.Cleanup(func() { fontconfig = old })
	fontconfig = func(name string, _ ...string) string {
		switch name {
		case "fc-match":
			return mono
		case "fc-list":
			if nerd == "" {
				return ""
			}
			return nerd + "\nOther Font\n"
		}
		return ""
	}
}

func TestCellTypefaceFollowsTheSystem(t *testing.T) {
	stubFontconfig(t, "JetBrainsMono Nerd Font,JetBrainsMono NF", "CaskaydiaMono Nerd Font Mono,CaskaydiaMono NFM")
	if got, want := cellTypeface(defaultConfig()),
		"JetBrainsMono Nerd Font, JetBrainsMono NF, Go Mono, CaskaydiaMono Nerd Font Mono, CaskaydiaMono NFM, monospace, emoji"; got != want {
		t.Errorf("no font named: %q; want the system's monospace first, then Go Mono, a Nerd Font, the generics: %q", got, want)
	}
	named := defaultConfig()
	WithFont("Iosevka Term", 0)(&named)
	if got, want := cellTypeface(named), "Iosevka Term, CaskaydiaMono Nerd Font Mono, CaskaydiaMono NFM, monospace, emoji"; got != want {
		t.Errorf("a font named: %q; want it first, no system monospace before it: %q", got, want)
	}
	stubFontconfig(t, "CaskaydiaMono Nerd Font Mono,CaskaydiaMono NFM", "CaskaydiaMono Nerd Font Mono,CaskaydiaMono NFM")
	if got := cellTypeface(defaultConfig()); strings.Count(got, "CaskaydiaMono NFM") != 1 {
		t.Errorf("the Nerd Font the monospace already is was added again: %q", got)
	}
	stubFontconfig(t, "", "")
	if got, want := cellTypeface(defaultConfig()), "Go Mono, monospace, emoji"; got != want {
		t.Errorf("no fontconfig: %q; want %q", got, want)
	}
}

// On this machine, the default cells draw a Nerd Font's icons when one is installed: a prompt's
// powerline arrow and branch, a file tree's folder.
func TestTheDefaultCellsDrawNerdIcons(t *testing.T) {
	if nerdFallback() == "" {
		t.Skip("no Nerd Font is installed here")
	}
	s := text.NewShaper(text.WithCollection(gofont.Collection()))
	chain := cellTypeface(defaultConfig())
	for _, icon := range []string{"", "", ""} {
		if glyphless(s, chain, icon) {
			t.Errorf("%U draws as a box in %q", []rune(icon)[0], chain)
		}
	}
}
