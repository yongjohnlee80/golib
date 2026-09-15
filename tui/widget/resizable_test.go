package widget_test

// A Resizable owns a size and hands it down as constraints. Almost every
// interesting failure is about WHO owns what: whether the child was measured
// under the constraints it was then placed in, whether a size that was clamped
// is reported as the size that was asked for, and whether a drag the runtime
// interrupted quietly reverts work the user did.

import (
	"os"
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
	passes  int
}

// layouts reports how many times this child has been measured, read the
// sanctioned way. One change must cost one measurement.
func (c *sizedChild) layouts() int { return c.passes }

func (c *sizedChild) Layout(cs tui.Constraints) tui.Size {
	c.sawMinW, c.sawMaxW = cs.MinW, cs.MaxW
	c.passes++
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
	// The wrapper is MOUNTED AFTER the subscription, which is the whole reason
	// the first assertion means anything: an app that has already drawn a frame
	// has already run the first commit, so subscribing afterwards would report
	// zero events whether or not the first layout published one.
	// A LOOSE container, so the wrapper chooses its own size — and one the test
	// attaches to AFTER subscribing, which is the whole reason the first
	// assertion means anything: an app that has already drawn a frame has
	// already run the first commit, so subscribing afterwards would report zero
	// events whether or not the first layout published one.
	root := &looseCeiling{w: 30, hh: 18}
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	var events []widget.ResizedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.ResizedEvent) {
		events = append(events, ev)
	})
	defer unsub()

	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithHandles(),
		widget.WithMaxSize(tui.Size{W: 20, H: 20}))
	h.onLoop(func() { root.attach(r) })
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
	// The SAME tree twice, wrapped and unwrapped, with the Tab order recorded
	// from each. Counting how often focus lands somewhere unexpected cannot see
	// the defect: a grip that became focusable is still "the wrapper's subtree",
	// and a count has nothing to compare itself against. Two orders do.
	order := func(wrap bool) []string {
		before := widget.NewButton("before")
		inner := widget.NewButton("inner")
		after := widget.NewButton("after")
		var middle tui.Component = inner
		if wrap {
			middle = widget.NewResizable(inner, widget.WithHandles(
				widget.HandleBottomRight, widget.HandleRight, widget.HandleBottom))
		}
		root := tui.NewFlex(tui.Vertical)
		root.Add(before, middle, after)
		host := widget.NewOverlayHost(root)
		h := startApp(t, host, 40, 20)
		defer h.stop()
		h.settle()

		var stops []string
		for range 5 {
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
			h.settle()
			h.onLoop(func() {
				switch {
				case before.Context() != nil && before.Context().Focused():
					stops = append(stops, "before")
				case inner.Context() != nil && inner.Context().Focused():
					stops = append(stops, "inner")
				case after.Context() != nil && after.Context().Focused():
					stops = append(stops, "after")
				case wrap && middle.(*widget.Resizable).Context().Focused():
					stops = append(stops, "wrapper")
				default:
					stops = append(stops, "OTHER")
				}
			})
		}
		return stops
	}

	bare := order(false)
	wrapped := order(true)

	// The ONLY stop wrapping may add is the wrapper itself, which has to be a
	// tab stop for the keyboard resize vocabulary to be reachable at all. Strip
	// it and the two orders must be identical: three grips contributed nothing.
	var stripped []string
	for _, s := range wrapped {
		if s != "wrapper" {
			stripped = append(stripped, s)
		}
	}
	if strings.Join(stripped, ",") != strings.Join(bare[:len(stripped)], ",") {
		t.Errorf("Tab order with the wrapper is %v (minus the wrapper: %v), bare is %v; "+
			"apart from the wrapper itself, wrapping must change traversal in no way",
			wrapped, stripped, bare)
	}
	for _, s := range wrapped {
		if s == "OTHER" {
			t.Errorf("focus landed on none of the three controls or the wrapper: %v — "+
				"a grip became a tab stop", wrapped)
			break
		}
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

// looseRow lays two children side by side under LOOSE constraints for the first
// of them, so a Resizable inside it is free to choose its own size. A Flex would
// hand it a tight cell and there would be nothing left to observe.
type looseRow struct {
	widget.Base
	a, b tui.Component
}

func (l *looseRow) Init(ctx *tui.Context) {
	l.Base.Init(ctx)
	ctx.Mount(l.a)
	ctx.Mount(l.b)
}

func (l *looseRow) Layout(c tui.Constraints) tui.Size {
	ctx := l.Context()
	half := c.MaxW / 2
	sa := ctx.LayoutChild(l.a, tui.Constraints{MaxW: half, MaxH: c.MaxH})
	ctx.PlaceChild(l.a, tui.Rect{X: 0, Y: 0, W: sa.W, H: sa.H})
	sb := ctx.LayoutChild(l.b, tui.Tight(tui.Size{W: c.MaxW - half, H: c.MaxH}))
	ctx.PlaceChild(l.b, tui.Rect{X: half, Y: 0, W: sb.W, H: sb.H})
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}

func (l *looseRow) Render(tui.Surface) {}

// TestARevokedCaptureLeavesTheResizeWhereItIs.
//
// A capture the runtime takes away is not a cancellation: the user dragged the
// wrapper to a size, and silently putting it back would undo work they did and
// never asked to undo. What the loss must do is END the gesture, so the next
// stray motion does not resize a box nobody is holding.
func TestARevokedCaptureLeavesTheResizeWhereItIs(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 18, H: 15}))
	other := &pane{fill: "o"}
	host := widget.NewOverlayHost(&looseRow{a: r, b: other})
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.onLoop(func() { r.Context().RequestFocus() })
	h.settle()

	before := sizeOn(t, h, r)
	gx, gy := before.W-1, before.H-1
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: gx, Y: gy})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: gx + 4, Y: gy + 2})
	dragged := tui.Size{W: before.W + 4, H: before.H + 2}
	h.waitFor("the drag resized the wrapper", func() bool { return sizeOn(t, h, r) == dragged })

	// Focus leaves the grip's subtree: the runtime revokes the capture.
	h.onLoop(func() { other.Context().RequestFocus() })
	h.settle()
	h.settle()
	if got := sizeOn(t, h, r); got != dragged {
		t.Errorf("Size() = %+v after a revoked capture, want the %+v the user dragged "+
			"it to", got, dragged)
	}

	// And the gesture really is over — motion and a release resize nothing.
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: gx + 9, Y: gy + 5})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: gx + 9, Y: gy + 5})
	h.settle()
	h.settle()
	if got := sizeOn(t, h, r); got != dragged {
		t.Errorf("Size() = %+v after input following a revoked capture, want %+v",
			got, dragged)
	}
}

