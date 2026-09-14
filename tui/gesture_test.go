package tui

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// The recogniser exists so that press-arm/release-activate is written once
// rather than once per widget. These tests are therefore mostly about the parts
// a hand-written implementation gets wrong: dragging out and back, releasing
// outside, and every way a gesture can die without a release at all.

// control is a minimal Activatable: it records activations and armed
// transitions and nothing else. Deliberately not a MultiChild — a real button
// is a leaf, and the point of the layer is that a leaf needs no more than this.
type control struct {
	size Size
	ctx  *Context

	enabled     atomic.Bool
	activations atomic.Int64
	origin      atomic.Int64

	presses  atomic.Int64
	armCalls atomic.Int64
	armTrue  atomic.Int64
	armFalse atomic.Int64
	armed    atomic.Bool

	mu    sync.Mutex
	order []string // the sequence of arm/activate callbacks, in order
}

func newControl(w, h int) *control {
	c := &control{size: Size{W: w, H: h}}
	c.enabled.Store(true)
	return c
}

func (c *control) Init(ctx *Context)          { c.ctx = ctx }
func (c *control) Layout(cs Constraints) Size { return cs.Constrain(c.size) }
func (c *control) Render(Surface)             {}
func (c *control) AcceptsFocus() bool         { return true }
func (c *control) HandleEvent(Event) bool     { return false }

func (c *control) Activate(origin ActionOrigin) bool {
	// The disabled check lives here and only here, so every producer gets the
	// same answer without each having to ask first.
	if !c.enabled.Load() {
		return false
	}
	c.activations.Add(1)
	c.origin.Store(int64(origin))
	c.note("activate")
	return true
}

func (c *control) SetArmed(v bool) {
	c.armCalls.Add(1)
	if v {
		c.armTrue.Add(1)
	} else {
		c.armFalse.Add(1)
	}
	c.armed.Store(v)
	c.note("armed:" + boolWord(v))
}

func (c *control) note(s string) {
	c.mu.Lock()
	c.order = append(c.order, s)
	c.mu.Unlock()
}

func (c *control) sequence() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.order...)
}

func (c *control) lastOrigin() ActionOrigin { return ActionOrigin(c.origin.Load()) }

// press/move/release drive one gesture in the target's own coordinates. The
// fixture below places the control at the origin, so screen and local agree.
func press(h *harness, x, y int) {
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: x, Y: y})
}
func move(h *harness, x, y int) { h.inject(MouseEvent{Kind: MouseMotion, X: x, Y: y}) }
func release(h *harness, x, y int) {
	h.inject(MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: x, Y: y})
}

// gestureFixture builds root(20x4) → control(8x2) at the top-left, so cells
// (0,0)..(7,1) are inside the control and everything else is outside it.
func gestureFixture(t *testing.T, opts ...AppOption) (*harness, *control) {
	t.Helper()
	root := &counter{size: Size{W: 20, H: 4}}
	c := newControl(8, 2)
	root.Add(c)
	h := startApp(t, root, 20, 4, opts...)
	h.sync()
	return h, c
}

