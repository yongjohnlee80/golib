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
