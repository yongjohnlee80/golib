package gui

import (
	"math"
	"testing"
	"time"
)

// Dashes follow their pattern along a polyline, round its corners, and an odd pattern repeats.
func TestDash(t *testing.T) {
	runs := Dash([]Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, []float32{2, 3})
	if len(runs) != 2 || runs[0][0].X != 0 || runs[0][1].X != 2 || runs[1][0].X != 5 || runs[1][1].X != 7 {
		t.Errorf("2 on 3 off over 10: %v", runs)
	}
	runs = Dash([]Point{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 4}}, []float32{3, 1})
	if len(runs) == 0 || len(runs[0]) != 3 || runs[0][1] != (Point{X: 2, Y: 0}) || runs[0][2] != (Point{X: 2, Y: 1}) {
		t.Errorf("a dash round a corner: %v", runs)
	}
	if runs = Dash([]Point{{X: 0, Y: 0}, {X: 6, Y: 0}}, []float32{2}); len(runs) != 2 {
		t.Errorf("an odd pattern [2]: %v, want 2 on 2 off twice", runs)
	}
}

// A pattern too fine to see (a zero or negative one included), or one that would cut a line into too many dashes, is drawn solid,
// and the walk ends: a sub-pixel dash once stalled it (float32 stopped advancing) and a thousandth
// of a pixel made 300,000 dashes. A zero-length dash draws nothing.
func TestDashStaysBounded(t *testing.T) {
	line := []Point{{X: 0, Y: 0}, {X: 0, Y: 600}}
	for _, pat := range [][]float32{{1e-6, 1e-6}, {1e-3, 1e-3}, {0.4, 0.4}} {
		done := make(chan [][]Point, 1)
		go func() { done <- Dash(line, pat) }()
		select {
		case runs := <-done:
			if len(runs) != 1 || len(runs[0]) != 2 {
				t.Errorf("%v over 600 px: %d runs, want the line solid", pat, len(runs))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v over 600 px: still cutting after 5 s", pat)
		}
	}
	if runs := Dash(line, []float32{1e-6, 2}); len(runs) > 301 {
		t.Errorf("dashes of a millionth of a pixel, 2 px apart: %d runs, want at most one a period", len(runs))
	}
	if runs := Dash(line, []float32{0, 3}); len(runs) != 0 {
		t.Errorf("zero-length dashes: %d runs drawn, want none", len(runs))
	}
	if runs := Dash([]Point{{X: 0, Y: 0}, {X: 0, Y: 1e5}}, []float32{1, 1}); len(runs) != 1 {
		t.Errorf("a 2-pixel period over 100,000 px (50,000 dashes): %d runs, want solid", len(runs))
	}
	for _, pat := range [][]float32{{0}, {-3}, {}} {
		done := make(chan [][]Point, 1)
		go func() { done <- Dash(line, pat) }()
		select {
		case runs := <-done:
			if len(runs) != 1 || len(runs[0]) != 2 {
				t.Errorf("pattern %v: %d runs, want the line solid", pat, len(runs))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("pattern %v: still cutting after 5 s", pat)
		}
	}
}

// A pattern with a negative or non-finite length is drawn solid, even when its period looks
// positive: a negative length would walk the line backwards and never end.
func TestDashRefusesANegativeOrNonFiniteLength(t *testing.T) {
	line := []Point{{X: 0, Y: 0}, {X: 600, Y: 0}}
	inf := float32(math.Inf(1))
	for _, pat := range [][]float32{{-1e30, 1e30, 1, 1}, {inf, 1}, {float32(math.NaN()), 2}} {
		done := make(chan [][]Point, 1)
		go func() { done <- Dash(line, pat) }()
		select {
		case runs := <-done:
			if len(runs) != 1 || len(runs[0]) != 2 {
				t.Errorf("pattern %v: %d runs, want the line solid", pat, len(runs))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("pattern %v: still cutting after 5 s", pat)
		}
	}
}

// A dash that comes back to where it began (a closed outline, out and back) is kept: its geometry
// is not its endpoints.
func TestDashKeepsARunThatEndsWhereItBegan(t *testing.T) {
	runs := Dash([]Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 0, Y: 0}}, []float32{30, 10})
	if len(runs) != 1 || len(runs[0]) != 3 {
		t.Errorf("a 20-unit out-and-back line inside a 30-unit dash: %v, want one run of three points", runs)
	}
}

// Dash never returns more than MaxDashes runs, even when a pattern's short dashes and long gap
// make the estimate from whole periods fall short.
func TestDashNeverReturnsMoreThanMaxDashes(t *testing.T) {
	runs := Dash([]Point{{X: 0, Y: 0}, {X: 1365005, Y: 0}}, []float32{1, 1, 1, 1, 1, 995})
	if len(runs) > MaxDashes {
		t.Errorf("%d runs, more than MaxDashes (%d)", len(runs), MaxDashes)
	}
}

// The walk is bounded before it starts, not only its output: a line a million times longer than
// MaxDashes periods is drawn solid at once, rather than walked period by period to be refused at
// the end.
func TestDashBoundsItsWalkBeforeWalking(t *testing.T) {
	done := make(chan [][]Point, 1)
	go func() { done <- Dash([]Point{{X: 0, Y: 0}, {X: 1e10, Y: 0}}, []float32{2, 2}) }()
	select {
	case runs := <-done:
		if len(runs) != 1 || len(runs[0]) != 2 {
			t.Errorf("%d runs, want the line solid", len(runs))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a 1e10-pixel line with a 4-pixel period: still walking after 5 s")
	}
}
