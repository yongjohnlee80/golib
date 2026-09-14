package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Capture exists for one situation that ordinary routing cannot serve: the
// pointer has left the widget that owns the drag. Every hit-test-based
// mechanism answers "what is under the pointer?" with the wrong widget from
// that moment on, so the tests below are written around a pointer that has
// travelled OFF the owner. A capture test that never leaves the owner's rect
// proves nothing, because plain hit-testing would pass it too.
//
// These use bare components rather than any widget, so they pin the runtime
// rule and not one component's behaviour.

// dragger takes the pointer on a left press and records what it is told
// afterwards. Counters are atomic because HandleEvent runs on the loop
// goroutine while the test goroutine reads them.
type dragger struct {
	MultiChild
	size Size

	grab    atomic.Bool // capture on the next press
	granted atomic.Int64
	refused atomic.Int64

	presses  atomic.Int64
	motions  atomic.Int64
	releases atomic.Int64
	wheels   atomic.Int64
	lastX    atomic.Int64
	lastY    atomic.Int64

	losses      atomic.Int64
	lastReason  atomic.Int64
	heldAtLoss  atomic.Bool // HasPointerCapture() as seen INSIDE the loss handler
	ownerAtLoss atomic.Uint64
}

func (d *dragger) Layout(cs Constraints) Size {
	if d.ctx != nil {
		x := 0
		for _, ch := range d.All() {
			sz := d.ctx.LayoutChild(ch, Loose(Size{W: cs.MaxW, H: cs.MaxH}))
			d.ctx.PlaceChild(ch, Rect{X: x, Y: 0, W: sz.W, H: sz.H})
			x += sz.W
		}
	}
	return cs.Constrain(d.size)
}
func (d *dragger) Render(Surface)     {}
func (d *dragger) AcceptsFocus() bool { return true }

func (d *dragger) HandleEvent(ev Event) bool {
	switch e := ev.(type) {
	case MouseEvent:
		d.lastX.Store(int64(e.X))
		d.lastY.Store(int64(e.Y))
		switch e.Kind {
		case MousePress:
			d.presses.Add(1)
			if d.grab.Load() && d.ctx != nil {
				if d.ctx.CapturePointer() {
					d.granted.Add(1)
				} else {
					d.refused.Add(1)
				}
			}
		case MouseMotion:
			d.motions.Add(1)
		case MouseRelease:
			d.releases.Add(1)
		case MouseWheel:
			d.wheels.Add(1)
		}
	case PointerCaptureLostEvent:
		d.losses.Add(1)
		d.lastReason.Store(int64(e.Reason))
		d.ownerAtLoss.Store(uint64(e.Owner))
		if d.ctx != nil {
			d.heldAtLoss.Store(d.ctx.HasPointerCapture())
		}
	}
	return false
}

// startDrag presses on the owner so it captures, and returns once the runtime
// has recorded the capture. Every test below needs this exact precondition, and
// asserting it here means a later failure means what it says instead of quietly
// meaning "the drag never started".
func startDrag(t *testing.T, h *harness, d *dragger, x, y int) {
	t.Helper()
	d.grab.Store(true)
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: x, Y: y})
	waitFor(t, "owner captured the pointer", func() bool { return d.granted.Load() == 1 })
	h.sync()
	var held bool
	h.onLoop(func() { held = h.app.captureOwner != 0 })
	if !held {
		t.Fatal("precondition failed: no capture is held, so nothing below is a capture test")
	}
}

