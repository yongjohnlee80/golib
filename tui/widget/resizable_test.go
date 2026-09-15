package widget_test

// A Resizable owns a size and hands it down as constraints. Almost every
// interesting failure is about WHO owns what: whether the child was measured
// under the constraints it was then placed in, whether a size that was clamped
// is reported as the size that was asked for, and whether a drag the runtime
// interrupted quietly reverts work the user did.

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// sizedChild reports a size it is told to report, so a test can make the child's
// intrinsic size change under an auto wrapper.
type sizedChild struct {
	widget.Base
	pref    tui.Size
	sawMinW int
	sawMaxW int
}

func (c *sizedChild) Layout(cs tui.Constraints) tui.Size {
	c.sawMinW, c.sawMaxW = cs.MinW, cs.MaxW
	return cs.Constrain(c.pref)
}

func (c *sizedChild) Render(s tui.Surface) {
	sz := s.Size()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, "·", style.New())
}

func sizeOn(t *testing.T, h *harness, r *widget.Resizable) tui.Size {
	t.Helper()
	var s tui.Size
	h.onLoop(func() { s = r.Size() })
	return s
}

// TestAutoTracksTheChildEveryPass.
//
// Auto is CONTINUOUS, not a one-shot adoption. A wrapper that adopted the
// child's first size and held it would freeze a growing child forever while
// still reporting itself as tracking one.
func TestAutoTracksTheChildEveryPass(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithHandles())
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	if got := sizeOn(t, h, r); got != (tui.Size{W: 6, H: 3}) {
		t.Fatalf("Size() = %+v, want the child's intrinsic 6x3", got)
	}
	if mode := modeOn(t, h, r); mode != widget.SizeAuto {
		t.Errorf("SizeMode() = %v, want auto", mode)
	}
	if _, ok := requestedOn(t, h, r); ok {
		t.Error("RequestedSize reported a request in auto mode; auto has none, and " +
			"persisting one would mistake a tracked size for a user's choice")
	}

	h.onLoop(func() {
		child.pref = tui.Size{W: 10, H: 5}
		child.Context().RequestLayout()
	})
	h.settle()
	if got := sizeOn(t, h, r); got != (tui.Size{W: 10, H: 5}) {
		t.Errorf("Size() = %+v after the child grew, want 10x5; auto stopped tracking", got)
	}
}

func modeOn(t *testing.T, h *harness, r *widget.Resizable) widget.SizeMode {
	t.Helper()
	var m widget.SizeMode
	h.onLoop(func() { m = r.SizeMode() })
	return m
}

func requestedOn(t *testing.T, h *harness, r *widget.Resizable) (tui.Size, bool) {
	t.Helper()
	var s tui.Size
	var ok bool
	h.onLoop(func() { s, ok = r.RequestedSize() })
	return s, ok
}

// TestTheChildIsMeasuredUnderTheConstraintsItIsPlacedIn.
//
// The ordering rule that makes the whole thing sound: constraints are derived
// from the parent's BEFORE the child is measured. Measuring first and clamping
// afterwards lets the child choose outside the configured bounds and then be
// placed into a rect it never saw — which breaks constraints-down/sizes-up and
// desynchronises render, hit geometry and every descendant's placement.
func TestTheChildIsMeasuredUnderTheConstraintsItIsPlacedIn(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 100, H: 100}} // asks for far too much
	r := widget.NewResizable(child,
		widget.WithHandles(),
		widget.WithMaxSize(tui.Size{W: 12, H: 6}))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	var sawMax int
	h.onLoop(func() { sawMax = child.sawMaxW })
	if sawMax != 12 {
		t.Errorf("the child was measured with MaxW=%d, want the configured 12 — it "+
			"must see the bound before it chooses, not after", sawMax)
	}
	if got := sizeOn(t, h, r); got.W != 12 {
		t.Errorf("Size() = %+v, want the configured maximum", got)
	}
}