// TestAPercentStepAlwaysMovesAtLeastOneCell.
//
// A percentage of a small box rounds to zero, and a keypress that provably
// cannot move anything is worse than a slow one: the user presses it, sees
// nothing, and concludes the control is broken rather than that their box is
// small.
func TestAPercentStepAlwaysMovesAtLeastOneCell(t *testing.T) {
	// 5% of a 6x3 box is zero cells on both axes under integer division.
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child,
		widget.WithMaxSize(tui.Size{W: 30, H: 15}),
		widget.WithResizeStep(5, widget.StepPercent))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.onLoop(func() { r.Context().RequestFocus() })
	h.settle()

	before := sizeOn(t, h, r)
	if before != (tui.Size{W: 6, H: 3}) {
		t.Fatalf("Size() = %+v, want the 6x3 that makes a 5%% step round to zero", before)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModShift})
	h.waitFor("the step moved one cell", func() bool {
		return sizeOn(t, h, r).W == before.W+1
	})
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown, Mods: tui.ModShift})
	h.waitFor("the step moved one cell on the other axis too", func() bool {
		return sizeOn(t, h, r).H == before.H+1
	})
}

// TestWrappingMakesAnUnmodifiedWidgetResizable.
//
// THE OPEN-FOR-EXTENSION CLAIM, tested rather than asserted. Resizing is a
// wrapper, so
// a widget becomes resizable by being wrapped — it is not modified, implements
// no interface, and never learns that resizing exists. Three children, two of
// them substantial widgets from this package and one defined in this external
// test package, which is the case that proves nothing in the mechanism is
// package-private.
//
// The source check is the other half. A test could pass while `Editor` had
// quietly grown a resize hook; asserting the child's source contains no mention
// of resizing is what makes "no edit to any of them" a fact rather than a habit.
func TestWrappingMakesAnUnmodifiedWidgetResizable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func() tui.Component
	}{
		{"Editor", func() tui.Component { return widget.NewEditor() }},
		{"TextArea", func() tui.Component { return widget.NewTextArea() }},
		// Defined in THIS package: a consumer's own component, which the widget
		// package has never heard of.
		{"an out-of-package component", func() tui.Component {
			return &sizedChild{pref: tui.Size{W: 5, H: 3}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := widget.NewResizable(tc.build(),
				widget.WithInitialSize(tui.Size{W: 10, H: 4}),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(r)
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.onLoop(func() { r.Context().RequestFocus() })
			h.settle()

			if got := sizeOn(t, h, r); got != (tui.Size{W: 10, H: 4}) {
				t.Fatalf("Size() = %+v, want the requested 10x4", got)
			}
			// And it RESIZES, from the keyboard, with no cooperation from the
			// child whatsoever.
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModShift})
			h.waitFor("the wrapped widget resized", func() bool {
				return sizeOn(t, h, r).W == 11
			})
		})
	}
}