// TestPressArmsAndReleaseInsideActivates is the whole gesture, end to end,
// through a component that implements nothing but Activate and SetArmed.
func TestPressArmsAndReleaseInsideActivates(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed on press", func() bool { return c.armTrue.Load() == 1 })
	if !c.armed.Load() {
		t.Error("the control is not armed after a press inside it")
	}
	if got := c.activations.Load(); got != 0 {
		t.Fatalf("activated %d time(s) on the PRESS; activation belongs to the release", got)
	}

	release(h, 2, 1)
	waitFor(t, "activated on release", func() bool { return c.activations.Load() == 1 })
	h.sync()

	if got := c.lastOrigin(); got != OriginPointer {
		t.Errorf("origin = %v, want %v", got, OriginPointer)
	}
	if c.armed.Load() {
		t.Error("the control is still armed after the gesture ended")
	}
	// Ordering: armed, then activated, then disarmed. A widget repainting on
	// each callback would otherwise flicker through a wrong intermediate state.
	want := []string{"armed:true", "activate", "armed:false"}
	got := c.sequence()
	if len(got) != len(want) {
		t.Fatalf("callback sequence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("callback[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDraggingOutDisarmsAndDraggingBackReArms. This is the behaviour users rely
// on to change their mind after pressing, and the one hand-written
// implementations most often omit — the gesture must survive leaving the
// control, not end there.
func TestDraggingOutDisarmsAndDraggingBackReArms(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	move(h, 15, 3) // well outside
	waitFor(t, "disarmed on the way out", func() bool { return c.armFalse.Load() == 1 })
	if c.armed.Load() {
		t.Error("still armed after the pointer left the control")
	}

	move(h, 2, 1) // back inside
	waitFor(t, "re-armed on the way back", func() bool { return c.armTrue.Load() == 2 })
	if !c.armed.Load() {
		t.Error("not re-armed after the pointer returned")
	}

	release(h, 2, 1)
	waitFor(t, "activated", func() bool { return c.activations.Load() == 1 })
	h.sync()
	if got := c.activations.Load(); got != 1 {
		t.Errorf("activations = %d, want exactly 1", got)
	}
}

// TestReleasingOutsideDoesNotActivate is the other half: leaving and letting go
// is how a user cancels, and it must leave the control disarmed and inert.
func TestReleasingOutsideDoesNotActivate(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })
	move(h, 15, 3)
	release(h, 15, 3)
	waitFor(t, "disarmed", func() bool { return !c.armed.Load() })
	h.sync()

	if got := c.activations.Load(); got != 0 {
		t.Errorf("activated %d time(s) on a release OUTSIDE the control, want 0", got)
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture still held by %d after the gesture ended", held)
	}
}

// TestAGestureHoldsThePointer. Motion after the press must keep reaching the
// recogniser even once the pointer is over something else entirely — without
// the capture, the sibling under the cursor would take it and the gesture could
// never be completed or cancelled.
func TestAGestureHoldsThePointer(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	c := newControl(8, 4)
	other := &counter{size: Size{W: 12, H: 4}}
	root.Add(c, other)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	var kind CaptureKind
	var held bool
	h.onLoop(func() {
		held = h.app.captureOwner == h.app.byComp[c].id
		kind = h.app.captureKind
	})
	if !held {
		t.Fatal("the gesture did not take the pointer; motion would go to whatever the " +
			"cursor is over and the gesture could never finish")
	}
	if kind != CaptureGesture {
		t.Errorf("capture kind = %v, want CaptureGesture: the runtime took this one, "+
			"not the component", kind)
	}

	// Motion squarely over the SIBLING.
	move(h, 15, 1)
	waitFor(t, "disarmed over the sibling", func() bool { return c.armFalse.Load() == 1 })
	h.sync()
	if _, _, m := other.totals(); m != 0 {
		t.Errorf("the sibling under the pointer received %d mouse events, want 0", m)
	}
}

// TestADisabledControlDoesNotActivateButStillArms. The disabled check lives in
// Activate and only there, so the gesture still runs and the control still
// shows pressed feedback — it simply does not fire.
func TestADisabledControlDoesNotActivateButStillArms(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()
	c.enabled.Store(false)

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })
	release(h, 2, 1)
	waitFor(t, "disarmed", func() bool { return c.armFalse.Load() == 1 })
	h.sync()

	if got := c.activations.Load(); got != 0 {
		t.Errorf("a disabled control activated %d time(s), want 0", got)
	}
	if c.armed.Load() {
		t.Error("a disabled control is left armed")
	}
}

// TestSetArmedIsCalledOnlyOnTransitions. Repainting on every pointer event is
// wasteful and makes SetArmed useless as a change signal; the contract is that
// arming when already armed and disarming when already disarmed are no-ops.
func TestSetArmedIsCalledOnlyOnTransitions(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	// Three moves that all stay INSIDE. The state never changes, so none of
	// them may reach SetArmed.
	move(h, 3, 1)
	move(h, 4, 1)
	move(h, 5, 0)
	// A move OUT is a real transition and gives the wait something positive to
	// observe, rather than asserting on a count that never moved.
	move(h, 15, 3)
	waitFor(t, "the single disarm", func() bool { return c.armFalse.Load() == 1 })
	h.sync()

	if got := c.armCalls.Load(); got != 2 {
		t.Errorf("SetArmed was called %d times, want 2 (one arm, one disarm): three "+
			"in-bounds moves must not re-arm an already-armed control; sequence was %v",
			got, c.sequence())
	}
}