// TestReserveCostsTheChildCellsAndOverlayDoesNot.
//
// A resize affordance should not resize the thing it is attached to merely by
// existing, which is why overlay is the default. Reserve is for callers who
// would rather lose a cell than occlude one.
func TestReserveCostsTheChildCellsAndOverlayDoesNot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     widget.PlacementMode
		wantSize tui.Size
	}{
		// Overlay draws over the child's corner: the wrapper IS the child.
		{"overlay", widget.PlacementOverlay, tui.Size{W: 8, H: 4}},
		// Reserve gives the grip its own cells: child + handle, exactly.
		{"reserve", widget.PlacementReserve, tui.Size{W: 9, H: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
			r := widget.NewResizable(child, widget.WithHandlePlacement(tc.mode))
			host := widget.NewOverlayHost(r)
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.settle()

			if got := sizeOn(t, h, r); got != tc.wantSize {
				t.Errorf("Size() = %+v, want %+v", got, tc.wantSize)
			}
			// And the child's own ceiling shrank only under reserve, which is
			// what stops it choosing a size the grip would then have to overlap.
			var childMax int
			h.onLoop(func() { childMax = child.sawMaxW })
			want := 40
			if tc.mode == widget.PlacementReserve {
				want = 39
			}
			if childMax != want {
				t.Errorf("the child was measured with MaxW=%d, want %d", childMax, want)
			}
		})
	}
}

// TestSetSizeIsARequestAndSizeIsWhatWasReached.
//
// Requested and effective are different questions and both are published,
// because persistence stores the request while the screen shows the result. A
// wrapper that reported one as the other would either lose the user's choice on
// restore or claim a size it does not have.
func TestSetSizeIsARequestAndSizeIsWhatWasReached(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 4, H: 2}}
	r := widget.NewResizable(child,
		widget.WithHandles(),
		widget.WithMaxSize(tui.Size{W: 10, H: 10}))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	h.onLoop(func() { r.SetSize(tui.Size{W: 50, H: 50}) })
	h.settle()

	if got, ok := requestedOn(t, h, r); !ok || got != (tui.Size{W: 50, H: 50}) {
		t.Errorf("RequestedSize() = %+v,%v; the unclamped request must survive for "+
			"persistence", got, ok)
	}
	if got := sizeOn(t, h, r); got != (tui.Size{W: 10, H: 10}) {
		t.Errorf("Size() = %+v, want the clamped 10x10 that is actually on screen", got)
	}
	if mode := modeOn(t, h, r); mode != widget.SizeExplicit {
		t.Errorf("SizeMode() = %v after SetSize, want explicit", mode)
	}

	// SetAuto returns to tracking, and the request goes with it.
	h.onLoop(func() { r.SetAuto() })
	h.settle()
	if _, ok := requestedOn(t, h, r); ok {
		t.Error("RequestedSize still reports a request after SetAuto")
	}
	if got := sizeOn(t, h, r); got != (tui.Size{W: 4, H: 2}) {
		t.Errorf("Size() = %+v after SetAuto, want the child's intrinsic size", got)
	}
}

// TestTheResizeEventComesFromCommitWithTheRightTiming.
//
// Timing is the contract, not the final value: nothing before the first layout,
// NO event on the first layout — there was no previous size to differ from — and
// none for a change that moves no cells.
func TestTheResizeEventComesFromCommitWithTheRightTiming(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithHandles(),
		widget.WithMaxSize(tui.Size{W: 20, H: 20}))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()

	var events []widget.ResizedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.ResizedEvent) {
		events = append(events, ev)
	})
	defer unsub()
	h.settle()
	h.settle()

	h.onLoop(func() {
		if len(events) != 0 {
			t.Errorf("%d events for the first layout, want none: there was no previous "+
				"size to differ from", len(events))
		}
	})

	h.onLoop(func() { r.SetSize(tui.Size{W: 12, H: 6}) })
	h.settle()
	h.settle()
	h.onLoop(func() {
		if len(events) != 1 {
			t.Fatalf("%d events for one committed change, want 1", len(events))
		}
		if events[0].Size != (tui.Size{W: 12, H: 6}) {
			t.Errorf("the event carries %+v, want the effective 12x6", events[0].Size)
		}
		if events[0].Owner != r.NodeID() {
			t.Errorf("the event names node %d, want the WRAPPER %d",
				events[0].Owner, r.NodeID())
		}
	})

	// A request that changes no cells publishes nothing.
	h.onLoop(func() { r.SetSize(tui.Size{W: 12, H: 6}) })
	h.settle()
	h.settle()
	h.onLoop(func() {
		if len(events) != 1 {
			t.Errorf("%d events after a no-op resize, want still 1", len(events))
		}
	})
}

