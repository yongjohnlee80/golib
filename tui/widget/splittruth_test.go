package widget_test

// GEOMETRY TRUTH for Split, and the divider's capture.
//
// Every failure in this file is a LIE rather than a crash: a ratio reported
// that the screen does not have, a clamped value persisted as the user's
// choice, an event for a drag that moved nothing, a drag silently reverted
// because the runtime took the pointer away. A lie in geometry is exactly the
// kind of defect that survives a screenshot, which is why these are asserted
// against the committed cells rather than against the rendered divider column.
//
// It pairs with split_test.go, which covers the pre-migration surface (layout,
// zoom, focus transfer). Those tests all passed unchanged across the migration
// onto commit + capture + actions — which is precisely why this file exists:
// a suite that cannot tell the two designs apart is not observing either.

import (
	"math"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// --- loop-goroutine readers -------------------------------------------------

func ratioOn(t *testing.T, h *harness, s *widget.Split) float64 {
	t.Helper()
	var r float64
	h.onLoop(func() { r = s.Ratio() })
	return r
}

func requestedRatioOn(t *testing.T, h *harness, s *widget.Split) float64 {
	t.Helper()
	var r float64
	h.onLoop(func() { r = s.RequestedRatio() })
	return r
}

func cellsOn(t *testing.T, h *harness, s *widget.Split) (a, b int, ok bool) {
	t.Helper()
	h.onLoop(func() { a, b, ok = s.Cells() })
	return a, b, ok
}

func aCellsOn(t *testing.T, h *harness, s *widget.Split) int {
	t.Helper()
	a, _, _ := cellsOn(t, h, s)
	return a
}

// splitFixture mounts a horizontal Split of two focusable panes and leaves the
// first one focused, so the divider's keyboard vocabulary has a route to it.
func splitFixture(t *testing.T, w, hgt int, opts ...widget.SplitOption) (*harness, *widget.Split, *shell) {
	t.Helper()
	s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"}, opts...)
	sh := newShell(s)
	h := startApp(t, sh, w, hgt)
	h.inject(tab())
	h.barrier(sh)
	return h, s, sh
}

// TestTheEffectiveRatioIsWhatIsOnScreenAndTheRequestSurvives.
//
// Persistence stores the REQUEST. A split that persisted the value a min size
// clamped it to would walk its divider a little further on every restore at a
// narrow width — a drift nobody watches happen and everybody eventually
// notices. Ratio() answers the other question, and answering both with one
// number means one of them is wrong.
func TestTheEffectiveRatioIsWhatIsOnScreenAndTheRequestSurvives(t *testing.T) {
	// 20 columns: 19 available to the panes, and a minimum of 12 on pane A makes
	// any request below 12/19 unreachable.
	//
	// It STARTS at 0.8 deliberately. Starting at the default 0.5 would already
	// be clamped on the very first layout, so the division never moves when the
	// request does — and a commit that overwrote the request with the effective
	// value would be invisible, because the no-change guard returns before it.
	// The cells have to move for the overwrite to have anywhere to happen.
	h, s, sh := splitFixture(t, 20, 6, widget.WithMinSizes(12, 0), widget.WithRatio(0.8))
	if a := aCellsOn(t, h, s); a != 15 {
		t.Fatalf("pane A starts with %d cells, want the unclamped 15; the fixture is "+
			"not in the state this test needs", a)
	}

	h.onLoop(func() { s.SetRatio(0.1) })
	h.barrier(sh)

	if got := requestedRatioOn(t, h, s); got != 0.1 {
		t.Errorf("RequestedRatio() = %v, want the unclamped 0.1 that was asked for", got)
	}
	a, b, ok := cellsOn(t, h, s)
	if !ok {
		t.Fatal("Cells() reports nothing committed after a layout")
	}
	if a != 12 {
		t.Errorf("pane A holds %d cells, want the minimum of 12", a)
	}
	if a+b != 19 {
		t.Errorf("the panes hold %d cells between them, want the 19 available", a+b)
	}
	want := 12.0 / 19.0
	if got := ratioOn(t, h, s); math.Abs(got-want) > 1e-9 {
		t.Errorf("Ratio() = %v, want the effective %v that is actually on screen", got, want)
	}
}

// TestRatioBeforeTheFirstLayoutReportsTheRequest.
//
// There is no effective value yet, and reporting zero would be worse than
// reporting the intention: a caller reading back the ratio of a split it has
// just configured should get what it configured.
func TestRatioBeforeTheFirstLayoutReportsTheRequest(t *testing.T) {
	s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"},
		widget.WithRatio(0.25))
	if got := s.Ratio(); got != 0.25 {
		t.Errorf("Ratio() = %v before any layout, want the request 0.25", got)
	}
	if a, b, ok := s.Cells(); ok {
		t.Errorf("Cells() = (%d, %d, true) before the first layout, want ok=false; "+
			"there is no committed division to report", a, b)
	}
}