// TestEveryCaptureLossLeavesTheControlDisarmed. A gesture can die without a
// release in several ways, and each one must leave the widget looking unpressed
// — otherwise a button stays visibly held down forever with nothing to clear it.
func TestEveryCaptureLossLeavesTheControlDisarmed(t *testing.T) {
	cases := []struct {
		name    string
		trigger func(t *testing.T, h *harness, c *control, root *counter)
	}{
		{"terminal focus lost", func(t *testing.T, h *harness, c *control, root *counter) {
			h.inject(FocusEvent{Gained: false, Terminal: true})
		}},
		{"control unmounts", func(t *testing.T, h *harness, c *control, root *counter) {
			h.onLoop(func() { root.Remove(c) })
		}},
		{"pointer input disabled", func(t *testing.T, h *harness, c *control, root *counter) {
			h.onLoop(func() { h.app.byComp[c].ctx.SetPointerPolicy(PointerDisabled) })
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := &counter{size: Size{W: 20, H: 4}}
			c := newControl(8, 2)
			root.Add(c)
			h := startApp(t, root, 20, 4)
			defer h.wait()
			h.sync()

			press(h, 2, 1)
			waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

			tc.trigger(t, h, c, root)
			waitFor(t, "disarmed by the loss", func() bool { return c.armFalse.Load() == 1 })
			h.sync()

			if c.armed.Load() {
				t.Error("the control is still armed after its gesture died")
			}
			if got := c.armFalse.Load(); got != 1 {
				t.Errorf("SetArmed(false) called %d times, want exactly 1", got)
			}
			if got := c.activations.Load(); got != 0 {
				t.Errorf("a gesture that died activated the control %d time(s)", got)
			}
			var held NodeID
			h.onLoop(func() { held = h.app.captureOwner })
			if held != 0 {
				t.Errorf("capture still held by %d", held)
			}
		})
	}
}

// TestActivationIsPublishedOnTheBus, and only for an activation that happened.
func TestActivationIsPublishedOnTheBus(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	var mu sync.Mutex
	var got []ControlActivatedEvent
	unsub := Subscribe(h.app.Bus(), func(ev ControlActivatedEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})
	defer unsub()

	press(h, 2, 1)
	release(h, 2, 1)
	waitFor(t, "activation published", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	h.sync()

	mu.Lock()
	evs := append([]ControlActivatedEvent(nil), got...)
	mu.Unlock()

	var id NodeID
	h.onLoop(func() { id = h.app.byComp[c].id })
	if evs[0].Owner != id {
		t.Errorf("event owner = %d, want %d", evs[0].Owner, id)
	}
	if evs[0].Origin != OriginPointer {
		t.Errorf("event origin = %v, want %v", evs[0].Origin, OriginPointer)
	}

	// A REFUSED activation publishes nothing: a listener counting these is
	// counting activations, not attempts.
	c.enabled.Store(false)
	press(h, 2, 1)
	release(h, 2, 1)
	waitFor(t, "second gesture finished", func() bool { return c.armFalse.Load() == 2 })
	h.sync()

	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 1 {
		t.Errorf("%d activation events published, want 1: a refused Activate must "+
			"publish nothing", n)
	}
}

// TestOnlyAnActivatableTargetStartsAGesture. A gesture whose only outcome is an
// activation is meaningless on a component that cannot be activated — and
// engaging anyway is harmful, because the capture it takes suppresses routing
// for every pointer event until the release.
func TestOnlyAnActivatableTargetStartsAGesture(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	plain := &counter{size: Size{W: 20, H: 4}} // not Activatable
	root.Add(plain)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	press(h, 2, 1)
	waitFor(t, "press delivered", func() bool {
		_, _, m := plain.totals()
		return m > 0
	})
	h.sync()

	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("a press on a non-Activatable component took capture %d; the gesture "+
			"could produce nothing and the capture would swallow the next press", held)
	}

	// The proof that it matters: a SECOND press still reaches the component.
	press(h, 2, 1)
	waitFor(t, "second press delivered", func() bool {
		_, _, m := plain.totals()
		return m >= 2
	})
}

