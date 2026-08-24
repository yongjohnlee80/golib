package term

// Criterion 18, terminal producer (ADR-0010): the pointer contract driven from
// real SGR bytes through this package's decoder into a live App and real widgets.
//
// The rest of the branch injects events straight into a TestBackend. That is the
// right place to cover behaviour exhaustively — the event type is identical
// whatever produced it — but it deliberately BYPASSES this decoder, so it proves
// nothing about the terminal producer. This closes that gap the same way
// tui/web/input_app_test.go closes it for the browser.

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// runSGR decodes real SGR mouse bytes and delivers the events to a live App. It
// returns a sender so a test can feed further byte sequences to the SAME app
// through the backend, rather than mixing in App.Post.
func runSGR(t *testing.T, root tui.Component, w, h int, input string) (func(string), *tui.App) {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	send := func(in string) {
		t.Helper()
		evs := decodeEvents(t, in)
		if len(evs) == 0 {
			t.Fatalf("SGR input %q decoded to nothing", in)
		}
		for _, ev := range evs {
			if err := tb.Inject(ev); err != nil {
				t.Fatalf("inject: %v", err)
			}
		}
	}
	send(input)
	return send, app
}

func onAppLoop(t *testing.T, app *tui.App, fn func()) {
	t.Helper()
	done := make(chan struct{})
	app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("loop did not run Update within 3s")
	}
}

func waitLoop(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// An SGR press selects the clicked row: CSI < 0 ; col ; row M, 1-based.
func TestSGRPressSelectsListRow(t *testing.T) {
	list := widget.NewList(widget.WithItems([]string{"a", "b", "c", "d"},
		func(s string) string { return s }))
	// column 2, row 3 (1-based) -> local (1,2) -> list row 2.
	_, app := runSGR(t, list, 10, 4, "\x1b[<0;2;3M")

	waitLoop(t, "row 2 selected from SGR bytes", func() bool {
		var i int
		var ok bool
		onAppLoop(t, app, func() { i, ok = list.Selected() })
		return ok && i == 2
	})
}

// An SGR press also FOCUSES the clicked widget, through the terminal producer.
func TestSGRPressFocusesClickedWidget(t *testing.T) {
	top := widget.NewList(widget.WithItems([]string{"t0", "t1"}, func(s string) string { return s }))
	bottom := widget.NewList(widget.WithItems([]string{"b0", "b1"}, func(s string) string { return s }))
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(top, 1)
	root.AddWeighted(bottom, 1)

	// Press row 1 (top), then row 3 (bottom), both 1-based.
	send, app := runSGR(t, root, 10, 4, "\x1b[<0;2;1M")
	focused := func(l *widget.List[string]) bool {
		var f bool
		onAppLoop(t, app, func() {
			if ctx := l.Context(); ctx != nil {
				f = ctx.Focused()
			}
		})
		return f
	}
	waitLoop(t, "top focused from SGR bytes", func() bool { return focused(top) })

	send("\x1b[<0;2;3M")
	waitLoop(t, "focus moved to the clicked bottom list", func() bool { return focused(bottom) })
}

// An SGR wheel scrolls: CSI < 65 ; col ; row M is wheel-down.
func TestSGRWheelScrolls(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = "row"
	}
	list := widget.NewList(widget.WithItems(items, func(s string) string { return s }))
	send, app := runSGR(t, list, 10, 3, "\x1b[<65;2;2M\x1b[<65;2;2M")

	// Two wheel reports are two steps: a press on the top row now selects row 2.
	send("\x1b[<0;2;1M")
	waitLoop(t, "the top row is now item 2", func() bool {
		var i int
		var ok bool
		onAppLoop(t, app, func() { i, ok = list.Selected() })
		return ok && i == 2
	})
}