// TestTheResizeEventTimingIsExact.
//
// The table is the contract: nothing for the first layout, one event for a
// committed change carrying the CLAMPED result, and none at all for a request
// that moves no cells.
//
// The split is mounted AFTER the subscription so its first layout is actually
// observable — subscribing to an app that has already drawn a frame would make
// the first assertion vacuous.
func TestTheResizeEventTimingIsExact(t *testing.T) {
	root := tui.NewFlex(tui.Vertical)
	h := startApp(t, root, 40, 8)
	h.settle()

	resized := record[widget.SplitResizedEvent](h)
	s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"},
		widget.WithMinSizes(10, 0))
	h.onLoop(func() { root.AddWeighted(s, 1) })
	h.settle()

	if n := resized.count(); n != 0 {
		t.Fatalf("%d resize events for the split's FIRST layout, want none", n)
	}
	// Positive control for that zero: the instrument does observe this split.
	h.onLoop(func() { s.SetRatio(0.05) })
	h.waitFor("the clamped change committed", func() bool { return resized.count() == 1 })
	h.settle()

	ev, _ := resized.last()
	if ev.ACells != 10 {
		t.Errorf("the event reports %d cells for pane A, want the clamped 10", ev.ACells)
	}
	if ev.ACells+ev.BCells != 39 {
		t.Errorf("the event's cells sum to %d, want the 39 available", ev.ACells+ev.BCells)
	}
	if want := 10.0 / 39.0; math.Abs(ev.Ratio-want) > 1e-9 {
		t.Errorf("the event's Ratio = %v, want the effective %v", ev.Ratio, want)
	}
	if got := requestedRatioOn(t, h, s); got != 0.05 {
		t.Errorf("RequestedRatio() = %v, want the unclamped 0.05", got)
	}

	// A DIFFERENT request that lands on the same cells publishes nothing: the
	// divider did not move, whatever the arithmetic did.
	h.onLoop(func() { s.SetRatio(0.02) })
	h.settle()
	h.settle()
	if n := resized.count(); n != 1 {
		t.Errorf("%d resize events after a request that moved no cells, want still 1", n)
	}
	if got := requestedRatioOn(t, h, s); got != 0.02 {
		t.Errorf("RequestedRatio() = %v; the request is recorded even when it moves "+
			"no cells, or a later resize would restore the wrong division", got)
	}
}

// TestAnAmbientResizeCommitsAndAnnouncesItself.
//
// The terminal changing size moves the divider without anyone calling SetRatio.
// A Split that only published from its setter would report a stale division
// after every window resize, and the request must not be touched: the user did
// not ask for anything.
func TestAnAmbientResizeCommitsAndAnnouncesItself(t *testing.T) {
	h, s, sh := splitFixture(t, 20, 6)
	resized := record[widget.SplitResizedEvent](h)

	before := aCellsOn(t, h, s)
	h.tb.InjectResize(60, 10)
	h.barrier(sh)

	after := aCellsOn(t, h, s)
	if after == before {
		t.Fatalf("pane A still holds %d cells after the terminal went 20 → 60 wide", after)
	}
	if n := resized.count(); n != 1 {
		t.Errorf("%d resize events for the ambient resize, want exactly 1", n)
	}
	if ev, _ := resized.last(); ev.ACells != after {
		t.Errorf("the event reports %d cells, but the split holds %d", ev.ACells, after)
	}
	if got := requestedRatioOn(t, h, s); got != 0.5 {
		t.Errorf("RequestedRatio() = %v after an ambient resize, want the untouched 0.5", got)
	}
}

// TestADividerDragSurvivesThePointerLeavingTheSplit.
//
// This is the reason the divider took a capture. Before it did, the drag
// tracked a bool and the motion stopped arriving the moment the pointer left
// the Split's rect: the divider froze mid-drag and the release was never seen,
// so the flag stayed set and the next stray motion moved the divider again.
func TestADividerDragSurvivesThePointerLeavingTheSplit(t *testing.T) {
	s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"})
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(s, 1)
	root.Add(widget.NewText("below")) // one row, so row 11 is outside the split
	sh := newShell(root)
	h := startApp(t, sh, 40, 12)
	h.barrier(sh)

	a0 := aCellsOn(t, h, s)
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		// Out of the Split entirely, onto the row below it.
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 6, Y: 11},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: a0 + 6, Y: 11},
	)
	h.waitFor("the drag followed the pointer out of the split", func() bool {
		return aCellsOn(t, h, s) == a0+6
	})

	// The release ended the gesture: motion afterwards is not a drag.
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 12, Y: 11})
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0+6 {
		t.Errorf("pane A holds %d cells after motion following the release, want %d; "+
			"the gesture outlived its own release", got, a0+6)
	}
}

