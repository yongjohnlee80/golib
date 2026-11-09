package widget

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// A tile's arrows read the manager's shared borders once a frame: FrameShapes on a tile costs
// fewer allocations than asking its target about each of its five frame cells one at a time
// (frameSide, each a HandleAt that rebuilds the placed rects and the shared borders). Measured in
// one build, against each other, so -race's own allocations cancel out.
func TestATilesArrowsReadItsBordersOncePerFrame(t *testing.T) {
	tiles := NewTiles()
	left := tiles.Add("left", NewText("l"), TilePlace{Edge: tui.DockLeft, Size: 30})
	tiles.Add("page", NewText("p"), TilePlace{Edge: tui.DockCenter})
	tiles.Add("right", NewText("r"), TilePlace{Edge: tui.DockRight, Size: 30})
	tb := tui.NewTestBackend(80, 24, tui.WithTestCapabilities(tui.Capabilities{FrameDrag: true}))
	app := tui.NewApp(tiles, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(5 * time.Second); tb.Flushes() == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no frame")
		}
	}
	var batched, single float64
	var shapes []tui.PointerRegion
	ran := make(chan struct{})
	app.Update(func() {
		defer close(ran)
		core := left.core()
		r := core.Bounds()
		size := tui.Size{W: r.W, H: r.H}
		points := []tui.Point{{X: 0, Y: 1}, {X: size.W - 1, Y: 1}, {X: 1, Y: size.H - 1}, {X: 0, Y: size.H - 1}, {X: size.W - 1, Y: size.H - 1}}
		sides := []Handle{HandleLeft, HandleRight, HandleBottom, HandleBottomLeft, HandleBottomRight}
		buf := make([]tui.PointerRegion, 0, 8)
		shapes = core.FrameShapes(buf, size)
		batched = testing.AllocsPerRun(200, func() { buf = core.FrameShapes(buf[:0], size) })
		single = testing.AllocsPerRun(200, func() {
			for i, p := range points {
				core.frameSide(sides[i], p)
			}
		})
	})
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not run the measurement")
	}
	if len(shapes) == 0 {
		t.Fatal("the left tile shows no arrows: the measurement measured nothing")
	}
	if batched >= single {
		t.Fatalf("FrameShapes allocates %.0f a call, asking each of its five cells %.0f: it doesn't read the borders once", batched, single)
	}
}
