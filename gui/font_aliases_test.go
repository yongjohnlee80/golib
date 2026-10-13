package gui

import (
	"strings"
	"testing"
)

// FONTCONFIG'S OTHER NAMES FOR A FONT.
//
// fontconfig lists a Nerd Font under a long name and a short one ("JetBrainsMono Nerd Font,
// JetBrainsMono NF"), and the shaper's font index knows it by the short one only. Families gives the
// long one, and a host stores it, so every chain handed to the shaper must carry the font's other
// names. These cells answer fontconfig as a system with such fonts, so they do not depend on what
// this machine has installed.

// stubAliasedFonts answers fontconfig as a system whose fonts have long and short names: a family
// listing, a Nerd Font for icons (fc-list :charset=f101), and Noto Sans Mono as the monospace.
func stubAliasedFonts(t *testing.T, nerd string) {
	t.Helper()
	old := fontconfig
	t.Cleanup(func() { fontconfig = old })
	listing := "JetBrainsMono Nerd Font,JetBrainsMono NF\n" +
		"CaskaydiaMono Nerd Font Mono,CaskaydiaMono NFM\n" +
		"CaskaydiaMono Nerd Font Mono,CaskaydiaMono NFM,CaskaydiaMono NFM Light\n" +
		"Symbols Nerd Font Mono\nAdwaita Mono\nNoto Sans Mono\n"
	fontconfig = func(name string, args ...string) string {
		switch {
		case name == "fc-match":
			return "Noto Sans Mono"
		case name == "fc-list" && len(args) > 0 && strings.HasPrefix(args[0], ":charset="):
			return nerd
		case name == "fc-list":
			return listing
		}
		return ""
	}
}

// TestAFamilyReachesTheShaperByEveryName: a family named by its long name carries its short one,
// and a weight's own name is left out; a family fontconfig does not list stays as it is.
func TestAFamilyReachesTheShaperByEveryName(t *testing.T) {
	stubAliasedFonts(t, "")
	for _, tc := range []struct{ in, want string }{
		{"JetBrainsMono Nerd Font", "JetBrainsMono Nerd Font, JetBrainsMono NF"},
		{"CaskaydiaMono Nerd Font Mono, Go Mono", "CaskaydiaMono Nerd Font Mono, CaskaydiaMono NFM, Go Mono"},
		{"JetBrainsMono NF", "JetBrainsMono NF, JetBrainsMono Nerd Font"},
		{"Go Mono, monospace", "Go Mono, monospace"},
	} {
		if got := withAliases(tc.in); got != tc.want {
			t.Errorf("withAliases(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTheCellFontByItsLongNameIsTheFont: the window's cells in "JetBrainsMono Nerd Font", as a host's
// dropdown stores it, reach the shaper with its short name too; that font has the icons, so no other
// Nerd Font is added for them.
func TestTheCellFontByItsLongNameIsTheFont(t *testing.T) {
	stubAliasedFonts(t, "JetBrainsMono Nerd Font,JetBrainsMono NF\n")
	c := defaultConfig()
	WithFont("JetBrainsMono Nerd Font", 0)(&c)
	if got, want := cellTypeface(c), "JetBrainsMono Nerd Font, JetBrainsMono NF, monospace, emoji"; got != want {
		t.Errorf("cellTypeface = %q, want %q", got, want)
	}
	b := NewBackend()
	b.SetFont("JetBrainsMono Nerd Font", 0)
	if got := b.font.Load().typeface; !namesFamily(got, "JetBrainsMono NF") {
		t.Errorf("SetFont's chain %q lacks the short name", got)
	}
}

// TestTheTerminalsFacesFallBackAsTheCellsDo: a cell face (SetCellFaces, a terminal's own font) by its
// long name reaches the shaper by both names; then a Nerd Font for icons, which a face font without
// them (Adwaita Mono) still draws; then monospace and emoji.
func TestTheTerminalsFacesFallBackAsTheCellsDo(t *testing.T) {
	stubAliasedFonts(t, "Symbols Nerd Font Mono\n")
	b := NewBackend()
	b.SetCellFaces([]string{"", "CaskaydiaMono Nerd Font Mono", "Adwaita Mono"})
	for i, want := range []string{"",
		"CaskaydiaMono Nerd Font Mono, CaskaydiaMono NFM, Symbols Nerd Font Mono, monospace, emoji",
		"Adwaita Mono, Symbols Nerd Font Mono, monospace, emoji"} {
		if got := string(b.render.faces[i]); got != want {
			t.Errorf("face %d: %q, want %q", i, got, want)
		}
	}
}

// TestTheProseFontByItsLongNameIsTheFont: the native views' prose family, named by its long name,
// reaches the shaper by both names.
func TestTheProseFontByItsLongNameIsTheFont(t *testing.T) {
	stubAliasedFonts(t, "")
	b := NewBackend()
	b.SetProseFont("JetBrainsMono Nerd Font")
	if got, want := b.render.fonts.Prose, "JetBrainsMono Nerd Font, JetBrainsMono NF"; got != want {
		t.Errorf("prose %q, want %q", got, want)
	}
}
