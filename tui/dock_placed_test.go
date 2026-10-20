package tui

import (
	"sync"
	"testing"
	"time"
)

// OnPlaced is told where each child is placed, Dock-relative, in the pass that places it: the
// pinned ones as they take their edges, then the center one in what they leave.
func TestDockOnPlacedTellsWhereEachChildIs(t *testing.T) {
	t.Parallel()
	top := &probe{name: "top", pref: Size{W: 99, H: 1}, fill: "t"}
	left := &probe{name: "left", pref: Size{W: 3, H: 99}, fill: "l"}
	center := &probe{name: "center", fill: "c"}
	var mu sync.Mutex
	got := map[string]Rect{}
	dock := NewDock()
	dock.Pin(DockTop, top)
	dock.Pin(DockLeft, left)
	dock.Add(center)
	dock.OnPlaced(func(c Component, r Rect) {
		mu.Lock()
		defer mu.Unlock()
		got[c.(*probe).name] = r
	})
	startApp(t, dock, 10, 4)
	mu.Lock()
	defer mu.Unlock()
	want := map[string]Rect{"top": {W: 10, H: 1}, "left": {Y: 1, W: 3, H: 3}, "center": {X: 3, Y: 1, W: 7, H: 3}}
	for name, r := range want {
		if got[name] != r {
			t.Errorf("%s placed at %+v; want %+v", name, got[name], r)
		}
	}
}

// SetEdge moves a pinned child to another edge from the next layout, keeping its order.
func TestDockSetEdgeMovesAPinnedChild(t *testing.T) {
	t.Parallel()
	left := &probe{name: "left", pref: Size{W: 3, H: 99}, fill: "l"}
	center := &probe{name: "center", fill: "c"}
	dock := NewDock()
	dock.Pin(DockLeft, left)
	dock.Add(center)
	h := startApp(t, dock, 10, 2)
	if want := "lllccccccc\nlllccccccc"; h.tb.String() != want {
		t.Fatalf("frame:\n%q\nwant\n%q", h.tb.String(), want)
	}
	h.onLoop(func() { dock.SetEdge(left, DockRight) })
	want := "ccccccclll\nccccccclll"
	for deadline := time.Now().Add(3 * time.Second); h.tb.String() != want; {
		if time.Now().After(deadline) {
			t.Fatalf("after SetEdge(right):\n%q\nwant\n%q", h.tb.String(), want)
		}
		h.sync()
	}
}
