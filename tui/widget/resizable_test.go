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
	passes  atomic.Int64
}

// layouts reports how many times this child has been measured, read the
// sanctioned way. One change must cost one measurement.
//
// ATOMIC, unlike the sawMin/sawMax fields beside it, because this one is polled
// from the test goroutine while the loop goroutine is still laying out: a
// waitFor condition has no synchronisation with the loop, where an onLoop or
// settle round-trip supplies one. A plain int here is a data race the race
// detector catches only on the runs where the poll and a pass overlap.
func (c *sizedChild) layouts() int { return int(c.passes.Load()) }

// AcceptsFocus makes this a tab stop, which the wrapper is deliberately NOT.
// Resize keys reach a Resizable by BUBBLING from a focused descendant, so a
// fixture with nothing focusable inside it cannot drive the keyboard path at
// all — and a test that focused the wrapper would be testing an arrangement the
// widget no longer has.
func (c *sizedChild) AcceptsFocus() bool { return true }

func (c *sizedChild) Layout(cs tui.Constraints) tui.Size {
	c.sawMinW, c.sawMaxW = cs.MinW, cs.MaxW
	c.passes.Add(1)
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

	// A request that changes no cells publishes nothing. This one is now caught
	// EARLY, in SetSize, which returns before requesting layout at all.
	h.onLoop(func() { r.SetSize(tui.Size{W: 12, H: 6}) })
	h.settle()
	h.settle()
	h.onLoop(func() {
		if len(events) != 1 {
			t.Errorf("%d events after a no-op resize, want still 1", len(events))
		}
	})

	// AND A LAYOUT PASS THAT NOBODY ASKED FOR. Making SetSize a no-op closed the
	// path above before it reaches the commit phase, so on its own it would leave
	// commit's own "nothing changed" guard with nothing observing it. This is the
	// case that still reaches it, and the one a user actually hits: the terminal
	// is resized, every widget lays out again, and a box that is explicitly 12x6
	// inside a ceiling that still fits it comes out 12x6. A listener counting
	// resizes must not see one, or every window-manager drag reports the box as
	// resized when it did not move a cell.
	h.tb.InjectResize(50, 24)
	h.settle()
	h.settle()
	h.onLoop(func() {
		if len(events) != 1 {
			t.Errorf("%d events after an ambient re-layout that left the size at "+
				"12x6, want still 1", len(events))
		}
	})
	// The control: the same ambient path DOES publish when the size really moves,
	// so the zero above is a guard doing its job rather than a wrapper that has
	// stopped publishing.
	h.onLoop(func() { r.SetSize(tui.Size{W: 14, H: 6}) })
	h.settle()
	h.settle()
	h.onLoop(func() {
		if len(events) != 2 {
			t.Errorf("%d events after a real change, want 2", len(events))
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
	h.onLoop(func() { child.Context().RequestFocus() })
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
	h.onLoop(func() { child.Context().RequestFocus() })
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
	// from each — and the two orders must be BYTE-IDENTICAL. Nothing is
	// subtracted before the comparison this time: the earlier version of this
	// test stripped the wrapper's own stop out of the wrapped order first,
	// which quietly conceded the very thing the rule forbids. Wrapping
	// arbitrary content changes traversal in no way at all.
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
		for range 7 {
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
				default:
					stops = append(stops, "OTHER")
				}
			})
		}
		return stops
	}

	bare := strings.Join(order(false), ",")
	wrapped := strings.Join(order(true), ",")
	if wrapped != bare {
		t.Errorf("Tab order differs:\n  unwrapped: %s\n  wrapped:   %s\n"+
			"wrapping arbitrary content must not change traversal — neither the "+
			"grips nor the wrapper may be a stop", bare, wrapped)
	}
	if strings.Contains(wrapped, "OTHER") {
		t.Errorf("focus landed on neither control: %s", wrapped)
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
			widget.NewResizable(child, widget.WithHandles(widget.HandleHorizontalDivider+1))
		}},
		{"a divider handle, which belongs to Split", func() {
			widget.NewResizable(child, widget.WithHandles(widget.HandleVerticalDivider))
		}},
		{"an empty grip glyph", func() {
			widget.NewResizable(child, widget.WithHandleGlyph(""))
		}},
		{"a multi-grapheme grip glyph", func() {
			widget.NewResizable(child, widget.WithHandleGlyph("ab"))
		}},
		{"a negative minimum width", func() {
			widget.NewResizable(child, widget.WithMinSize(tui.Size{W: -1, H: 4}))
		}},
		{"a negative minimum height", func() {
			widget.NewResizable(child, widget.WithMinSize(tui.Size{W: 4, H: -1}))
		}},
		{"Unbounded as a minimum width", func() {
			// Unbounded is legal for a MAXIMUM only: as a minimum it asks for a
			// box no terminal can satisfy, and normalising it away later hides
			// the author's mistake instead of reporting it.
			widget.NewResizable(child, widget.WithMinSize(tui.Size{W: tui.Unbounded, H: 4}))
		}},
		{"Unbounded as a minimum height", func() {
			widget.NewResizable(child, widget.WithMinSize(tui.Size{W: 4, H: tui.Unbounded}))
		}},
		{"a negative maximum", func() {
			widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: -1, H: 10}))
		}},
		{"a zero-width grip glyph", func() {
			// One cluster, no cells. It would be placed, hit-tested and
			// invisible — the same failure as a wide glyph in a narrow rect,
			// reached from the other end.
			widget.NewResizable(child, widget.WithHandleGlyph("\u200b"))
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
			widget.WithHandles(widget.HandleBottomRight),
			widget.WithHandleGlyph("世"), // wide, but declared and measured
			// The boundary values of the minimum/maximum domain: zero is a
			// legal minimum, and Unbounded is legal as a maximum.
			widget.WithMinSize(tui.Size{W: 0, H: 0}),
			widget.WithMaxSize(tui.Size{W: tui.Unbounded, H: 40}),
			widget.WithHandlePlacement(widget.PlacementReserve),
			widget.WithResizeStep(1, widget.StepPercent))
	}); f != nil {
		t.Errorf("a legal configuration was rejected: %v", f.Rule)
	}
}