// TestCaptureDeliversOffWidgetMotionAndRelease is the whole point of the
// feature. The pointer leaves the owner entirely; without capture the motion
// and release hit-test to the sibling and the drag is stranded unfinished.
func TestCaptureDeliversOffWidgetMotionAndRelease(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	other := &counter{size: Size{W: 10, H: 4}}
	root.Add(owner, other)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	// Baseline AFTER the drag has started. The press that begins a gesture is
	// an ordinary uncaptured event and bubbles to the parent exactly as it
	// should; only what follows the capture is under test here. Asserting the
	// parent's absolute total instead would fail on correct code.
	_, _, rootBefore := root.totals()

	// X=15 is squarely inside the SIBLING, not the owner.
	h.inject(MouseEvent{Kind: MouseMotion, X: 15, Y: 1})
	h.inject(MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 15, Y: 1})
	waitFor(t, "owner received the off-widget release", func() bool {
		return owner.releases.Load() == 1
	})
	h.sync()

	if got := owner.motions.Load(); got != 1 {
		t.Errorf("owner received %d off-widget motions, want 1: capture must keep "+
			"delivering once the pointer leaves the owner", got)
	}
	// The sibling under the pointer must NOT have been given the drag. This is
	// the half that fails if capture routes in addition to, rather than
	// instead of, the hit test.
	if _, _, m := other.totals(); m != 0 {
		t.Errorf("the sibling under the pointer received %d mouse events, want 0: "+
			"a captured event is addressed to the owner and not hit-tested", m)
	}
	// Nor does a captured event bubble. root is the owner's PARENT, so it is
	// exactly where a stray bubble would surface, and an ancestor cannot tell
	// one child's drag from another's.
	if _, _, rootAfter := root.totals(); rootAfter != rootBefore {
		t.Errorf("the owner's parent received %d further mouse events once the capture "+
			"was held, want 0: a captured event is addressed to the owner and must "+
			"not bubble", rootAfter-rootBefore)
	}
}

// TestCaptureIsRefusedOutsideTheActiveTrap. Capture would otherwise be a way to
// reach around a modal: a widget behind the dialog could take the pointer and
// keep receiving it, which is the precise thing the trap exists to prevent.
func TestCaptureIsRefusedOutsideTheActiveTrap(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	outside := &dragger{size: Size{W: 10, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	inTrap := &focusableCounter{counter{size: Size{W: 4, H: 1}}}
	trap.Add(inTrap)
	root.Add(outside, trap)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Open the trap.
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[inTrap]) })
	h.sync()
	h.onLoop(func() {
		if h.app.confinement() == nil {
			t.Fatal("precondition failed: no active trap, so a refusal proves nothing")
		}
	})

	var got bool
	h.onLoop(func() {
		n := h.app.byComp[outside]
		prev := h.app.inHandler
		h.app.inHandler = true
		got = n.ctx.CapturePointer()
		h.app.inHandler = prev
	})
	if got {
		t.Error("a node outside the active trap was granted the capture; that is a " +
			"route around the modal the trap exists to prevent")
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture is held by %d after a refused acquisition, want nobody", held)
	}
}

// TestCapturedCoordinatesAreOwnerLocalAndUnclamped pins the coordinate rule.
// Clamping to the owner's rect would report the pointer parked on the border
// while the user is dragging well past it, which is exactly the information a
// resize or drag gesture needs.
func TestCapturedCoordinatesAreOwnerLocalAndUnclamped(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	spacer := &counter{size: Size{W: 6, H: 4}}
	owner := &dragger{size: Size{W: 6, H: 4}}
	root.Add(spacer, owner) // owner occupies x=6..11

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 7, 1)

	// Far to the LEFT of the owner: owner-local X must go negative.
	h.inject(MouseEvent{Kind: MouseMotion, X: 1, Y: 1})
	waitFor(t, "off-widget motion arrived", func() bool { return owner.motions.Load() == 1 })
	h.sync()
	if got := owner.lastX.Load(); got != 1-6 {
		t.Errorf("owner-local X = %d, want %d: coordinates are owner-local and "+
			"must NOT be clamped into the owner's rect", got, 1-6)
	}

	// Far to the RIGHT, past the owner's width.
	h.inject(MouseEvent{Kind: MouseMotion, X: 19, Y: 1})
	waitFor(t, "second off-widget motion arrived", func() bool { return owner.motions.Load() == 2 })
	h.sync()
	if got := owner.lastX.Load(); got != 19-6 {
		t.Errorf("owner-local X = %d, want %d: past-the-edge coordinates are legal", got, 19-6)
	}
}

// TestWheelIsNotCaptured guards a decision that is easy to reverse by accident
// while making "all pointer events go to the owner" true.
func TestWheelIsNotCaptured(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	other := &counter{size: Size{W: 10, H: 4}}
	root.Add(owner, other)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	// Wheel over the SIBLING while the owner holds the capture.
	h.inject(MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 15, Y: 1})
	waitFor(t, "wheel reached the pane under the pointer", func() bool {
		_, _, m := other.totals()
		return m == 1
	})
	h.sync()

	if got := owner.wheels.Load(); got != 0 {
		t.Errorf("capture owner received %d wheel events, want 0: scrolling addresses "+
			"the pane under the pointer even during a drag", got)
	}
}

