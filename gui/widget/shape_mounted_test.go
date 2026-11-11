package widget

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// A mounted shape's SetAcceptsPointer changes what a press reaches from then on: on the loop, after
// the first frame, accepting makes the shape take the next press, and not accepting again lets the
// one after reach the widget beneath.
func TestAMountedShapesAcceptsPointerChangesThePressesTarget(t *testing.T) {
	t.Parallel()
	probe := &pointerProbe{}
	s := NewShape(ShapeRect, WithShapeSize(4, 2))
	root := tui.NewStack()
	root.AddFilled(probe)
	root.AddAt(s, 2, 1)
	tb := tui.NewTestBackend(10, 4)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)
	for deadline := time.Now().Add(5 * time.Second); tb.Flushes() == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the app never drew")
		}
	}
	press := func() {
		t.Helper()
		done, err := tb.InjectFenced(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 3, Y: 2},
			tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 3, Y: 2})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the press was never dispatched")
		}
	}
	press()
	if got := probe.got(); len(got) != 1 {
		t.Fatalf("before any change the widget beneath got %v, want the press", got)
	}
	onLoop(t, app, func() { s.SetAcceptsPointer(true) })
	press()
	if got := probe.got(); len(got) != 1 {
		t.Fatalf("after SetAcceptsPointer(true) on the mounted shape the widget beneath got %v, want no new press", got)
	}
	onLoop(t, app, func() { s.SetAcceptsPointer(false) })
	press()
	if got := probe.got(); len(got) != 2 {
		t.Fatalf("after SetAcceptsPointer(false) the widget beneath got %v, want the press again", got)
	}
}