// TestAGripIsDroppedWhenITSOWNRectangleWillNotFit.
//
// The rule is about the grip's ACTUAL requirements, not a blanket "the box is
// tiny". A two-column glyph needs two columns: dropping only at one-cell boxes
// let a wide grip be placed in a one-column rect, where it rendered nothing —
// an affordance that is mounted, hit-testable and INVISIBLE, which is strictly
// worse than no affordance at all.
func TestAGripIsDroppedWhenITSOWNRectangleWillNotFit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		glyph    string
		width    int
		wantGrip bool
	}{
		// One column available. A one-cell glyph fits; a two-cell one does not.
		{"a narrow glyph in one column", "◢", 1, true},
		{"a wide glyph in one column", "世", 1, false},
		// Two columns: both fit.
		{"a wide glyph in two columns", "世", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 4, H: 4}}
			r := widget.NewResizable(child,
				widget.WithHandleGlyph(tc.glyph),
				widget.WithHandlePlacement(widget.PlacementOverlay))
			host := widget.NewOverlayHost(&fixedBox{child: r, w: tc.width, hh: 2})
			h := startApp(t, host, 20, 6)
			defer h.stop()
			h.settle()

			got := strings.Contains(h.grid(), tc.glyph)
			if got != tc.wantGrip {
				t.Errorf("grip painted = %v, want %v — a grip is placed only when the "+
					"rectangle it needs actually fits:\n%s", got, tc.wantGrip, h.grid())
			}
		})
	}
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
			wrapped := tc.build()
			r := widget.NewResizable(wrapped,
				widget.WithInitialSize(tui.Size{W: 10, H: 4}),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(r)
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.settle()

			if got := sizeOn(t, h, r); got != (tui.Size{W: 10, H: 4}) {
				t.Fatalf("Size() = %+v, want the requested 10x4", got)
			}
			// And it RESIZES, with no cooperation from the child whatsoever.
			//
			// Driven through the wrapper's ACTION rather than a keystroke,
			// because the keystroke path depends on the child: an Editor and a
			// TextArea consume Shift-arrows for selection, so nothing bubbles
			// up to the wrapper. That is a real property of wrapping a text
			// widget and it is recorded in the doc comment; what this row
			// claims is that the widget became resizable by being wrapped,
			// which the action and the grips both demonstrate for every child.
			h.onLoop(func() {
				r.Context().DoAction(widget.ResizeStepAction{DX: 1, Unit: widget.StepCells})
			})
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
			h.onLoop(func() { child.Context().RequestFocus() })
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

// TestAnInvalidGestureIsRefusedWithoutMutation.
//
// An action is PUBLIC INPUT — a consumer's resolver, a key binding, a DoAction
// from application code — so a malformed one is an ordinary occurrence rather
// than a programmer error worth a panic. What matters is that a refusal leaves
// NOTHING behind: accepting Handle(255) and storing a live drag meant the
// gesture had a direction of zero on both axes and then swallowed every Update
// and End that followed, so a later legitimate gesture could not start.
func TestAnInvalidGestureIsRefusedWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		begin widget.ResizeBeginAction
	}{
		{"a handle outside the declared set",
			widget.ResizeBeginAction{Handle: widget.HandleHorizontalDivider + 1}},
		{"a divider, which is Split's and not a box's",
			widget.ResizeBeginAction{Handle: widget.HandleVerticalDivider}},
		{"a handle this wrapper was not configured with",
			widget.ResizeBeginAction{Handle: widget.HandleTopLeft}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
			r := widget.NewResizable(child,
				widget.WithHandles(widget.HandleBottomRight),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.settle()
			before := sizeOn(t, h, r)

			var begun, updated, ended bool
			h.onLoop(func() {
				begun = r.Context().DoAction(tc.begin)
				// The follow-ups must be inert too: a refused Begin leaves no
				// gesture, so there is nothing for them to act on.
				updated = r.Context().DoAction(widget.ResizeUpdateAction{At: tui.Point{X: 99, Y: 99}})
				ended = r.Context().DoAction(widget.ResizeEndAction{})
			})
			h.settle()

			if begun {
				t.Error("the invalid begin was handled; it must be refused")
			}
			if updated || ended {
				t.Errorf("update=%v end=%v after a refused begin; a refusal must leave "+
					"no gesture state behind", updated, ended)
			}
			if got := sizeOn(t, h, r); got != before {
				t.Errorf("Size() = %+v, want the untouched %+v", got, before)
			}
		})
	}
}