// TestCaptureIsNeverStolen: the widget holding the drag is the one that can
// finish it, so a second acquirer is refused rather than served.
func TestCaptureIsNeverStolen(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	thief := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner, thief)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)
	ownerID := uint64(0)
	h.onLoop(func() { ownerID = uint64(h.app.captureOwner) })

	// The thief tries to capture from its own press. Capture routing sends the
	// press to the OWNER, so drive the thief's attempt directly on the loop
	// through its own Context — otherwise this test would silently be asserting
	// that the thief never ran at all.
	var got bool
	h.onLoop(func() {
		n := h.app.byComp[thief]
		prev := h.app.inHandler
		h.app.inHandler = true
		got = n.ctx.CapturePointer()
		h.app.inHandler = prev
	})
	if got {
		t.Error("a second node was granted the capture; capture must never be stolen")
	}
	var after uint64
	h.onLoop(func() { after = uint64(h.app.captureOwner) })
	if after != ownerID {
		t.Errorf("capture owner changed from %d to %d on a refused acquisition; "+
			"a refusal must change nothing", ownerID, after)
	}
}

// TestCaptureIsIdempotentForTheSameNode: re-acquiring is a true no-op, so a
// widget that captures on every press of a drag does not have to track whether
// it already holds one.
func TestCaptureIsIdempotentForTheSameNode(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 3, Y: 1})
	waitFor(t, "second press handled", func() bool { return owner.presses.Load() == 2 })
	h.sync()

	if got := owner.refused.Load(); got != 0 {
		t.Errorf("re-acquiring by the SAME node was refused %d time(s); it must be "+
			"idempotent", got)
	}
	if got := owner.granted.Load(); got != 2 {
		t.Errorf("granted=%d, want 2: re-acquisition returns true", got)
	}
	if got := owner.losses.Load(); got != 0 {
		t.Errorf("re-acquiring emitted %d loss event(s); it must emit none", got)
	}
}

// TestReleasePointerEmitsNothing: the caller already knows it released, and a
// loss event would make widgets filter their own deliberate releases out.
func TestReleasePointerEmitsNothing(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)
	h.onLoop(func() { h.app.byComp[owner].ctx.ReleasePointer() })
	h.sync()

	if got := owner.losses.Load(); got != 0 {
		t.Errorf("an explicit release delivered %d loss event(s), want 0", got)
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture still held by %d after ReleasePointer", held)
	}
	// And routing really has gone back to hit-testing.
	h.inject(MouseEvent{Kind: MouseMotion, X: 2, Y: 1})
	waitFor(t, "post-release motion", func() bool { return owner.motions.Load() == 1 })
}

// lossCase drives one involuntary-loss path and names the reason it must
// report. Each trigger runs with a capture already held.
type lossCase struct {
	name    string
	want    CaptureLostReason
	trigger func(t *testing.T, h *harness, owner *dragger, root *counter)
}

// TestInvoluntaryLossReportsTheRightReason walks every way a capture can die
// without the owner releasing it. The REASON is asserted, not merely that a
// loss happened: the reason is what a widget branches on, and a single wrong
// constant is invisible to a test that only counts losses.
func TestInvoluntaryLossReportsTheRightReason(t *testing.T) {
	cases := []lossCase{
		{
			name: "owner unmounts",
			want: CaptureLostUnmount,
			trigger: func(t *testing.T, h *harness, owner *dragger, root *counter) {
				h.onLoop(func() { root.Remove(owner) })
			},
		},
		{
			name: "terminal window loses focus",
			want: CaptureLostBackend,
			trigger: func(t *testing.T, h *harness, owner *dragger, root *counter) {
				h.inject(FocusEvent{Gained: false})
			},
		},
		{
			name: "owner cancels",
			want: CaptureLostCancelled,
			trigger: func(t *testing.T, h *harness, owner *dragger, root *counter) {
				h.onLoop(func() { h.app.byComp[owner].ctx.CancelGesture() })
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := &counter{size: Size{W: 20, H: 4}}
			owner := &dragger{size: Size{W: 10, H: 4}}
			root.Add(owner)

			h := startApp(t, root, 20, 4)
			defer h.wait()
			h.sync()

			startDrag(t, h, owner, 2, 1)
			tc.trigger(t, h, owner, root)
			waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
			h.sync()

			if got := CaptureLostReason(owner.lastReason.Load()); got != tc.want {
				t.Errorf("loss reason = %v, want %v", got, tc.want)
			}
			var held NodeID
			h.onLoop(func() { held = h.app.captureOwner })
			if held != 0 {
				t.Errorf("capture still held by %d after an involuntary loss", held)
			}
		})
	}
}

