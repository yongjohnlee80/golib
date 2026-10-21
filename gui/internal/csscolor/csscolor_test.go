package csscolor

import (
	"image/color"
	"testing"
)

// A fractional channel rounds to the nearest byte, as CSS rounds it: an alpha of .5 is 128, a
// channel of 50% is 128, a channel of 10.6 is 11.
func TestParseRoundsFractionalChannels(t *testing.T) {
	cur := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	for in, want := range map[string]color.NRGBA{
		"rgba(0, 0, 0, .5)":   {A: 128},
		"rgba(0, 0, 0, 50%)":  {A: 128},
		"rgb(50%, 0%, 100%)":  {R: 128, B: 255, A: 255},
		"rgb(10.6, 0.4, 255)": {R: 11, B: 255, A: 255},
		"Navy":                {B: 128, A: 255},
		"currentColor":        cur,
		"#0000ff80":           {B: 255, A: 128},
	} {
		if got, ok := Parse(in, cur); !ok || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
}