// TestGestureRecognitionCanBeSwitchedOff. Passing nil is the honest way to say
// "this app interprets its own pointer input".
func TestGestureRecognitionCanBeSwitchedOff(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	c := newControl(8, 2)
	sentinel := &counter{size: Size{W: 12, H: 4}}
	root.Add(c, sentinel)
	h := startApp(t, root, 20, 4, WithGestureRecognizer(nil))
	defer h.wait()
	h.sync()

	press(h, 2, 1)
	release(h, 2, 1)

	// A LANE-A sentinel, not a lane-B round trip. Injected input and App.Update
	// travel different lanes, so syncing the program lane proves nothing about
	// whether the press above has been dispatched yet — "nothing happened"
	// would be indistinguishable from "nothing has happened YET". Dispatch is
	// ordered, so a later press that IS observable proves the earlier ones were
	// processed.
	press(h, 15, 1)
	waitFor(t, "lane-A sentinel dispatched", func() bool {
		_, _, m := sentinel.totals()
		return m > 0
	})
	h.sync()

	if got := c.armCalls.Load(); got != 0 {
		t.Errorf("SetArmed was called %d times with recognition off, want 0", got)
	}
	if got := c.activations.Load(); got != 0 {
		t.Errorf("activated %d time(s) with recognition off, want 0", got)
	}
}

// countingRecognizer replaces the default so replacement itself is observable.
type countingRecognizer struct{ calls atomic.Int64 }

func (r *countingRecognizer) OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState) {
	r.calls.Add(1)
	return nil, false, GestureIdle
}

// TestACustomRecognizerReplacesTheDefault. The recogniser is a replaceable
// module behind an interface, not a fixed behaviour.
func TestACustomRecognizerReplacesTheDefault(t *testing.T) {
	r := &countingRecognizer{}
	h, c := gestureFixture(t, WithGestureRecognizer(r))
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "custom recogniser consulted", func() bool { return r.calls.Load() >= 1 })
	h.sync()

	if got := c.armCalls.Load(); got != 0 {
		t.Errorf("the DEFAULT recogniser still armed the control (%d calls); a custom "+
			"one must replace it, not run beside it", got)
	}
}

// TestTheRecognizerSeesRuntimeComputedInside. Inside is the one thing a
// recogniser cannot work out for itself: a local coordinate alone says nothing
// without the target's size.
func TestTheRecognizerSeesRuntimeComputedInside(t *testing.T) {
	rec := &insideRecorder{}
	h, _ := gestureFixture(t, WithGestureRecognizer(rec))
	defer h.wait()

	press(h, 2, 1) // inside the 8x2 control
	waitFor(t, "press seen", func() bool { return rec.seen.Load() == 1 })
	if !rec.lastInside.Load() {
		t.Error("Inside was false for a press within the control's rect")
	}

	// The outside case must arrive by the path that actually reaches the
	// recogniser once a gesture is running: CAPTURED motion. A second press
	// aimed past the control hit-tests to a different node entirely and never
	// reaches this recogniser at all, so asserting on it would prove nothing.
	rec.arm.Store(true)
	press(h, 2, 1)
	waitFor(t, "gesture armed", func() bool { return rec.seen.Load() == 2 })
	move(h, 15, 3) // outside, delivered because the gesture holds the pointer
	waitFor(t, "captured motion seen", func() bool { return rec.seen.Load() == 3 })
	if rec.lastInside.Load() {
		t.Error("Inside was true for a captured motion outside the control's rect")
	}
}

// insideRecorder records the Inside flag the runtime computed.
type insideRecorder struct {
	seen       atomic.Int64
	lastInside atomic.Bool
	arm        atomic.Bool // once set, presses begin a real gesture
}

func (r *insideRecorder) OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState) {
	r.lastInside.Store(gc.Inside)
	r.seen.Add(1)
	if r.arm.Load() {
		return nil, true, GestureArmed
	}
	return nil, false, GestureIdle
}

// TestGestureStateNamesEveryValue. The state appears in traces and failures, so
// an unnamed one turns a diagnosis into a guess.
func TestGestureStateNamesEveryValue(t *testing.T) {
	for st, want := range map[GestureState]string{
		GestureIdle:  "idle",
		GestureArmed: "armed",
	} {
		if got := st.String(); got != want {
			t.Errorf("GestureState(%d).String() = %q, want %q", st, got, want)
		}
	}
	if got := GestureState(200).String(); got != "unknown" {
		t.Errorf("an undefined state rendered as %q, want %q", got, "unknown")
	}
}

