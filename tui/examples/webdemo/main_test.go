package main

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/examples/demoapp"
	"github.com/yongjohnlee80/golib/tui/web"
)

// TestCriterion1_SameTreeOnTheWebBackend asserts the SAME-TREE guarantee
// mechanically rather than by inspection.
//
// It runs the demo's component tree — the same package the terminal demo runs,
// whose own interaction script passes against tui.TestBackend — through
// tui/web's Backend, and requires that it renders, accepts input and repaints.
// The only difference from the terminal path is the backend handed to
// tui.NewApp.
func TestCriterion1_SameTreeOnTheWebBackend(t *testing.T) {
	backend := web.New()
	quit := func() {}

	// Identical to webdemo's factory, and identical to the terminal demo's
	// construction apart from the backend.
	app := tui.NewApp(demoapp.New(quit, true), tui.WithBackend(backend))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()

	// A client attaches with measured metrics, which is what releases Start.
	if err := backend.Attach(web.Hello{
		Cols: 80, Rows: 24, Metrics: web.Metrics{CellW: 8, CellH: 16},
	}); err != nil {
		t.Fatal(err)
	}

	// It renders: a frame appears with cells in it.
	frame, ok := waitFrame(t, backend, 5*time.Second)
	if !ok {
		t.Fatal("the demo produced no frame on the web backend")
	}
	if !frame.Full {
		t.Error("the first frame to a fresh client must be a full snapshot")
	}
	if len(frame.Updates) == 0 {
		t.Fatal("the first frame carried no cells — nothing was rendered")
	}
	if frame.W != 80 || frame.H != 24 {
		t.Errorf("frame is %dx%d, want the client's measured 80x24", frame.W, frame.H)
	}
	painted := 0
	for _, u := range frame.Updates {
		if c := u.Cell.Content; c != "" && c != " " {
			painted++
		}
	}
	if painted == 0 {
		t.Error("the frame is entirely blank — the component tree drew nothing")
	}
	backend.AckFrame(frame.Rev)

	// It accepts input and repaints. Tab cycles the panes, which the demo's own
	// interaction script exercises against TestBackend.
	if err := backend.Submit(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}); err != nil {
		t.Fatal(err)
	}
	repaint, ok := waitFrame(t, backend, 5*time.Second)
	if !ok {
		t.Fatal("a keystroke produced no repaint")
	}
	// Acknowledged, because only one frame is in flight at a time — leaving this
	// unacked blocks every later frame, which is the design working rather than
	// a fault.
	backend.AckFrame(repaint.Rev)

	// It handles a resize, and the next frame matches the new size.
	if err := backend.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		fr, ok := waitFrame(t, backend, time.Until(deadline))
		if !ok {
			t.Fatal("no frame after the resize")
		}
		backend.AckFrame(fr.Rev)
		if fr.W == 100 && fr.H == 30 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("frames never reached 100x30 (last %dx%d)", fr.W, fr.H)
		}
	}

	// And it exits cleanly.
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("app.Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app did not exit")
	}
	if err := backend.Stop(); err != nil {
		t.Errorf("Stop = %v", err)
	}
}

// waitFrame polls for the next frame the backend has ready.
func waitFrame(t *testing.T, b *web.Backend, within time.Duration) (web.Frame, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if fr, ok := b.NextFrame(); ok {
			return fr, true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return web.Frame{}, false
}
