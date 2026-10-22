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

// Polyline adds a subpath through its points, closed when asked; no points add nothing.
func TestPathPolylineAddsASubpath(t *testing.T) {
	pts := []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 5}}
	if p := new(Path).Polyline(pts, false); p.Segments() != 3 || p.Bounds() != (Rect{W: 10, H: 5}) {
		t.Fatalf("open: %d segments, bounds %v; want 3 and 10x5", p.Segments(), p.Bounds())
	}
	p := new(Path).Polyline(pts, true)
	if p.Segments() != 4 {
		t.Fatalf("closed: %d segments, want 4 (a move, two lines, a close)", p.Segments())
	}
	if p.Polyline(nil, true) != p || p.Segments() != 4 {
		t.Errorf("no points added %d segments", p.Segments()-4)
	}
}
