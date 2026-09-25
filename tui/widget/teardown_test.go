package widget_test

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// teardown_test.go: an App's teardown unmounts the whole tree while a Float or
// a Modal is still OPEN. What owns them may release them only afterwards — a
// declarative tree destroyed after the App stops — and hiding, detaching or
// dismissing then must be a no-op, not a panic over a layer already gone.
func TestAnOpenFloatOrModalCanBeReleasedAfterTheAppStops(t *testing.T) {
	host := widget.NewOverlayHost(widget.NewText("base"))
	float := widget.NewFloat(widget.NewText("floating"), widget.WithModal(true))
	host.Attach(float)
	modal := widget.NewModal(widget.NewText("asking"))
	tb := tui.NewTestBackend(30, 6)
	app := tui.NewApp(host, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	opened := make(chan error, 1)
	app.Update(func() {
		float.Show()
		opened <- modal.Open(host)
	})
	if err := <-opened; err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the app did not stop")
	}
	// The tree is gone; its owners release what they held.
	float.Hide()
	host.Detach(float)
	modal.Dismiss(widget.DismissProgrammatic)
	if float.Shown() || modal.IsOpen() {
		t.Errorf("after release: float shown %v, modal open %v", float.Shown(), modal.IsOpen())
	}
}