// TestAnInvalidStepIsRefused — the same rule on the other action. A step of
// nothing is not a step, and a unit outside the closed set is not a unit.
func TestAnInvalidStepIsRefused(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
	host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()
	before := sizeOn(t, h, r)

	var zero, badUnit, good bool
	h.onLoop(func() {
		zero = r.Context().DoAction(widget.ResizeStepAction{Unit: widget.StepCells})
		badUnit = r.Context().DoAction(widget.ResizeStepAction{DX: 1, Unit: widget.StepPercent + 1})
		good = r.Context().DoAction(widget.ResizeStepAction{DX: 1, Unit: widget.StepCells})
	})
	h.settle()

	if zero {
		t.Error("a step of zero on both axes was handled")
	}
	if badUnit {
		t.Error("a step with an undeclared unit was handled")
	}
	if !good {
		t.Fatal("the valid step was refused, so the refusals above prove nothing")
	}
	if got := sizeOn(t, h, r); got.W != before.W+1 {
		t.Errorf("Size() = %+v after one valid step, want one cell wider than %+v",
			got, before)
	}
}

// TestCancellingRestoresTheEXACTPriorRequest.
//
// The request, not the effective size. A wrapper asking for 50x10 inside a
// ceiling that clamps it to 12x6 must come out of a cancelled drag still asking
// for 50x10 — writing the clamp back as the request means a cancelled gesture
// silently changed what gets persisted, and the next terminal with room would
// show a box the user never resized.
func TestCancellingRestoresTheEXACTPriorRequest(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 4, H: 2}}
	r := widget.NewResizable(child,
		widget.WithHandles(widget.HandleBottomRight),
		widget.WithMaxSize(tui.Size{W: 100, H: 100}))
	// A 12x6 ceiling: the 50x10 request below cannot be reached.
	host := widget.NewOverlayHost(&looseCeiling{child: r, w: 12, hh: 6})
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	h.onLoop(func() { r.SetSize(tui.Size{W: 50, H: 10}) })
	h.settle()
	want, ok := requestedOn(t, h, r)
	if !ok || want != (tui.Size{W: 50, H: 10}) {
		t.Fatalf("RequestedSize() = (%+v, %v), want the unclamped 50x10", want, ok)
	}
	if eff := sizeOn(t, h, r); eff == want {
		t.Fatalf("Size() = %+v equals the request, so this fixture is not clamping "+
			"and the test below would prove nothing", eff)
	}

	// Drag, then cancel.
	h.onLoop(func() {
		r.Context().DoAction(widget.ResizeBeginAction{
			Handle: widget.HandleBottomRight, At: tui.Point{X: 11, Y: 5}})
		r.Context().DoAction(widget.ResizeUpdateAction{At: tui.Point{X: 4, Y: 2}})
	})
	h.settle()
	h.onLoop(func() { r.Context().DoAction(widget.ResizeCancelAction{}) })
	h.settle()

	got, ok := requestedOn(t, h, r)
	if !ok || got != want {
		t.Errorf("RequestedSize() = (%+v, %v) after cancelling, want the exact prior "+
			"request %+v; cancel wrote the clamped effective size back", got, ok, want)
	}
	if mode := modeOn(t, h, r); mode != widget.SizeExplicit {
		t.Errorf("SizeMode() = %v after cancelling an explicit drag, want explicit", mode)
	}
}

