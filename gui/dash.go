package gui

import cv "github.com/yongjohnlee80/golib/chart/vegalite"

// MaxDashes bounds the dashes Dash cuts a polyline into: a pattern that would cut it finer is drawn
// solid.
const MaxDashes = cv.MaxDashes

// Dash cuts the polyline through pts into its dashes: runs of pattern's on lengths, skipping its
// off lengths, in the polyline's units. An odd pattern repeats, as SVG's does. It is
// chart/vegalite's Dash (a chart is laid out there, without gio), over gui's points; that one
// says when the polyline comes back solid. A caller strokes each run.
func Dash(pts []Point, pattern []float32) [][]Point {
	in := make([]cv.Point, len(pts))
	for i, p := range pts {
		in[i] = cv.Point(p)
	}
	runs := cv.Dash(in, pattern)
	out := make([][]Point, len(runs))
	for i, run := range runs {
		if run == nil {
			continue
		}
		out[i] = make([]Point, len(run))
		for j, p := range run {
			out[i][j] = Point(p)
		}
	}
	return out
}