// TestTheWrappedWidgetsSourceNeverMentionsResizing.
//
// The static half of the OCP claim: the widgets above are resizable and their
// own source knows nothing about it. A wrapper that had quietly required a hook
// in its children would still pass the behavioural test.
func TestTheWrappedWidgetsSourceNeverMentionsResizing(t *testing.T) {
	for _, name := range []string{"editor.go", "textarea.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, term := range []string{"Resizable", "ResizeStepAction", "resizeHandle", "SizeMode"} {
			if strings.Contains(string(b), term) {
				t.Errorf("%s mentions %q; a wrapped widget must not know that resizing "+
					"exists, or the wrapper has stopped being open-for-extension",
					name, term)
			}
		}
	}
}

// TestAutoTracksTheChildInONELayoutPassPerFrame.
//
// Auto re-measures every pass, and the risk in that is a wrapper and child that
// chase each other: the commit stores a size, which dirties layout, which
// measures again. One pass per frame is the contract — and a child collapsed to
// 0x0 is the shape the chase produces when it does go wrong.
func TestAutoTracksTheChildInONELayoutPassPerFrame(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 7, H: 3}}
	r := widget.NewResizable(child, widget.WithHandles())
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	before := child.layouts()
	// One change, one frame: the child is measured once more, not twice.
	h.onLoop(func() {
		child.pref = tui.Size{W: 9, H: 5}
		child.Context().RequestLayout()
	})
	h.settle()

	if got := sizeOn(t, h, r); got != (tui.Size{W: 9, H: 5}) {
		t.Errorf("Size() = %+v after the child grew, want 9x5", got)
	}
	if got := child.layouts() - before; got != 1 {
		t.Errorf("the child was measured %d times for one change, want 1; the wrapper "+
			"and the child are chasing each other across passes", got)
	}
	if got := sizeOn(t, h, r); got.W == 0 || got.H == 0 {
		t.Errorf("Size() = %+v; auto collapsed the wrapper", got)
	}
}

// TestAResizableInsideAFloatReportsTheFloatsTruth.
//
// Float composition precedence. A NATURAL-sized Float
// leaves the content to choose, so the wrapper inside it resizes normally.
// Under AtRect or a two-axis fraction the Float imposes a tight size, and then
// the wrapper reports THAT — a drag moves nothing and emits nothing. Not an
// error and not a special case: the wrapper is telling the truth about a
// constraint its parent set, which is the same rule as everywhere else.
func TestAResizableInsideAFloatReportsTheFloatsTruth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  []widget.FloatOption
		fixed bool
	}{
		{"natural size", nil, false},
		{"AtRect", []widget.FloatOption{
			widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 14, H: 6}))}, true},
		{"two-axis fraction", []widget.FloatOption{
			widget.WithSizeFraction(40, 40)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
			r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			// Nesting depth, through a container that PASSES CONSTRAINTS ON
			// unchanged. A Flex would stretch the wrapper to the Float's rect
			// and make every case the fixed one, which would test the fixture
			// rather than the Float's policy.
			f := widget.NewFloat(&passthrough{child: r}, tc.opts...)
			host := widget.NewOverlayHost(widget.NewText("base"))
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.onLoop(func() {
				host.Attach(f)
				f.Show()
			})
			h.settle()

			before := sizeOn(t, h, r)
			resized := record[widget.ResizedEvent](h)
			h.onLoop(func() { r.Context().RequestFocus() })
			h.settle()
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModShift})
			h.settle()
			h.settle()
			after := sizeOn(t, h, r)

			if tc.fixed {
				if after != before {
					t.Errorf("Size() moved %+v -> %+v inside a fixed-rect Float; the "+
						"parent's tight constraint wins and the wrapper reports it",
						before, after)
				}
				if n := resized.count(); n != 0 {
					t.Errorf("%d ResizedEvent(s) for a resize that changed no cells", n)
				}
				return
			}
			if after.W != before.W+1 {
				t.Errorf("Size() = %+v inside a natural-sized Float, want one cell wider "+
					"than %+v — the content chooses, so the wrapper resizes", after, before)
			}
		})
	}
}

// passthrough hands its constraints to its single child untouched and takes the
// child's size, so a test can add nesting depth without adding a layout policy.
type passthrough struct {
	widget.Base
	child tui.Component
}

func (p *passthrough) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	ctx.Mount(p.child)
}

