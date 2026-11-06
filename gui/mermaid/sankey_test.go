package mermaid

import (
	"cmp"
	"math"
	"slices"
	"testing"
)

// A sankey lays out as its nodes in columns by depth and its flows as bands: a band a flow, each as
// thick as its amount on one scale, starting on its source's right edge and ending on its target's
// left edge, within each node's height; a node as tall as what passes through it.
func TestASankeyIsLaidOut(t *testing.T) {
	l, err := lay(t, "sankey-beta\na,b,10\na,c,5\nb,d,6\nc,d,5\nb,e,4\n", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.bands) != 5 {
		t.Fatalf("%d bands; want one a flow (5)", len(l.bands))
	}
	box := map[string]laidNode{}
	for _, n := range l.nodes {
		box[n.id] = n
	}
	// columns: a, then b and c, then d and e
	if !(box["a"].box.X < box["b"].box.X && box["b"].box.X == box["c"].box.X && box["c"].box.X < box["d"].box.X && box["d"].box.X == box["e"].box.X) {
		t.Errorf("columns: a %v, b %v, c %v, d %v, e %v", box["a"].box.X, box["b"].box.X, box["c"].box.X, box["d"].box.X, box["e"].box.X)
	}
	values := []float64{10, 5, 6, 5, 4}
	scale := (l.bands[0].y0b - l.bands[0].y0a) / 10
	for i, b := range l.bands {
		if got := b.y0b - b.y0a; math.Abs(float64(got-float32(values[i])*scale)) > 0.01 || math.Abs(float64((b.y1b-b.y1a)-got)) > 0.01 {
			t.Errorf("band %d is %v thick at its source, %v at its target; want %v both", i, got, b.y1b-b.y1a, float32(values[i])*scale)
		}
	}
	// what passes through: a 15, b 10, d 11; each band within its nodes' heights
	for name, through := range map[string]float32{"a": 15, "b": 10, "d": 11} {
		if h := box[name].box.H; math.Abs(float64(h-through*scale)) > 0.01 {
			t.Errorf("%s is %v tall, want %v (%v through it)", name, h, through*scale, through)
		}
	}
	// the bands into a node are stacked down its left edge, edge to edge from its top, and the bands
	// out of it down its right edge: never two at one place
	stacked := func(what string, spans [][2]float32, top float32) {
		t.Helper()
		slices.SortFunc(spans, func(a, b [2]float32) int { return cmp.Compare(a[0], b[0]) })
		at := top
		for _, s := range spans {
			if math.Abs(float64(s[0]-at)) > 0.01 {
				t.Errorf("%s: a band starts at %v, want %v (the one before it ends there)", what, s[0], at)
			}
			at = s[1]
		}
	}
	links := [][2]string{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}, {"b", "e"}}
	for _, n := range []string{"a", "b", "d"} {
		var in, out [][2]float32
		for i, b := range l.bands {
			if links[i][1] == n {
				in = append(in, [2]float32{b.y1a, b.y1b})
			}
			if links[i][0] == n {
				out = append(out, [2]float32{b.y0a, b.y0b})
			}
		}
		if len(in) > 0 {
			stacked(n+"'s inflows", in, box[n].box.Y)
		}
		if len(out) > 0 {
			stacked(n+"'s outflows", out, box[n].box.Y)
		}
	}
	for i, b := range l.bands {
		s, d := box[links[i][0]].box, box[links[i][1]].box
		if b.x0 != s.X+s.W || b.x1 != d.X || b.y0a < s.Y-0.01 || b.y0b > s.Y+s.H+0.01 || b.y1a < d.Y-0.01 || b.y1b > d.Y+d.H+0.01 {
			t.Errorf("band %d (%s to %s) %+v isn't from %s's right edge %+v to %s's left edge %+v", i, links[i][0], links[i][1], b, links[i][0], s, links[i][1], d)
		}
	}
}
