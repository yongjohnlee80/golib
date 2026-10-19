package gui

import (
	"image/color"
	"testing"
)

// RoundRect is r with elliptical corners, its handles a quarter circle's (Kappa) back from each
// corner's end, and a plain rectangle when either radius is 0. FillRRect's path is it, clamped.
func TestPathRoundRectIsRWithEllipticalCorners(t *testing.T) {
	r := Rect{X: 10, Y: 20, W: 40, H: 10}
	p := new(Path).RoundRect(r, 8, 4)
	if p.Segments() != 10 || p.Bounds() != r { // a move, four edges, four corners, a close
		t.Fatalf("%d segments, bounds %v; want 10 and %v", p.Segments(), p.Bounds(), r)
	}
	// the top-right corner: from (x1-rx, y0) to (x1, y0+ry), its handles (1-Kappa) of each radius in
	// from the corner's ends
	c := p.cmds[2]
	rx, ry, x1, y0 := float32(8), float32(4), r.X+r.W, r.Y // as RoundRect computes them, in float32
	kx, ky := rx*(1-Kappa), ry*(1-Kappa)
	want := [3]Point{Pt(x1-kx, y0), Pt(x1, y0+ky), Pt(x1, y0+ry)}
	if c.kind != 'C' || c.pts != want {
		t.Errorf("the top-right corner: %c %v; want C %v", c.kind, c.pts, want)
	}
	if q := new(Path).RoundRect(r, 0, 4); q.Segments() != 5 || q.Bounds() != r {
		t.Errorf("a radius of 0: %d segments, bounds %v; want a plain rectangle", q.Segments(), q.Bounds())
	}
	// FillRRect's: one radius for both, clamped to half the shorter side, so its corners stay round
	if q := rrectPath(r, 99); q.cmds[2].pts[2] != Pt(50, 25) || q.cmds[1].pts[0] != Pt(45, 20) {
		t.Errorf("rrectPath's clamp: %v %v", q.cmds[1].pts[0], q.cmds[2].pts[2])
	}
}

// Mix blends every channel, alpha included, and rounds to the nearest: the blend gui, gui/widget and
// gui/mermaid share.
func TestMixBlendsEachChannelRounded(t *testing.T) {
	a, b := color.NRGBA{R: 0, G: 10, B: 200, A: 255}, color.NRGBA{R: 255, G: 11, B: 100, A: 0}
	if Mix(a, b, 0) != a || Mix(a, b, 1) != b {
		t.Fatalf("ends: %v %v", Mix(a, b, 0), Mix(a, b, 1))
	}
	if got, want := Mix(a, b, 0.5), (color.NRGBA{R: 128, G: 11, B: 150, A: 128}); got != want {
		t.Errorf("halfway: %v, want %v (127.5 and 10.5 round up)", got, want)
	}
}