// TestReserveTakesCellsFromTheSIDEItsHandleIsOn.
//
// Reserve means the child does not share cells with a grip. A single
// width/height bit could not say WHICH side the cells came off, so the child was
// always placed at (0,0) and a reserved TOP-LEFT grip sat on the child's first
// cell — reserve behaving exactly like overlay, which is the one thing it exists
// not to do. Opposing handles must reserve both sides.
func TestReserveTakesCellsFromTheSIDEItsHandleIsOn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handles []widget.Handle
	}{
		{"top-left", []widget.Handle{widget.HandleTopLeft}},
		{"bottom-right", []widget.Handle{widget.HandleBottomRight}},
		{"left and right", []widget.Handle{widget.HandleLeft, widget.HandleRight}},
		{"top and bottom", []widget.Handle{widget.HandleTop, widget.HandleBottom}},
		{"all four corners", []widget.Handle{widget.HandleTopLeft, widget.HandleTopRight,
			widget.HandleBottomLeft, widget.HandleBottomRight}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 6, H: 4}}
			r := widget.NewResizable(child,
				widget.WithHandles(tc.handles...),
				widget.WithHandlePlacement(widget.PlacementReserve),
				widget.WithHandleGlyph("#"),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.settle()

			// The child keeps EVERY cell it was measured for, and a grip
			// shares none of them. The child fills with "·", so counting those
			// cells counts exactly the area reserve left it — if a grip were
			// sitting on the child, one of those cells would be the glyph
			// instead and the count would come up short.
			sz := sizeOn(t, h, r)
			content := 0
			for y := range sz.H {
				for _, ch := range h.row(y) {
					if ch == '·' {
						content++
					}
				}
			}
			wantW, wantH := childExtent(tc.handles, sz)
			if content != wantW*wantH {
				t.Errorf("%d child cells painted, want %d (%dx%d) inside a %dx%d "+
					"wrapper; a reserved grip is sharing cells with the child\n%s",
					content, wantW*wantH, wantW, wantH, sz.W, sz.H, h.grid())
			}
		})
	}
}