// TestReleasingOutsideWithoutMovingFirstDoesNotActivate.
//
// Distinct from the drag-out case, and the only one that tests the release's
// OWN bounds check: after dragging out, the gesture is already disarmed, so a
// release that ignored its own Inside check would still not activate and the
// bug would hide. A terminal can report a release at a cell it never reported
// motion through, so this sequence is real, not contrived.
func TestReleasingOutsideWithoutMovingFirstDoesNotActivate(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	// No motion. Straight to a release well outside the control, while the
	// gesture is still ARMED.
	release(h, 15, 3)
	waitFor(t, "disarmed", func() bool { return c.armFalse.Load() == 1 })
	h.sync()

	if got := c.activations.Load(); got != 0 {
		t.Errorf("activated %d time(s) on a release outside the control while still "+
			"armed, want 0: the release checks its own bounds, not just the arm state", got)
	}
}

// TestACapturedPressOutsideDisarms is the press branch's bounds check. A press
// reaches the recogniser with Inside false only once a gesture already holds the
// pointer — an uncaptured press always lands on the node it hit — so this is the
// one sequence that exercises it.
func TestACapturedPressOutsideDisarms(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	// A second press, delivered to the gesture because it holds the pointer,
	// but positioned outside the control.
	press(h, 15, 3)
	waitFor(t, "disarmed by the outside press", func() bool { return c.armFalse.Load() == 1 })
	h.sync()

	if c.armed.Load() {
		t.Error("a press outside the control left it armed; the press branch must " +
			"check its own bounds")
	}
	if got := c.activations.Load(); got != 0 {
		t.Errorf("activated %d time(s), want 0", got)
	}
}

// TestAPressOnAPolicyDisabledControlStartsNoGesture. Step 6 has its own policy
// gate, separate from the one that cancels a gesture already in flight: a
// control whose mouse is off must never begin one at all.
func TestAPressOnAPolicyDisabledControlStartsNoGesture(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	c := newControl(8, 2)
	sentinel := &counter{size: Size{W: 12, H: 4}}
	root.Add(c, sentinel)
	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	h.onLoop(func() { h.app.byComp[c].ctx.SetPointerPolicy(PointerDisabled) })
	h.sync()

	press(h, 2, 1)
	release(h, 2, 1)

	press(h, 15, 1) // lane-A sentinel, as above
	waitFor(t, "sentinel dispatched", func() bool {
		_, _, m := sentinel.totals()
		return m > 0
	})
	h.sync()

	if got := c.armCalls.Load(); got != 0 {
		t.Errorf("a policy-disabled control was armed %d time(s), want 0", got)
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("a policy-disabled control took capture %d", held)
	}
}

// selfHandlingControl is Activatable but consumes its own presses, the way a
// widget with its own pointer semantics would.
type selfHandlingControl struct{ control }

func (s *selfHandlingControl) HandleEvent(ev Event) bool {
	if e, ok := ev.(MouseEvent); ok && e.Kind == MousePress {
		s.presses.Add(1)
		return true // mine; nobody else's business
	}
	return false
}

// TestAConsumedPressNeverReachesTheRecognizer.
//
// The recogniser runs LAST, on what nobody wanted. A widget that handles its
// own presses must not then also be armed and captured behind its back — it
// would find itself in a gesture it never asked for, and holding the pointer
// until a release it is not expecting.
//
// The target here is Activatable, so the only thing that can stop step 6 is the
// press having been consumed. A non-Activatable fixture would pass this whether
// or not the consumed check existed.
func TestAConsumedPressNeverReachesTheRecognizer(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	c := &selfHandlingControl{control: *newControl(8, 2)}
	root.Add(c)
	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	press(h, 2, 1)
	waitFor(t, "the widget handled its own press", func() bool { return c.presses.Load() == 1 })
	h.sync()

	if got := c.armCalls.Load(); got != 0 {
		t.Errorf("the recogniser armed a control that had already consumed the press "+
			"(%d SetArmed calls, want 0)", got)
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("a consumed press still took capture %d; the widget would be holding "+
			"the pointer for a gesture it never began", held)
	}
}