// TestCancellingADividerDragRestoresTheRequest.
//
// The REQUEST, not the effective ratio: cancelling must put back what the user
// had asked for rather than what a clamp had made of it, or a cancel on a
// narrow terminal quietly commits the clamp as their choice.
func TestCancellingADividerDragRestoresTheRequest(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8)

	beforeReq := requestedRatioOn(t, h, s)
	a0 := aCellsOn(t, h, s)

	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 8, Y: 0},
	)
	h.waitFor("the drag moved the divider", func() bool { return aCellsOn(t, h, s) == a0+8 })

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.barrier(sh)

	if got := aCellsOn(t, h, s); got != a0 {
		t.Errorf("pane A holds %d cells after the cancel, want the original %d", got, a0)
	}
	if got := requestedRatioOn(t, h, s); got != beforeReq {
		t.Errorf("RequestedRatio() = %v after a cancel, want the restored %v", got, beforeReq)
	}
	// The gesture is over: further motion is not a drag.
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 4, Y: 0})
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0 {
		t.Errorf("pane A holds %d cells after motion following a cancel, want %d", got, a0)
	}
}

// TestARevokedCaptureLeavesTheDivisionWhereItIs.
//
// A capture the runtime takes away is not a cancellation. The user dragged the
// divider somewhere; silently putting it back would be a change they never
// made. What the loss must do is END the gesture, so the next stray motion
// does not move a divider nobody is holding.
func TestARevokedCaptureLeavesTheDivisionWhereItIs(t *testing.T) {
	s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"})
	outside := &pane{fill: "o"}
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(s, 3)
	root.AddWeighted(outside, 1)
	sh := newShell(root)
	h := startApp(t, sh, 40, 12)
	h.inject(tab()) // focus inside the split, so leaving it is a real transition
	h.barrier(sh)

	a0 := aCellsOn(t, h, s)
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 5, Y: 0},
	)
	h.waitFor("the drag moved the divider", func() bool { return aCellsOn(t, h, s) == a0+5 })

	// Focus leaves the capture owner's subtree: the runtime revokes the capture.
	h.onLoop(func() { outside.Context().RequestFocus() })
	h.barrier(sh)

	if got := aCellsOn(t, h, s); got != a0+5 {
		t.Errorf("pane A holds %d cells after a revoked capture, want the %d the user "+
			"dragged it to", got, a0+5)
	}
	// And the gesture really is over — motion and a release move nothing.
	h.inject(
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 12, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: a0 + 12, Y: 0},
	)
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0+5 {
		t.Errorf("pane A holds %d cells after input following a revoked capture, want %d",
			got, a0+5)
	}
}

// TestAltArrowsStepAlongTheSplitsOwnAxisOnly.
//
// Alt-Left on a VERTICAL split is not a smaller step, it is a different
// gesture, and consuming it would swallow a binding the application may want.
func TestAltArrowsStepAlongTheSplitsOwnAxisOnly(t *testing.T) {
	s := widget.NewSplit(widget.Vertical, &pane{fill: "a"}, &pane{fill: "b"})
	sh := newShell(s)
	h := startApp(t, sh, 30, 20)
	h.inject(tab())
	h.barrier(sh)

	a0 := aCellsOn(t, h, s)

	h.inject(keyMod(tui.KeyLeft, tui.ModAlt))
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0 {
		t.Errorf("Alt-Left moved a VERTICAL split's divider to %d; it steps on its "+
			"own axis only", got)
	}
	bubbled := sh.bubbledKeys()
	if n := len(bubbled); n == 0 || bubbled[n-1].Code != tui.KeyLeft {
		t.Errorf("bubbled keys = %+v, want the off-axis Alt-Left to reach the "+
			"application unconsumed", bubbled)
	}
	offAxis := len(bubbled)

	// The on-axis key is the control for that: it is CONSUMED, so the count
	// does not move. Without this the assertion above would also pass for a
	// split that consumed nothing at all.
	h.inject(keyMod(tui.KeyDown, tui.ModAlt))
	h.waitFor("Alt-Down stepped the vertical split", func() bool {
		return aCellsOn(t, h, s) == a0+1
	})
	h.barrier(sh)
	if n := len(sh.bubbledKeys()); n != offAxis {
		t.Errorf("%d keys bubbled after the on-axis Alt-Down, want still %d; the "+
			"step was published AND handed on", n, offAxis)
	}
}