// TestCaptureIsClearedBeforeTheLossIsDelivered is what makes loss exactly-once
// rather than usually-once. A handler that reacts by releasing or cancelling
// must find nothing left to release, or the loss path can re-enter itself.
func TestCaptureIsClearedBeforeTheLossIsDelivered(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)
	h.inject(FocusEvent{Gained: false})
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if owner.heldAtLoss.Load() {
		t.Error("HasPointerCapture() was true inside the loss handler; capture must be " +
			"cleared BEFORE delivery so the handler cannot re-enter the loss path")
	}
	var ownerID NodeID
	h.onLoop(func() { ownerID = h.app.byComp[owner].id })
	if got := NodeID(owner.ownerAtLoss.Load()); got != ownerID {
		t.Errorf("loss event named owner %d, want %d", got, ownerID)
	}
}

// TestCancelGestureInsideTheLossHandlerIsANoOp: cancelling from within the
// notification is the obvious cleanup reflex, and it must not recurse or
// double-deliver.
func TestCancelGestureInsideTheLossHandlerIsANoOp(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &cancelOnLoss{dragger: dragger{size: Size{W: 10, H: 4}}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, &owner.dragger, 2, 1)
	h.inject(FocusEvent{Gained: false})
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := owner.losses.Load(); got != 1 {
		t.Errorf("loss delivered %d times, want exactly 1: cancelling from inside the "+
			"loss handler must not re-enter", got)
	}
}

// cancelOnLoss cancels its own gesture from inside the loss notification.
type cancelOnLoss struct{ dragger }

func (c *cancelOnLoss) HandleEvent(ev Event) bool {
	consumed := c.dragger.HandleEvent(ev)
	if _, ok := ev.(PointerCaptureLostEvent); ok && c.ctx != nil {
		c.ctx.CancelGesture() // must be a no-op, not a second loss
	}
	return consumed
}

// TestCapturePointerOutsideAHandlerPanics. Capture with no gesture in hand is
// meaningless, and one taken during Init or Layout would outlive its cause.
func TestCapturePointerOutsideAHandlerPanics(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var panicked bool
	h.onLoop(func() {
		defer func() { panicked = recover() != nil }()
		h.app.byComp[owner].ctx.CapturePointer()
	})
	h.sync()

	if !panicked {
		t.Error("CapturePointer outside a handler did not panic; it is legal only " +
			"while the component's HandleEvent is running")
	}
	// The positive control: the SAME call inside a handler is granted. Without
	// this the test above would pass against a CapturePointer that always
	// panicked.
	startDrag(t, h, owner, 2, 1)
}

// TestFocusLeavingTheOwnersSubtreeEndsTheCapture is the positive half of the
// focus rule; the negative half is the test below it, and neither is meaningful
// alone — together they pin the boundary at the owner's subtree rather than at
// the owner itself.
func TestFocusLeavingTheOwnersSubtreeEndsTheCapture(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	elsewhere := &focusableCounter{counter{size: Size{W: 10, H: 4}}}
	root.Add(owner, elsewhere)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	h.onLoop(func() { h.app.requestFocus(h.app.byComp[elsewhere]) })
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := CaptureLostReason(owner.lastReason.Load()); got != CaptureLostFocusChange {
		t.Errorf("loss reason = %v, want %v", got, CaptureLostFocusChange)
	}
}

// TestATrapOpeningOverTheOwnerEndsTheCapture: a dialog appearing must not leave
// a drag running behind it. The check happens when the scope opens rather than
// on the next pointer event, because with no further mouse input the owner
// would otherwise stay in its dragging state indefinitely.
func TestATrapOpeningOverTheOwnerEndsTheCapture(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	trap := &trapScope{size: Size{W: 10, H: 4}}
	inTrap := &focusableCounter{counter{size: Size{W: 4, H: 1}}}
	trap.Add(inTrap)
	root.Add(owner, trap)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	// Focus into the trap the way opening a dialog does: this pushes the scope.
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[inTrap]) })
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := CaptureLostReason(owner.lastReason.Load()); got != CaptureLostFocusScope {
		t.Errorf("loss reason = %v, want %v: an owner excluded by a newly active trap "+
			"loses the pointer to the trap, not to the focus move", got, CaptureLostFocusScope)
	}
}

