package tui_test

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

func TestAppDoneClosesAfterRun(t *testing.T) {
	tb := tui.NewTestBackend(10, 2)
	app := tui.NewApp(&staticText{}, tui.WithBackend(tb))
	select {
	case <-app.Done():
		t.Fatal("Done closed before Run")
	default:
	}
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- app.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-app.Done():
		t.Fatal("Done closed while Run runs")
	default:
	}
	cancel()
	<-res
	select {
	case <-app.Done():
	case <-time.After(time.Second):
		t.Fatal("Done still open after Run returned")
	}
}

// staticText is a component that renders nothing.
type staticText struct{}

func (*staticText) Init(*tui.Context)                 {}
func (*staticText) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{W: 1, H: 1}) }
func (*staticText) Render(tui.Surface)                {}
func (*staticText) HandleEvent(tui.Event) bool        { return false }