// --- F1: a typed-nil action from a recogniser ---

// nilActionRecognizer arms normally but hands back a TYPED-NIL action on
// release: an interface with a live *ActivateAction descriptor and no value.
type nilActionRecognizer struct{}

func (nilActionRecognizer) OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState) {
	switch ev.Kind {
	case MousePress:
		if ev.Button == MouseLeft && gc.Inside {
			return nil, true, GestureArmed
		}
	case MouseRelease:
		if ev.Button == MouseLeft {
			var typed *ActivateAction
			return typed, true, GestureIdle // claims nothing, but not with a plain nil
		}
	}
	return nil, false, gc.State
}

// TestATypedNilRecognizerActionActivatesNothing. A typed nil passes an
// act != nil check and then matches dispatchAction's pointer arm, so a
// recogniser producing no action at all could activate a control and publish
// an event for it. Recognisers legitimately consume an event while returning
// nothing, so this is treated as an absent action rather than an error.
func TestATypedNilRecognizerActionActivatesNothing(t *testing.T) {
	h, c := gestureFixture(t, WithGestureRecognizer(nilActionRecognizer{}))
	defer h.wait()

	var mu sync.Mutex
	var events int
	unsub := Subscribe(h.app.Bus(), func(ControlActivatedEvent) {
		mu.Lock()
		events++
		mu.Unlock()
	})
	defer unsub()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })
	release(h, 2, 1)
	waitFor(t, "disarmed", func() bool { return c.armFalse.Load() == 1 })
	h.sync()

	if got := c.activations.Load(); got != 0 {
		t.Errorf("a typed-nil action activated the control %d time(s), want 0", got)
	}
	mu.Lock()
	n := events
	mu.Unlock()
	if n != 0 {
		t.Errorf("%d ControlActivatedEvent(s) published for a typed-nil action, want 0", n)
	}
	// The gesture still ends cleanly: consuming without producing is legal.
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture still held by %d", held)
	}
	if c.armed.Load() {
		t.Error("the control is still armed")
	}
}

// --- F2: runtime replacement ---

// armingCountingRecognizer both arms a gesture and counts every event it is
// shown, so "this recogniser stopped being consulted" is observable rather than
// inferred.
type armingCountingRecognizer struct{ calls atomic.Int64 }

func (r *armingCountingRecognizer) OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState) {
	r.calls.Add(1)
	if ev.Kind == MousePress && ev.Button == MouseLeft && gc.Inside {
		return nil, true, GestureArmed
	}
	return nil, false, gc.State
}

// lossControl is a control that also records the capture-loss events it is
// sent, so the cancellation can be checked at the PUBLIC boundary — the event a
// real widget would act on — rather than through the runtime's private state.
type lossControl struct {
	control
	losses     atomic.Int64
	lastReason atomic.Int64
	lastOwner  atomic.Uint64
}

func (l *lossControl) HandleEvent(ev Event) bool {
	if e, ok := ev.(PointerCaptureLostEvent); ok {
		l.losses.Add(1)
		l.lastReason.Store(int64(e.Reason))
		l.lastOwner.Store(uint64(e.Owner))
	}
	return false
}

