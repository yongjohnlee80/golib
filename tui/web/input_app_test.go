package web

// Criterion 18 (ADR-0010): the pointer contract must be proven against BOTH
// producers, not one. Every other test in this branch injects events straight
// into a TestBackend; these drive the real web path — a client MouseReport
// through this package's decoder — and then assert the App and a real widget
// behave, rather than asserting the decoded event's shape.
//
// The existing input_test.go cases pin decode SHAPE. What was missing, and what
// lector's r2 review required before merge, is a behavioural path.

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// runDecoded decodes a client report the way a live session does and delivers the
// resulting event to a real App over a TestBackend.
// onLoop runs fn on the App's loop goroutine and waits. Widget state MUST be read
// this way: the loop mutates it concurrently, and reading from the test goroutine
// is a data race (-race caught exactly that in the first version of this file).
func onLoop(t *testing.T, app *tui.App, fn func()) {
	t.Helper()
	done := make(chan struct{})
	app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("loop did not run Update within 3s")
	}
}

func runDecoded(t *testing.T, root tui.Component, w, h int, reports ...MouseReport) (func(...MouseReport), *tui.App) {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	send := func(rs ...MouseReport) {
		t.Helper()
		for _, r := range rs {
			ev, ok := decodeMouse(r)
			if !ok {
				t.Fatalf("decodeMouse(%+v) emitted nothing", r)
			}
			if err := tb.Inject(ev); err != nil {
				t.Fatalf("inject: %v", err)
			}
		}
	}
	send(reports...)
	return send, app
}

func waitUntil(t *testing.T, what string, cond func() bool) {
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

// A browser press, decoded here, must select the clicked list row — the same
// behaviour a terminal press produces.
func TestWebPressSelectsListRow(t *testing.T) {
	list := widget.NewList(widget.WithItems([]string{"a", "b", "c", "d"},
		func(s string) string { return s }))
	_, app := runDecoded(t, list, 10, 4, MouseReport{Kind: "down", Button: 0, X: 1, Y: 2})

	waitUntil(t, "row 2 selected via the web decoder", func() bool {
		var i int
		var ok bool
		onLoop(t, app, func() { i, ok = list.Selected() })
		return ok && i == 2
	})
}

// A browser press must also FOCUS the clicked widget (§2.1) when it arrives
// through the web decoder, not only through TestBackend injection.
//
// The first version of this test was VACUOUS and lector caught it: both Lists
// start at cursor 0, so asserting `bottom.Selected() == 0` was already true
// before the report was decoded or delivered. It could not fail. This version
// asserts FOCUS via Context().Focused(), and starts by focusing the TOP list so
// there is a real transition to observe rather than an initial state to restate.
func TestWebPressFocusesClickedWidget(t *testing.T) {
	top := widget.NewList(widget.WithItems([]string{"t0", "t1"}, func(s string) string { return s }))
	bottom := widget.NewList(widget.WithItems([]string{"b0", "b1"}, func(s string) string { return s }))
	root := tui.NewFlex(tui.Vertical)
	// Weighted, so each list really occupies half the viewport. With plain Add a
	// List has no intrinsic height and the first can take everything, leaving the
	// second unplaced and therefore not hit-testable at all.
	root.AddWeighted(top, 1)
	root.AddWeighted(bottom, 1)

	// Press the TOP list first, so focus starts somewhere it must move FROM.
	send, app := runDecoded(t, root, 10, 4, MouseReport{Kind: "down", Button: 0, X: 1, Y: 0})

	focused := func(l *widget.List[string]) bool {
		var f bool
		onLoop(t, app, func() {
			if ctx := l.Context(); ctx != nil {
				f = ctx.Focused()
			}
		})
		return f
	}

	waitUntil(t, "the top list is focused first", func() bool { return focused(top) })

	// Now click the BOTTOM list: focus must move to it.
	send(MouseReport{Kind: "down", Button: 0, X: 1, Y: 2})
	waitUntil(t, "focus moved to the clicked (bottom) list", func() bool { return focused(bottom) })
	if focused(top) {
		t.Error("the top list still holds focus after the bottom list was clicked")
	}
}

// A browser wheel scrolls, and N reports are N steps — the producer rule stated
// in §2.3, proven on the producer that actually sends them.
func TestWebWheelIsOneStepPerReport(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = "row"
	}
	list := widget.NewList(widget.WithItems(items, func(s string) string { return s }))
	send, app := runDecoded(t, list, 10, 3,
		MouseReport{Kind: "wheel", Dir: "down", X: 1, Y: 1},
		MouseReport{Kind: "wheel", Dir: "down", X: 1, Y: 1},
	)

	// Two wheel reports are two steps, observed behaviourally: a press on the TOP
	// visible row now selects row 2 rather than row 0. One report multiplied into
	// several steps would land further down.
	send(MouseReport{Kind: "down", Button: 0, X: 1, Y: 0})
	waitUntil(t, "the top row is now item 2", func() bool {
		var i int
		var ok bool
		onLoop(t, app, func() { i, ok = list.Selected() })
		return ok && i == 2
	})
}