// hider lays its child out only while shown, so a child can be made invisible
// the way a real container hides one — by ceasing to place it.
type hider struct {
	MultiChild
	size Size
	show atomic.Bool
}

func (hd *hider) Layout(cs Constraints) Size {
	if hd.ctx != nil && hd.show.Load() {
		for _, ch := range hd.All() {
			sz := hd.ctx.LayoutChild(ch, Loose(Size{W: cs.MaxW, H: cs.MaxH}))
			hd.ctx.PlaceChild(ch, Rect{W: sz.W, H: sz.H})
		}
	}
	return cs.Constrain(hd.size)
}
func (hd *hider) Render(Surface)            {}
func (hd *hider) HandleEvent(ev Event) bool { return false }

// TestHidingTheOwnerEndsTheCapture. There is nothing left on screen for the
// user to be dragging, and the owner would otherwise keep receiving motion for
// a widget that is not displayed.
func TestHidingTheOwnerEndsTheCapture(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	box := &hider{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	box.Add(owner)
	box.show.Store(true)
	root.Add(box)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	// Hide, and force the layout pass that makes it true.
	h.onLoop(func() {
		box.show.Store(false)
		h.app.layoutDirty = true
		h.app.renderDirty = true
		h.app.renderFrame()
	})
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := CaptureLostReason(owner.lastReason.Load()); got != CaptureLostHidden {
		t.Errorf("loss reason = %v, want %v", got, CaptureLostHidden)
	}
}

// captureTrace collects the TraceCapture records the runtime emits, so a test
// can assert on the trace seam the same way an operator reads it.
type captureTrace struct {
	mu      sync.Mutex
	details []string
}

func (ct *captureTrace) opt() AppOption {
	return WithTrace(func(ev TraceEvent) {
		if ev.Kind != TraceCapture {
			return
		}
		ct.mu.Lock()
		ct.details = append(ct.details, ev.Detail)
		ct.mu.Unlock()
	})
}

func (ct *captureTrace) all() []string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return append([]string(nil), ct.details...)
}

func (ct *captureTrace) has(substr string) bool {
	for _, d := range ct.all() {
		if strings.Contains(d, substr) {
			return true
		}
	}
	return false
}

// TestCaptureIsVisibleOnTheTraceSeam. A dropped drag and a misbehaving modal
// present identically to a user, so the trace has to say which happened —
// including the REFUSALS, whose only other evidence is a widget doing nothing.
func TestCaptureIsVisibleOnTheTraceSeam(t *testing.T) {
	ct := &captureTrace{}
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	thief := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner, thief)

	h := startApp(t, root, 20, 4, ct.opt())
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)
	if !ct.has("pointer captured") {
		t.Errorf("no acquisition record on the trace; got %q", ct.all())
	}

	// A refused steal must leave a record naming why.
	h.onLoop(func() {
		n := h.app.byComp[thief]
		prev := h.app.inHandler
		h.app.inHandler = true
		n.ctx.CapturePointer()
		h.app.inHandler = prev
	})
	h.sync()
	if !ct.has("capture refused: already held") {
		t.Errorf("a refused acquisition left no trace record; got %q", ct.all())
	}

	// And the loss records its reason, not merely that a loss occurred.
	h.inject(FocusEvent{Gained: false})
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()
	if !ct.has("pointer capture lost: backend") {
		t.Errorf("the loss record does not name its reason; got %q", ct.all())
	}
}

