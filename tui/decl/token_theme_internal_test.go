package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// The token theme pairs each surface with the text meant for it: the foreground is the page's
// text, drawn on the page's base (the background, and a popup's surface), never the window's
// text on the terminal's default. Mono's window text (black) on a dark terminal was unreadable.
func TestTheTokenThemePairsItsSurfacesAndText(t *testing.T) {
	mono := palette{
		roleHighlight: style.ANSI(0), roleWindow: style.ANSI(7), roleWindowText: style.ANSI(0),
		roleBase: style.Default(), roleText: style.Default(),
	}
	th, ok := tokenTheme(mono)
	if !ok {
		t.Fatal("no theme")
	}
	if fg := th.Color(style.TokenForeground); fg != style.Default() {
		t.Errorf("mono's foreground %v, want the page's text (the terminal's default), not the window's black", fg)
	}
	if bg, panel := th.Color(style.TokenBackground), th.Color(style.TokenPanel); bg != style.Default() || panel != style.Default() {
		t.Errorf("mono's background %v and panel %v, want the page's base", bg, panel)
	}

	paper, ink := style.RGB(0xe6, 0xd9, 0xb9), style.RGB(0x5b, 0x46, 0x36)
	sepia := palette{
		roleHighlight: style.RGB(0x8a, 0x5a, 0x2b), roleWindow: style.RGB(0xda, 0xcc, 0xa9), roleWindowText: style.RGB(0x4a, 0x38, 0x26),
		roleBase: paper, roleText: ink,
	}
	th, _ = tokenTheme(sepia)
	for _, surface := range []style.Token{style.TokenBackground, style.TokenSurface, style.TokenPanel} {
		if r, ok := style.ContrastRatio(th.Color(style.TokenForeground), th.Color(surface)); !ok || r < 4.5 {
			t.Errorf("sepia's foreground on token %d reads at %.2f:1, want the page's text on the page", surface, r)
		}
	}
	if b := th.Color(style.TokenBoost); b == paper {
		t.Error("the boost (a scrim's surface) is the page itself: what is behind a dialog would not dim")
	} else if r, g, bl, ok := b.RGBValues(); !ok || r >= 0xe6 || g >= 0xd9 || bl >= 0xb9 {
		t.Errorf("the boost %v is not the page darkened", b)
	}
}