// TestANonPrimaryReleaseDoesNotEndADividerDrag.
//
// A middle-click release arriving mid-drag is not the user letting go of the
// divider. Ending on it would strand the gesture's state on a button it never
// started with.
func TestANonPrimaryReleaseDoesNotEndADividerDrag(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8)

	a0 := aCellsOn(t, h, s)
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseMiddle, X: a0, Y: 0},
	)
	h.barrier(sh)
	// The gesture is still running, so motion still moves the divider.
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 3, Y: 0})
	h.waitFor("the primary drag survived a stray middle release", func() bool {
		return aCellsOn(t, h, s) == a0+3
	})
}

// TestAZoomedSplitHasNoDividerToDragOrStep.
//
// While one pane fills the rect there is no divider on screen. Input that
// pretended otherwise would move a division the user cannot see, and they
// would find it moved when they unzoomed.
func TestAZoomedSplitHasNoDividerToDragOrStep(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8)

	a0 := aCellsOn(t, h, s)
	h.onLoop(func() { s.Zoom(widget.PaneA) })
	h.barrier(sh)

	h.inject(
		keyMod(tui.KeyRight, tui.ModAlt),
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 5, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: a0 + 5, Y: 0},
	)
	h.barrier(sh)

	if got := requestedRatioOn(t, h, s); got != 0.5 {
		t.Errorf("RequestedRatio() = %v after input to a zoomed split, want the "+
			"untouched 0.5", got)
	}
	h.onLoop(func() { s.Zoom(widget.PaneNone) })
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0 {
		t.Errorf("pane A holds %d cells after unzooming, want the untouched %d", got, a0)
	}
}

// TestZoomingEndsADividerDragInProgress.
//
// The divider the user is holding disappears under a zoom, and none of the
// runtime's automatic capture losses fire for it: the Split stays mounted,
// visible and focused. So the gesture has to be ended explicitly, or it waits —
// holding the pointer — for the unzoom, and the next motion moves a divider
// nobody is dragging.
//
// The division reached before the zoom STANDS. Zooming is not a cancel, and the
// user's drag up to that point was real.
func TestZoomingEndsADividerDragInProgress(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8)

	a0 := aCellsOn(t, h, s)
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 5, Y: 0},
	)
	h.waitFor("the drag moved the divider", func() bool { return aCellsOn(t, h, s) == a0+5 })

	h.onLoop(func() { s.Zoom(widget.PaneA) })
	h.barrier(sh)
	h.onLoop(func() { s.Zoom(widget.PaneNone) })
	h.barrier(sh)

	if got := aCellsOn(t, h, s); got != a0+5 {
		t.Fatalf("pane A holds %d cells after a zoom round trip, want the %d the drag "+
			"had reached", got, a0+5)
	}
	// The gesture did not wait out the zoom: motion afterwards is not a drag.
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 12, Y: 0})
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0+5 {
		t.Errorf("pane A holds %d cells after motion following the unzoom, want %d; "+
			"the drag survived the divider vanishing", got, a0+5)
	}
}

// TestADragEmitsOneEventPerCOMMITTEDStep.
//
// The count is per committed DIVISION, not per motion event. A drag that
// crosses three cell boundaries publishes three times; motion within one cell
// publishes nothing, however much of it there is. An application that persists
// on this event, or recomputes a layout from it, is doing work per publication
// — so "one per pixel of mouse travel" and "one per cell the divider actually
// moved" are very different contracts.
func TestADragEmitsOneEventPerCOMMITTEDStep(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8)
	resized := record[widget.SplitResizedEvent](h)

	a0 := aCellsOn(t, h, s)
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0})

	// Three distinct positions, each a real move.
	for _, dx := range []int{1, 2, 3} {
		h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + dx, Y: 0})
		h.waitFor("the divider reached the next cell", func() bool {
			return aCellsOn(t, h, s) == a0+dx
		})
	}
	h.barrier(sh)
	if n := resized.count(); n != 3 {
		t.Errorf("%d events for three committed positions, want 3", n)
	}

	// More motion, SAME cell: the divider has not moved, so nothing is
	// published — which is the half a per-motion implementation gets wrong.
	h.inject(
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 3, Y: 1},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 3, Y: 2},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 3, Y: 3},
	)
	h.barrier(sh)
	if n := resized.count(); n != 3 {
		t.Errorf("%d events after motion that moved no cells, want still 3", n)
	}

	// And the release at the same position adds nothing either.
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: a0 + 3, Y: 3})
	h.barrier(sh)
	if n := resized.count(); n != 3 {
		t.Errorf("%d events after the release, want still 3; a release that moves the "+
			"divider nowhere is not a resize", n)
	}
}
