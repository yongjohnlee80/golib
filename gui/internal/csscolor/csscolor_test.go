package csscolor

import (
	"image/color"
	"testing"
)

// Parse is parse/csscolor in image/color: a colour read through it is the same colour, and
// currentColor is the one given.
func TestParseReadsCSSColoursAsNRGBA(t *testing.T) {
	cur := color.NRGBA{R: 1, G: 2, B: 3, A: 4}
	for _, c := range []struct {
		in   string
		want color.NRGBA
		ok   bool
	}{
		{"#ff8000", color.NRGBA{R: 0xff, G: 0x80, A: 0xff}, true},
		{"#0f08", color.NRGBA{G: 0xff, A: 0x88}, true},
		{"rgba(10, 20, 30, 0.5)", color.NRGBA{R: 10, G: 20, B: 30, A: 0x80}, true},
		{"red", color.NRGBA{R: 0xff, A: 0xff}, true},
		{"currentColor", cur, true},
		{"not a colour", color.NRGBA{}, false},
	} {
		got, ok := Parse(c.in, cur)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Parse(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
