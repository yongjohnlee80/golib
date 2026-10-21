package style_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// Legible raises a colour to the ratio asked against a light or a dark background, keeps one that
// already reads as written, and leaves a colour it cannot read (the terminal's default) alone.
func TestLegible(t *testing.T) {
	sepia, dark := style.RGB(0xe6, 0xd9, 0xb9), style.RGB(0x1c, 0x1c, 0x1c)
	for _, tc := range []struct {
		name   string
		fg, bg style.Color
	}{
		{"yellow on sepia", style.ANSI(3), sepia},
		{"bright green on sepia", style.ANSI(10), sepia},
		{"blue on dark", style.ANSI(4), dark},
		{"a 256-colour grey on dark", style.ANSI256(236), dark},
	} {
		got := style.Legible(tc.fg, tc.bg, 4.5)
		if r, ok := style.ContrastRatio(got, tc.bg); !ok || r < 4.5 {
			t.Errorf("%s: raised to %v, ratio %.2f, want 4.5", tc.name, got, r)
		}
	}
	if got := style.Legible(style.ANSI(0), sepia, 4.5); got != style.ANSI(0) {
		t.Errorf("a black that reads on sepia became %v", got)
	}
	if got := style.Legible(style.Default(), sepia, 4.5); got != style.Default() {
		t.Errorf("the terminal's default colour became %v", got)
	}
	if got := style.Legible(style.ANSI(3), sepia, 0); got != style.ANSI(3) {
		t.Errorf("off, yellow became %v", got)
	}
}

// Legible reaches the ratio asked wherever an extreme can: on a mid grey, light grey must go to
// near-black, and a fixed number of steps fell short (4.43:1 for 4.5). Where no colour can reach
// it, the extreme that reads best comes back.
func TestLegibleReachesTheRatioOrTheExtreme(t *testing.T) {
	grey := style.RGB(118, 118, 118)
	got := style.Legible(style.RGB(200, 200, 200), grey, 4.5)
	if r, _ := style.ContrastRatio(got, grey); r < 4.5 {
		t.Errorf("light grey on mid grey raised to %v at %.4f:1, want 4.5", got, r)
	}
	for _, bg := range []style.Color{grey, style.RGB(0x30, 0x30, 0x30), style.RGB(0xe6, 0xd9, 0xb9)} {
		for _, fg := range []style.Color{style.ANSI(3), style.ANSI(4), style.RGB(128, 128, 128), style.ANSI256(250)} {
			for _, want := range []float64{3, 4.5, 7} {
				out := style.Legible(fg, bg, want)
				r, _ := style.ContrastRatio(out, bg)
				if r >= want {
					continue
				}
				best := max(ratioAgainst(t, style.RGB(0, 0, 0), bg), ratioAgainst(t, style.RGB(255, 255, 255), bg))
				if r < best-1e-9 {
					t.Errorf("%v on %v for %v: %.4f:1, below both the ratio and the best extreme's %.4f", fg, bg, want, r, best)
				}
			}
		}
	}
	// 21:1 is reachable only at the extremes, on them: what can be reached is
	if r, _ := style.ContrastRatio(style.Legible(style.RGB(90, 90, 90), grey, 21), grey); r < 1 {
		t.Error("an unreachable ratio gave no colour")
	}
}

func ratioAgainst(t *testing.T, a, b style.Color) float64 {
	t.Helper()
	r, ok := style.ContrastRatio(a, b)
	if !ok {
		t.Fatal("not concrete")
	}
	return r
}

// RelativeLuminance runs from black's 0 to white's 1, and Dark turns at the grey where white and
// black text have equal contrast: #757575 is dark (white reads better), #767676 light.
func TestDarkTurnsWhereWhiteAndBlackContrastEqually(t *testing.T) {
	if l := style.RelativeLuminance(0, 0, 0); l != 0 {
		t.Errorf("black's luminance %v", l)
	}
	if l := style.RelativeLuminance(255, 255, 255); l < 0.9999 || l > 1.0001 {
		t.Errorf("white's luminance %v", l)
	}
	for _, c := range []struct {
		v    uint8
		dark bool
	}{{0x00, true}, {0x75, true}, {0x76, false}, {0xff, false}} {
		if got := style.Dark(c.v, c.v, c.v); got != c.dark {
			t.Errorf("#%02x%02x%02x: Dark %v, want %v", c.v, c.v, c.v, got, c.dark)
		}
	}
}