// TestReplacingTheRecognizerMidGestureCancelsIt.
//
// No gesture may span two recognisers: the replacement would be handed a
// sequence whose beginning it never saw and whose state it cannot interpret.
//
// Everything here is observed through the public boundary. An earlier version
// of this test built an "old" recogniser, never installed it, and checked only
// that SetArmed(false) had been called — so it proved neither that the old
// recogniser stopped being consulted nor that the owner was told WHY its
// capture ended. Both of those are the actual promise.
func TestReplacingTheRecognizerMidGestureCancelsIt(t *testing.T) {
	original := &armingCountingRecognizer{}
	root := &counter{size: Size{W: 20, H: 4}}
	c := &lossControl{control: *newControl(8, 2)}
	root.Add(c)
	h := startApp(t, root, 20, 4, WithGestureRecognizer(original))
	defer h.wait()
	h.sync()

	press(h, 2, 1)
	waitFor(t, "armed by the original", func() bool { return c.armTrue.Load() == 1 })
	callsBefore := original.calls.Load()
	if callsBefore == 0 {
		t.Fatal("precondition failed: the original recogniser was never consulted")
	}

	var ownerID NodeID
	h.onLoop(func() { ownerID = h.app.byComp[c].id })

	replacement := &armingCountingRecognizer{}
	h.onLoop(func() { h.app.byComp[c].ctx.SetGestureRecognizer(replacement) })
	waitFor(t, "the owner was told its capture ended", func() bool { return c.losses.Load() == 1 })
	h.sync()

	// The cancellation, at the boundary a widget actually sees.
	if got := c.losses.Load(); got != 1 {
		t.Errorf("PointerCaptureLostEvent delivered %d times, want exactly 1", got)
	}
	if got := CaptureLostReason(c.lastReason.Load()); got != CaptureLostCancelled {
		t.Errorf("loss reason = %v, want %v: replacement is the program deciding this "+
			"gesture is over", got, CaptureLostCancelled)
	}
	if got := NodeID(c.lastOwner.Load()); got != ownerID {
		t.Errorf("loss event named owner %d, want %d", got, ownerID)
	}
	if got := c.armFalse.Load(); got != 1 {
		t.Errorf("SetArmed(false) called %d times, want exactly 1", got)
	}
	if c.armed.Load() {
		t.Error("the old target is still armed after the recogniser was replaced")
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture still held by %d after replacement", held)
	}

	// ISOLATION: the next press goes to the replacement and the original never
	// sees it again. Asserting only that the replacement advanced would pass
	// just as well against a runtime that consulted both.
	frozen := original.calls.Load()
	press(h, 2, 1)
	waitFor(t, "the replacement was consulted", func() bool { return replacement.calls.Load() >= 1 })
	h.sync()

	if got := original.calls.Load(); got != frozen {
		t.Errorf("the replaced recogniser saw %d further event(s), want 0: no gesture "+
			"and no event may reach a recogniser that has been swapped out",
			got-frozen)
	}
}

// TestRecognitionCanBeDisabledAtRuntime, by nil and by a typed nil alike.
func TestRecognitionCanBeDisabledAtRuntime(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(c *Context)
	}{
		{"nil interface", func(c *Context) { c.SetGestureRecognizer(nil) }},
		{"typed nil", func(c *Context) {
			var r *countingRecognizer
			c.SetGestureRecognizer(r)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := &counter{size: Size{W: 20, H: 4}}
			c := newControl(8, 2)
			sentinel := &counter{size: Size{W: 12, H: 4}}
			root.Add(c, sentinel)
			h := startApp(t, root, 20, 4)
			defer h.wait()
			h.sync()

			h.onLoop(func() { tc.set(h.app.byComp[c].ctx) })
			h.sync()

			press(h, 2, 1)
			release(h, 2, 1)
			press(h, 15, 1) // ordered lane-A sentinel
			waitFor(t, "sentinel dispatched", func() bool {
				_, _, m := sentinel.totals()
				return m > 0
			})
			h.sync()

			if got := c.armCalls.Load(); got != 0 {
				t.Errorf("SetArmed called %d times after runtime disablement, want 0", got)
			}
		})
	}
}

// --- F3: unrelated buttons must not end the primary gesture ---

// TestAnUnrelatedButtonDoesNotEndThePrimaryGesture. A right-click during a left
// drag changes nothing about the left drag; disarming or dropping the capture
// there would abandon a gesture the user is still performing.
func TestAnUnrelatedButtonDoesNotEndThePrimaryGesture(t *testing.T) {
	h, c := gestureFixture(t)
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	// A complete secondary click while the primary gesture is live.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseRight, X: 2, Y: 1})
	h.inject(MouseEvent{Kind: MouseRelease, Button: MouseRight, X: 2, Y: 1})

	// The primary release must still activate — which also proves the events
	// above were processed, so no separate sentinel is needed.
	release(h, 2, 1)
	waitFor(t, "the primary gesture still completed", func() bool {
		return c.activations.Load() == 1
	})
	h.sync()

	if got := c.armFalse.Load(); got != 1 {
		t.Errorf("SetArmed(false) called %d times, want exactly 1: an unrelated button "+
			"must not disarm the primary gesture", got)
	}
	if got := c.activations.Load(); got != 1 {
		t.Errorf("activations = %d, want exactly 1", got)
	}
}