// TestTraceCaptureIsItsOwnKind. Capture records must not be filed as scope
// records: an operator filtering for focus-trap problems would otherwise get
// every drag, and one filtering for drags would get every modal.
func TestTraceCaptureIsItsOwnKind(t *testing.T) {
	var scopeDetails []string
	var mu sync.Mutex
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4, WithTrace(func(ev TraceEvent) {
		if ev.Kind == TraceScope {
			mu.Lock()
			scopeDetails = append(scopeDetails, ev.Detail)
			mu.Unlock()
		}
	}))
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)
	h.onLoop(func() { h.app.byComp[owner].ctx.CancelGesture() })
	h.sync()

	mu.Lock()
	got := append([]string(nil), scopeDetails...)
	mu.Unlock()
	for _, d := range got {
		if strings.Contains(d, "pointer") {
			t.Errorf("a pointer-capture record was filed under TraceScope: %q", d)
		}
	}
	if TraceCapture.String() != "capture" {
		t.Errorf("TraceCapture.String() = %q, want %q", TraceCapture.String(), "capture")
	}
}

// layoutReleaser calls ReleasePointer from inside Layout, which is illegal:
// Layout is walking the tree, and mutating dispatch state mid-walk is the
// thing the phase guard exists to stop.
type layoutReleaser struct {
	MultiChild
	size  Size
	arm   atomic.Bool
	fired atomic.Bool
}

func (lr *layoutReleaser) Layout(cs Constraints) Size {
	if lr.arm.Load() && lr.ctx != nil {
		func() {
			defer func() {
				if recover() != nil {
					lr.fired.Store(true)
				}
			}()
			lr.ctx.ReleasePointer()
		}()
	}
	return cs.Constrain(lr.size)
}
func (lr *layoutReleaser) Render(Surface)            {}
func (lr *layoutReleaser) HandleEvent(ev Event) bool { return false }

// TestReleasePointerInsideLayoutPanics gives the phase guard a witness. Without
// this the guard is registered in the panic budget as deliberate behaviour that
// nothing proves ever happens.
func TestReleasePointerInsideLayoutPanics(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	lr := &layoutReleaser{size: Size{W: 10, H: 4}}
	root.Add(lr)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	if lr.fired.Load() {
		t.Fatal("precondition failed: the guard fired before it was armed")
	}
	h.onLoop(func() {
		lr.arm.Store(true)
		h.app.layoutDirty = true
		h.app.renderDirty = true
		h.app.renderFrame()
	})
	h.sync()

	if !lr.fired.Load() {
		t.Error("ReleasePointer inside Layout did not panic; mutating dispatch state " +
			"mid-layout must be refused at the moment the rule is broken")
	}
}

// TestCaptureLostReasonNamesEveryValue. The reason is what a widget branches on
// and what a trace line shows, so an unnamed one turns a diagnosis into a
// guess. The default arm is included: a reason added later without a name here
// would otherwise silently print as a bare number.
func TestCaptureLostReasonNamesEveryValue(t *testing.T) {
	want := map[CaptureLostReason]string{
		CaptureLostUnmount:     "unmount",
		CaptureLostHidden:      "hidden",
		CaptureLostFocusChange: "focus-change",
		CaptureLostFocusScope:  "focus-scope",
		CaptureLostCancelled:   "cancelled",
		CaptureLostBackend:     "backend",
		CaptureLostShutdown:    "shutdown",
	}
	for r, s := range want {
		if got := r.String(); got != s {
			t.Errorf("CaptureLostReason(%d).String() = %q, want %q", r, got, s)
		}
	}
	// One past the last defined reason: unnamed values must say so rather than
	// render as an empty string that reads like "no reason".
	if got := CaptureLostReason(200).String(); got != "unknown" {
		t.Errorf("an undefined reason rendered as %q, want %q", got, "unknown")
	}
}

// TestFocusMovingWithinTheOwnersSubtreeKeepsTheCapture is the negative half of
// the focus rule. A drag begun on a container survives focus moving between its
// own descendants — which the drag itself routinely causes.
func TestFocusMovingWithinTheOwnersSubtreeKeepsTheCapture(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 20, H: 4}}
	inner := &focusableCounter{counter{size: Size{W: 4, H: 1}}}
	owner.Add(inner)
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	startDrag(t, h, owner, 2, 1)

	// Move focus to a DESCENDANT of the owner.
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[inner]) })
	h.sync()

	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held == 0 {
		t.Fatal("capture was lost when focus moved WITHIN the owner's subtree; only " +
			"focus leaving the subtree ends a capture")
	}
	if got := owner.losses.Load(); got != 0 {
		t.Errorf("delivered %d loss event(s) for an in-subtree focus move, want 0", got)
	}
}
