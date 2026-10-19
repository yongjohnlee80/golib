package gui

import "testing"

// Ellipse adds a closed subpath to the path it is called on, so many circles are one path; a
// path made by Ellipse alone is the ellipse FillEllipse fills.
func TestPathEllipseAddsASubpath(t *testing.T) {
	a, b := Rect{X: 0, Y: 0, W: 10, H: 10}, Rect{X: 20, Y: 30, W: 4, H: 6}
	p := new(Path).Ellipse(a)
	if p.Segments() != 6 || p.Bounds() != a { // a move, four arcs, a close
		t.Fatalf("one ellipse: %d segments, bounds %v; want 6 and %v", p.Segments(), p.Bounds(), a)
	}
	if p.Ellipse(b) != p || p.Segments() != 12 || p.Bounds() != (Rect{X: 0, Y: 0, W: 24, H: 36}) {
		t.Errorf("two ellipses: %d segments, bounds %v", p.Segments(), p.Bounds())
	}
	if e := ellipsePath(b); e.Segments() != 6 || e.Bounds() != b {
		t.Errorf("FillEllipse's path: %d segments, bounds %v", e.Segments(), e.Bounds())
	}
}