// TestAmbientResizeCommitsWithoutAnySetSizeCall.
//
// The parent's constraints changing is a resize the user caused with the window
// manager rather than with a grip, and it must reach Size() and the bus the same
// way. A wrapper that only updated on SetSize would report a stale size after
// every terminal resize.
func TestAmbientResizeCommitsWithoutAnySetSizeCall(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 100, H: 100}}
	r := widget.NewResizable(child, widget.WithHandles())
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 30, 10)
	defer h.stop()
	h.settle()

	first := sizeOn(t, h, r)
	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.ResizedEvent) { events.Add(1) })
	defer unsub()

	h.tb.InjectResize(50, 16)
	h.waitFor("the ambient resize committed", func() bool {
		return sizeOn(t, h, r) != first
	})
	h.settle()
	if events.Load() == 0 {
		t.Error("an ambient resize changed the size without publishing anything")
	}
}

// TestDraggingAGripResizes.
func TestDraggingAGripResizes(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	before := sizeOn(t, h, r)
	gx, gy := before.W-1, before.H-1
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: gx, Y: gy})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: gx + 4, Y: gy + 2})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: gx + 4, Y: gy + 2})
	h.waitFor("the drag resized the wrapper", func() bool {
		return sizeOn(t, h, r) == tui.Size{W: before.W + 4, H: before.H + 2}
	})
}

// TestCancellingADragRestoresTheSizeAndTheMode.
//
// The MODE too. Cancelling a drag that switched an auto wrapper to explicit must
// return it to auto, or the wrapper silently stops tracking its child because
// the user changed their mind mid-gesture.
func TestCancellingADragRestoresTheSizeAndTheMode(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()
	h.onLoop(func() { r.Context().RequestFocus() })
	h.settle()

	before := sizeOn(t, h, r)
	gx, gy := before.W-1, before.H-1
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: gx, Y: gy})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: gx + 5, Y: gy + 3})
	h.waitFor("the drag moved it", func() bool { return sizeOn(t, h, r) != before })

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("the cancel restored the size", func() bool { return sizeOn(t, h, r) == before })
	h.settle()
	if mode := modeOn(t, h, r); mode != widget.SizeAuto {
		t.Errorf("SizeMode() = %v after cancelling a drag that began in auto, want auto",
			mode)
	}
}

// TestKeyboardResizingWorksWithThePointerDisabled.
//
// The parity that makes the pointer policy safe to use. Every resize action
// resolves on the wrapper, so disabling the pointer path disables the grips and
// nothing else.
func TestKeyboardResizingWorksWithThePointerDisabled(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15})).
		WithPointerPolicy(tui.PointerDisabled)
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.onLoop(func() { r.Context().RequestFocus() })
	h.settle()

	before := sizeOn(t, h, r)

	// The grip is inert.
	gx, gy := before.W-1, before.H-1
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: gx, Y: gy})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: gx + 4, Y: gy + 2})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: gx + 4, Y: gy + 2})
	h.settle()
	h.settle()
	if got := sizeOn(t, h, r); got != before {
		t.Errorf("a drag resized a pointer-disabled wrapper to %+v", got)
	}

	// The keyboard is not.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModShift})
	h.waitFor("the keyboard resized it", func() bool {
		return sizeOn(t, h, r).W == before.W+1
	})
}

// TestAWrapperUnderATightParentTellsTheTruth.
//
// A fixed outer rect is ordinary layout composition, not invalid input. The
// wrapper reports the size it actually reached, a resize that cannot change
// cells moves nothing and emits nothing, and RequestedSize still reports what was
// asked for — no special case, and it behaves the same through any nesting depth.
func TestAWrapperUnderATightParentTellsTheTruth(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 4, H: 2}}
	r := widget.NewResizable(child, widget.WithHandles())
	// A two-axis size fraction is the canonical tight parent: the outer policy
	// fixes both axes, so the wrapper cannot change the visible rectangle.
	f := widget.NewFloat(r, widget.WithSizeFraction(25, 25))
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.onLoop(func() {
		host.Attach(f)
		f.Show()
	})
	h.settle()

	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.ResizedEvent) { events.Add(1) })
	defer unsub()

	fixed := sizeOn(t, h, r)
	h.onLoop(func() { r.SetSize(tui.Size{W: 30, H: 12}) })
	h.settle()
	h.settle()

	if got := sizeOn(t, h, r); got != fixed {
		t.Errorf("Size() = %+v under a tight parent, want the unchanged %+v", got, fixed)
	}
	if got, ok := requestedOn(t, h, r); !ok || got != (tui.Size{W: 30, H: 12}) {
		t.Errorf("RequestedSize() = %+v,%v; the request survives even when it cannot "+
			"be honoured", got, ok)
	}
	if events.Load() != 0 {
		t.Errorf("%d events for a resize that moved no cells", events.Load())
	}
}

