package style

import "testing"

// TestXtermRGBIsXtermsPalette: the 256-colour indices resolve to xterm's default palette, the one
// source the downsampler, the contrast check and a renderer read: the sixteen ANSI colours, the
// 6×6×6 cube on levels 0, 95, 135, 175, 215, 255, and the 24-step grey ramp from 8.
func TestXtermRGBIsXtermsPalette(t *testing.T) {
	for i, want := range map[uint8][3]uint8{
		0: {0, 0, 0}, 1: {205, 0, 0}, 7: {229, 229, 229}, 8: {127, 127, 127}, 12: {92, 92, 255}, 15: {255, 255, 255},
		16: {0, 0, 0}, 17: {0, 0, 95}, 21: {0, 0, 255}, 46: {0, 255, 0}, 196: {255, 0, 0}, 208: {255, 135, 0}, 231: {255, 255, 255},
		232: {8, 8, 8}, 244: {128, 128, 128}, 255: {238, 238, 238},
	} {
		if got := XtermRGB(i); got != want {
			t.Errorf("XtermRGB(%d) = %v, want %v", i, got, want)
		}
	}
}
