package gui

import "math"

// MaxDashes bounds the dashes Dash cuts a polyline into: a pattern that would cut it finer is drawn
// solid.
const MaxDashes = 4096

// Dash cuts the polyline through pts into its dashes: runs of pattern's on lengths, skipping its
// off lengths, in the polyline's units. An odd pattern repeats, as SVG's does. A pattern whose
// period is under a pixel (a zero or negative one included), or that would cut the polyline into
// more than MaxDashes, comes back as the whole polyline, solid: at that density a dash is not
// seen, and the walk stays bounded. A zero-length dash draws nothing, as SVG's does. The walk is
// in float64, so a short dash still advances along a long line. A caller strokes each run.
func Dash(pts []Point, pattern []float32) [][]Point {
	if len(pts) < 2 || len(pattern) == 0 {
		return [][]Point{pts}
	}
	pat := make([]float64, 0, 2*len(pattern))
	period := 0.0
	for _, d := range pattern {
		pat = append(pat, float64(d))
		period += float64(d)
	}
	if len(pat)%2 == 1 {
		pat = append(pat, pat...)
		period *= 2
	}
	length := 0.0
	for k := 1; k < len(pts); k++ {
		length += math.Hypot(float64(pts[k].X-pts[k-1].X), float64(pts[k].Y-pts[k-1].Y))
	}
	if period < 1 || length/period*float64(len(pat)/2) > MaxDashes {
		return [][]Point{pts}
	}
	var out [][]Point
	keep := func(run []Point) {
		if len(run) > 1 && run[0] != run[len(run)-1] {
			out = append(out, run)
		}
	}
	i, left, on := 0, pat[0], true
	run := []Point{pts[0]}
	for k := 1; k < len(pts); k++ {
		a, z := pts[k-1], pts[k]
		seg := math.Hypot(float64(z.X-a.X), float64(z.Y-a.Y))
		for at := 0.0; at < seg; {
			step := min(left, seg-at)
			at += step
			left -= step
			t := float32(at / seg)
			p := Pt(a.X+(z.X-a.X)*t, a.Y+(z.Y-a.Y)*t)
			if on {
				run = append(run, p)
			}
			if left <= 0 {
				if on {
					keep(run)
				}
				run = []Point{p}
				on, i = !on, (i+1)%len(pat)
				left = pat[i]
			}
		}
	}
	if on {
		keep(run)
	}
	return out
}
