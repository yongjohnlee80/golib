package gui

import (
	"bytes"
	"image"
	"image/color"
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
// the drawing isn't deterministic and no tolerance means anything. The comparison is checked to
// tell want from want moved by one pixel, across or down, or it proves nothing about a shape.
func compareShots(t *testing.T, name string, got, want, ctrl *image.RGBA) {
	t.Helper()
	if !bytes.Equal(ctrl.Pix, want.Pix) {
		t.Fatalf("%s: the reference drawn twice differs: the drawing isn't deterministic", name)
	}
	d := shotsDiffer(got, want)
	t.Logf("%s: %d of %d bytes differ, the worst by %d; %d pixels inked, %d inked in only one, %d more at the ink's edge",
		name, d.differ, len(want.Pix), d.worst, d.inked, d.shape, d.edge)
	if d.inked == 0 {
		t.Fatalf("%s: the reference drew nothing: the comparison proves nothing", name)
	}
	if d.alike() {
		moved := false
		for _, by := range []image.Point{{X: 1}, {Y: 1}} {
			moved = moved || !shotsDiffer(shifted(want, by), want).alike()
		}
		if !moved {
			t.Fatalf("%s: the reference moved by one pixel compares alike: the comparison can't tell a shape moved", name)
		}
	}
	if d.worst > pixelTolerance || d.shape != 0 {
		t.Errorf("%s: worst byte %d apart (tolerance %d), %d pixels inked in only one", name, d.worst, pixelTolerance, d.shape)
	}
}

// shotDiff is how one drawing differs from another (shotsDiffer).
type shotDiff struct {
	differ, worst int // the bytes that differ, and the most any differs by
	inked         int // the pixels the reference inks
	// shape counts the pixels inked in one drawing and clearly not in the other: no byte of it
	// within pixelTolerance of inkLevel. edge counts the others inked in only one: a byte a level
	// or two either side of the cut-off, as rasterizers on two CPUs round a shape's antialiasing
	// (a GitHub runner's llvmpipe and VM43's differ by one level on path-stroke@2x). Those are
	// bounded by the byte tolerance, not by the shape.
	shape, edge int
}

func (d shotDiff) alike() bool { return d.worst <= pixelTolerance && d.shape == 0 }

// shotsDiffer is how got differs from want.
func shotsDiffer(got, want *image.RGBA) shotDiff {
	var d shotDiff
	for i := range want.Pix {
		v := int(got.Pix[i]) - int(want.Pix[i])
		if v < 0 {
			v = -v
		}
		if v != 0 {
			d.differ++
			d.worst = max(d.worst, v)
		}
	}
	// ink is how far the pixel's farthest colour byte is from the white ground
	ink := func(px []uint8) int { return 0xff - int(min(px[0], px[1], px[2])) }
	for p := 0; p+3 < len(want.Pix); p += 4 {
		w, g := ink(want.Pix[p:p+4]), ink(got.Pix[p:p+4])
		if w > inkLevel {
			d.inked++
		}
		if (w > inkLevel) == (g > inkLevel) {
			continue
		}
		if min(w, g) > inkLevel-pixelTolerance {
			d.edge++ // the one not inked is within the tolerance of the cut-off
			continue
		}
		d.shape++
	}
	return d
}

// shifted is img moved by (by.X, by.Y) pixels, the ground white where it came from.
func shifted(img *image.RGBA, by image.Point) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	for i := range out.Pix {
		out.Pix[i] = 0xff
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if p := (image.Point{X: x, Y: y}).Add(by); p.In(b) {
				out.SetRGBA(p.X, p.Y, img.RGBAAt(x, y))
			}
		}
	}
	return out
}

// TestShotsDifferAllowsTheInksEdgeAndNoMore: a pixel a level either side of the ink's cut-off in
// two drawings is at the edge, and they compare alike; a pixel inked in one and clear in the other
// is a shape apart; a drawing moved by one pixel, across or down, is never alike.
func TestShotsDifferAllowsTheInksEdgeAndNoMore(t *testing.T) {
	white := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for i := range img.Pix {
			img.Pix[i] = 0xff
		}
		return img
	}
	grey := func(img *image.RGBA, x, y, ink int) {
		v := uint8(0xff - ink)
		img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 0xff})
	}
	want, got := white(), white()
	grey(want, 2, 2, inkLevel+1) // inked, by one level
	grey(got, 2, 2, inkLevel)    // not, by one level: a rasterizer's rounding apart
	if d := shotsDiffer(got, want); !d.alike() || d.edge != 1 || d.shape != 0 || d.worst != 1 {
		t.Errorf("a pixel either side of the cut-off by a level: %+v; want alike, one at the edge", d)
	}
	got = white()
	grey(got, 2, 2, inkLevel-pixelTolerance) // clear of the cut-off by the tolerance
	if d := shotsDiffer(got, want); d.alike() || d.shape != 1 || d.edge != 0 {
		t.Errorf("a pixel inked in one and clear in the other: %+v; want a shape apart", d)
	}
	box := white()
	for y := 2; y < 6; y++ {
		for x := 2; x < 6; x++ {
			grey(box, x, y, 0xc0)
		}
	}
	for _, by := range []image.Point{{X: 1}, {Y: 1}} {
		if d := shotsDiffer(shifted(box, by), box); d.alike() {
			t.Errorf("a box moved by %v: %+v; want it told apart", by, d)
		}
	}
}
