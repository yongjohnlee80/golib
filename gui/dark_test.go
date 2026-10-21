package gui

import (
	"image/color"
	"testing"
)

// A theme is dark as tui/style judges darkness (WCAG: white text out-contrasts black): a mid grey
// #777777 is light, #6e6e6e dark, as the terminal backend reads the same backgrounds.
func TestThemeDarknessFollowsStyle(t *testing.T) {
	for _, c := range []struct {
		bg   color.NRGBA
		dark bool
	}{
		{color.NRGBA{0x77, 0x77, 0x77, 0xff}, false},
		{color.NRGBA{0x6e, 0x6e, 0x6e, 0xff}, true},
		{color.NRGBA{0x1e, 0x1e, 0x2e, 0xff}, true},
		{color.NRGBA{0xf8, 0xf8, 0xf8, 0xff}, false},
	} {
		if got := (Theme{BG: c.bg}).dark(); got != c.dark {
			t.Errorf("Theme{BG: %v}.dark() = %v, want %v", c.bg, got, c.dark)
		}
	}
}