// childExtent is how much of the wrapper the child keeps once each configured
// handle has taken its band.
func childExtent(handles []widget.Handle, eff tui.Size) (w, h int) {
	var left, right, top, bottom int
	for _, hh := range handles {
		switch hh {
		case widget.HandleLeft, widget.HandleTopLeft, widget.HandleBottomLeft:
			left = 1
		case widget.HandleRight, widget.HandleTopRight, widget.HandleBottomRight:
			right = 1
		}
		switch hh {
		case widget.HandleTop, widget.HandleTopLeft, widget.HandleTopRight:
			top = 1
		case widget.HandleBottom, widget.HandleBottomLeft, widget.HandleBottomRight:
			bottom = 1
		}
	}
	return eff.W - left - right, eff.H - top - bottom
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
	h.onLoop(func() { child.Context().RequestFocus() })
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
	h.onLoop(func() { child.Context().RequestFocus() })
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

// TestReserveKeepsTheOUTERContractTruthful.
//
// The configured min and max describe the WRAPPER, not the child. Under reserve
// the child is measured against those bounds LESS the bands, so the wrapper
// still owes its parent exactly what it advertised — a wrapper with a 12-cell
// minimum and a reserved edge is 12 cells wide, of which the child has 11.
//
// Subtracting the bands from the maximum only, and leaving the minimum alone,
// is the easy version of this and it is wrong in the direction nobody notices:
// the wrapper quietly grows one cell past its own minimum on every reserved
// side, and a layout built to that minimum is off by one per handle.
func TestReserveKeepsTheOUTERContractTruthful(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handles []widget.Handle
		// bands is how many cells the configured handles reserve on each axis.
		bandW, bandH int
	}{
		{"one corner", []widget.Handle{widget.HandleBottomRight}, 1, 1},
		{"both vertical edges", []widget.Handle{widget.HandleLeft, widget.HandleRight}, 2, 0},
		{"both horizontal edges", []widget.Handle{widget.HandleTop, widget.HandleBottom}, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			minSize := tui.Size{W: 12, H: 6}
			maxSize := tui.Size{W: 20, H: 10}
			child := &sizedChild{pref: tui.Size{W: 1, H: 1}} // wants far less than the min
			r := widget.NewResizable(child,
				widget.WithHandles(tc.handles...),
				widget.WithHandlePlacement(widget.PlacementReserve),
				widget.WithMinSize(minSize),
				widget.WithMaxSize(maxSize))
			host := widget.NewOverlayHost(&looseCeiling{child: r, w: 40, hh: 20})
			h := startApp(t, host, 50, 24)
			defer h.stop()
			h.settle()

			// The MINIMUM is the wrapper's, bands included.
			if got := sizeOn(t, h, r); got != minSize {
				t.Errorf("Size() = %+v with a child that wants 1x1, want the wrapper's "+
					"configured minimum %+v — the bands come out of the child's share, "+
					"not out of what the wrapper owes its parent", got, minSize)
			}
			// And the child got the minimum less the bands, exactly.
			var sawMin int
			h.onLoop(func() { sawMin = child.sawMinW })
			if sawMin != minSize.W-tc.bandW {
				t.Errorf("the child's minimum was %d, want %d (%d less the %d reserved)",
					sawMin, minSize.W-tc.bandW, minSize.W, tc.bandW)
			}

			// The MAXIMUM is the wrapper's too: ask for more than it and the
			// wrapper stops there, bands still inside.
			h.onLoop(func() { r.SetSize(tui.Size{W: 999, H: 999}) })
			h.settle()
			if got := sizeOn(t, h, r); got != maxSize {
				t.Errorf("Size() = %+v after asking for far too much, want the "+
					"configured maximum %+v", got, maxSize)
			}
		})
	}
}
