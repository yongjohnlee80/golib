package gui

import (
	"image"
	"testing"
)

// pixelTolerance is how far, per byte, a fast path's pixels may be from the path they replace:
// rasterizers round an edge's coverage, so a pixel clip and a path on the same whole-pixel edges
// can differ by a level or two. Measured on VM43's Mesa llvmpipe (2026-10-30): at most 2.
const pixelTolerance = 2

// inkLevel is how far from the white ground a byte must be for its pixel to count as inked: past
// any rounding, so the inked set is the shape, not its antialiasing.
const inkLevel = 16

// compareShots compares got with want, both drawn over white: no byte more than pixelTolerance
// apart, and the same set of inked pixels, so a shape moved, grown or shrunk by even one pixel
// fails however slightly its bytes differ. ctrl is want drawn again: it must equal want exactly, or
// the drawing isn't deterministic and no tolerance means anything.
func compareShots(t *testing.T, name string, got, want, ctrl *image.RGBA) {
	t.Helper()
	differ, worst, inked, shape := 0, 0, 0, 0
	for i := range want.Pix {
		if ctrl.Pix[i] != want.Pix[i] {
			t.Fatalf("%s: the reference drawn twice differs at byte %d: the drawing isn't deterministic", name, i)
		}
		d := int(got.Pix[i]) - int(want.Pix[i])
		if d < 0 {
			d = -d
		}
		if d != 0 {
			differ++
			worst = max(worst, d)
		}
	}
	for p := 0; p+3 < len(want.Pix); p += 4 {
		in := func(px []uint8) bool {
			return 0xff-int(px[0]) > inkLevel || 0xff-int(px[1]) > inkLevel || 0xff-int(px[2]) > inkLevel
		}
		w, g := in(want.Pix[p:p+4]), in(got.Pix[p:p+4])
		if w {
			inked++
		}
		if w != g {
			shape++
		}
	}
	t.Logf("%s: %d of %d bytes differ, the worst by %d; %d pixels inked, %d inked in only one", name, differ, len(want.Pix), worst, inked, shape)
	if inked == 0 {
		t.Fatalf("%s: the reference drew nothing: the comparison proves nothing", name)
	}
	if worst > pixelTolerance || shape != 0 {
		t.Errorf("%s: worst byte %d apart (tolerance %d), %d pixels inked in only one", name, worst, pixelTolerance, shape)
	}
}