func (p *passthrough) Layout(cs tui.Constraints) tui.Size {
	ctx := p.Context()
	got := ctx.LayoutChild(p.child, cs)
	ctx.PlaceChild(p.child, tui.Rect{X: 0, Y: 0, W: got.W, H: got.H})
	return got
}

func (p *passthrough) Render(tui.Surface) {}

// TestARequestLargerThanTheParentNeverReachesTheChild.
//
// The wrapper clamps in two places and they cover different things. The final
// Constrain on the returned size keeps the WRAPPER legal; this is about the
// CHILD's box, which only the request-side clamp can get right — and under a
// LOOSE parent the two come apart. Ask for 50 cells inside a 20-cell ceiling: if
// the request went down untouched, the child would be measured at 50 and then
// placed at 50 inside a wrapper the parent has pinned at 20, which is the
// desynchronised render-versus-hit-geometry the constraints rule exists to
// prevent. The wrapper's own reported size is 20 either way, so a test that
// looks only at Size() cannot see it.
func TestARequestLargerThanTheParentNeverReachesTheChild(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 4, H: 2}}
	r := widget.NewResizable(child,
		widget.WithHandles(),
		widget.WithInitialSize(tui.Size{W: 50, H: 30}),
		widget.WithMaxSize(tui.Size{W: 100, H: 100}))
	// A LOOSE 20x10 ceiling: the parent allows anything up to it, so nothing
	// downstream of the child's measurement will correct an over-large box.
	host := widget.NewOverlayHost(&looseCeiling{child: r, w: 20, hh: 10})
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	var sawW int
	h.onLoop(func() { sawW = child.sawMaxW })
	if sawW != 20 {
		t.Errorf("the child was measured with MaxW=%d, want the parent's 20; a request "+
			"of 50 reached it untouched and it was then placed inside a 20-cell "+
			"wrapper", sawW)
	}
	if got := sizeOn(t, h, r); got.W != 20 {
		t.Errorf("Size() = %+v, want the parent's 20", got)
	}
}

// looseCeiling gives its child a LOOSE box: a maximum, and no minimum. That is
// the shape that separates the request-side clamp from the returned-size one —
// under a tight parent the final Constrain would hide the difference.
type looseCeiling struct {
	widget.Base
	child tui.Component
	w, hh int
}

func (l *looseCeiling) Init(ctx *tui.Context) {
	l.Base.Init(ctx)
	if l.child != nil {
		ctx.Mount(l.child)
	}
}

// attach mounts a child after the container is running, so a test can subscribe
// to the bus BEFORE the child's first layout — the only way to observe what that
// first layout does or does not publish.
func (l *looseCeiling) attach(c tui.Component) {
	l.child = c
	l.Context().Mount(c)
	l.Context().RequestLayout()
}

func (l *looseCeiling) Layout(cs tui.Constraints) tui.Size {
	ctx := l.Context()
	if l.child != nil {
		got := ctx.LayoutChild(l.child, tui.Constraints{MaxW: l.w, MaxH: l.hh})
		ctx.PlaceChild(l.child, tui.Rect{X: 0, Y: 0, W: got.W, H: got.H})
	}
	return cs.Constrain(tui.Size{W: l.w, H: l.hh})
}

func (l *looseCeiling) Render(tui.Surface) {}

// TestTheWrapperNeverReturnsASizeItsParentForbids.
//
// The case the other clamp cannot reach, and the defect the Float-composition
// test above uncovered: a
// parent whose MINIMUM exceeds the wrapper's configured MAXIMUM. The child must
// not be forced past its cap, so it is measured at 30 — and the wrapper must
// still return the 40 its parent demanded, carrying the slack. Returning 30 is a
// constraint violation, which the runtime reports but only after the illegal
// size has already been handed up.
func TestTheWrapperNeverReturnsASizeItsParentForbids(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 8}))
	// TIGHT at 40x12 — larger than the wrapper's cap on both axes.
	host := widget.NewOverlayHost(&fixedBox{child: r, w: 40, hh: 12})
	h := startApp(t, host, 60, 20)
	defer h.stop()
	h.settle()

	if got := sizeOn(t, h, r); got != (tui.Size{W: 40, H: 12}) {
		t.Errorf("Size() = %+v, want the parent's tight 40x12; the wrapper may not "+
			"return a size its parent forbids, whatever its own maximum says", got)
	}
	// The child is still capped: the maximum is not silently raised to fit.
	var sawW int
	h.onLoop(func() { sawW = child.sawMaxW })
	if sawW != 30 {
		t.Errorf("the child was measured with MaxW=%d, want the configured cap of 30", sawW)
	}
}