// TestGripsAreNotTabStops.
//
// Adding nodes to the tree must not pollute Tab order. A grip is a pointer
// affordance, and a traversal that stopped on one would give the user a stop
// where nothing visibly happens.
func TestGripsAreNotTabStops(t *testing.T) {
	child := widget.NewButton("inside")
	r := widget.NewResizable(child, widget.WithHandles(
		widget.HandleBottomRight, widget.HandleRight, widget.HandleBottom))
	after := widget.NewButton("after")
	root := tui.NewFlex(tui.Vertical)
	root.Add(r, after)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	// Three Tabs from nothing: wrapper, child, after — and back round. No stop
	// may be a grip, which is observable as the count of distinct stops.
	stops := map[string]int{}
	for range 6 {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
		h.settle()
		h.onLoop(func() {
			switch {
			case r.Context().Focused():
				stops["wrapper"]++
			case child.Context() != nil && child.Context().Focused():
				stops["child"]++
			case after.Context() != nil && after.Context().Focused():
				stops["after"]++
			default:
				stops["other"]++
			}
		})
	}
	if stops["other"] != 0 {
		t.Errorf("focus landed somewhere that is not the wrapper, its child or its "+
			"sibling %d times; a grip became a tab stop", stops["other"])
	}
}

// TestConstructionRefusesWhatCannotBeHonoured.
func TestConstructionRefusesWhatCannotBeHonoured(t *testing.T) {
	child := widget.NewText("x")
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"a nil child", func() { widget.NewResizable(nil) }},
		{"a minimum above the maximum", func() {
			widget.NewResizable(child,
				widget.WithMinSize(tui.Size{W: 10, H: 10}),
				widget.WithMaxSize(tui.Size{W: 5, H: 5}))
		}},
		{"a zero initial size", func() {
			widget.NewResizable(child, widget.WithInitialSize(tui.Size{W: 0, H: 4}))
		}},
		{"an undeclared handle", func() {
			widget.NewResizable(child, widget.WithHandles(widget.HandleBottom+1))
		}},
		{"an undeclared placement mode", func() {
			widget.NewResizable(child, widget.WithHandlePlacement(widget.PlacementReserve+1))
		}},
		{"a non-positive step", func() {
			widget.NewResizable(child, widget.WithResizeStep(0, widget.StepCells))
		}},
		{"an undeclared step unit", func() {
			widget.NewResizable(child, widget.WithResizeStep(1, widget.StepPercent+1))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if f := fatalFromWidgetExt(tc.call); f == nil {
				t.Error("it was accepted")
			}
		})
	}
	// The controls: the last valid value of each closed set is accepted.
	if f := fatalFromWidgetExt(func() {
		widget.NewResizable(child,
			widget.WithHandles(widget.HandleBottom),
			widget.WithHandlePlacement(widget.PlacementReserve),
			widget.WithResizeStep(1, widget.StepPercent))
	}); f != nil {
		t.Errorf("a legal configuration was rejected: %v", f.Rule)
	}
}

// TestAGripThatWillNotFitIsDroppedForTheFrame.
//
// The wrapper never renders an affordance it cannot fit, and it never drives the
// child below its minimum to make room for one. A one-cell box with a grip over
// its only cell shows nothing at all.
func TestAGripThatWillNotFitIsDroppedForTheFrame(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 4, H: 4}}
	r := widget.NewResizable(child, widget.WithHandlePlacement(widget.PlacementReserve))
	host := widget.NewOverlayHost(r)
	// A ONE-CELL terminal: the wrapper cannot be larger than its parent, so
	// after the grip's reserved cell there is nothing left for the child, and a
	// grip painted over the only cell would show the affordance and no content.
	h := startApp(t, host, 1, 1)
	defer h.stop()
	h.settle()

	if got := h.grid(); strings.Contains(got, "◢") {
		t.Errorf("a grip was painted into a box too small for one:\n%s", got)
	}
	// And the child still got its cell.
	if got := sizeOn(t, h, r); got.W < 1 || got.H < 1 {
		t.Errorf("Size() = %+v; the wrapper collapsed making room for a grip", got)
	}
}