// --- F4: an invalid state is refused before anything moves ---

// badStateRecognizer returns a value outside the closed enum.
type badStateRecognizer struct{}

func (badStateRecognizer) OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState) {
	if ev.Kind == MousePress && ev.Button == MouseLeft && gc.Inside {
		return nil, true, GestureArmed
	}
	if ev.Kind == MouseMotion {
		// The FIRST value past the closed set. A far-out one like 200 is
		// rejected by an off-by-one bound exactly as readily as by a correct
		// one, so it cannot tell the edge from a mistake about the edge.
		return nil, true, GestureArmed + 1
	}
	return nil, false, gc.State
}

// TestAnInvalidGestureStateIsRefusedBeforeAnythingChanges. GestureState is a
// published closed enum, so a value outside it is a broken recogniser rather
// than data — and half-applying an interpretation nothing can read is worse
// than refusing it.
func TestAnInvalidGestureStateIsRefusedBeforeAnythingChanges(t *testing.T) {
	h, c := gestureFixture(t, WithGestureRecognizer(badStateRecognizer{}))
	defer h.wait()

	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return c.armTrue.Load() == 1 })

	var before, after NodeID
	var stBefore, stAfter GestureState
	var armedAfter bool
	var fatal *errs.Fatal
	h.onLoop(func() {
		before, stBefore = h.app.captureOwner, h.app.gestureState
		n := h.app.byComp[c]
		local := MouseEvent{Kind: MouseMotion, X: 1, Y: 1}
		fatal = fatalFrom(func() { h.app.runRecognizer(badStateRecognizer{}, n, local) })
		after, stAfter, armedAfter = h.app.captureOwner, h.app.gestureState, h.app.gestureArmed
	})
	h.sync()

	if fatal == nil {
		t.Fatal("a GestureState outside the closed enum was accepted")
	}
	if after != before {
		t.Errorf("capture owner changed from %d to %d on a refused result", before, after)
	}
	if stAfter != stBefore {
		t.Errorf("gesture state changed from %v to %v on a refused result", stBefore, stAfter)
	}
	if !armedAfter {
		t.Error("the control was disarmed by a refused result; validation must happen " +
			"before any mutation")
	}
	if c.armFalse.Load() != 0 {
		t.Errorf("SetArmed(false) was called %d times on a refused result", c.armFalse.Load())
	}

	// The bound must reject ONLY what is invalid: GestureArmed itself, the last
	// legal value, still goes through.
	var legal *errs.Fatal
	h.onLoop(func() {
		n := h.app.byComp[c]
		legal = fatalFrom(func() {
			h.app.runRecognizer(&armingCountingRecognizer{}, n,
				MouseEvent{Kind: MousePress, Button: MouseLeft, X: 1, Y: 1})
		})
	})
	h.sync()
	if legal != nil {
		t.Errorf("GestureArmed was rejected (%v); the bound excludes a legal value", legal.Rule)
	}
}

// TestConstructionTimeTypedNilDisablesRecognition. The construction path shares
// the runtime path's normaliser, and this pins that it does: a future
// "simplification" of the option back to a direct assignment would install a
// typed nil, which then panics on a nil receiver at the first press.
func TestConstructionTimeTypedNilDisablesRecognition(t *testing.T) {
	var typed *armingCountingRecognizer
	root := &counter{size: Size{W: 20, H: 4}}
	c := newControl(8, 2)
	sentinel := &counter{size: Size{W: 12, H: 4}}
	root.Add(c, sentinel)
	h := startApp(t, root, 20, 4, WithGestureRecognizer(typed))
	defer h.wait()
	h.sync()

	press(h, 2, 1)
	release(h, 2, 1)
	press(h, 15, 1) // ordered lane-A sentinel
	waitFor(t, "sentinel dispatched", func() bool {
		_, _, m := sentinel.totals()
		return m > 0
	})
	h.sync()

	if got := c.armCalls.Load(); got != 0 {
		t.Errorf("SetArmed called %d times with a typed-nil recogniser installed at "+
			"construction, want 0", got)
	}
}
