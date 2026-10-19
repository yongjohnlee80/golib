package gui

import "image/color"

// Mix is a blended toward b by t: a at 0, b at 1, each channel (alpha too) rounded to the nearest.
// The one colour blend gui and its packages share (painters, gui/widget, gui/mermaid).
func Mix(a, b color.NRGBA, t float32) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: m(a.A, b.A)}
}
