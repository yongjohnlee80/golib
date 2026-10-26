package tui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// unclaimed_press_test.go: STEP 7 of a pointer press. An UnclaimedPressHandler is asked only for a
// primary press that no node on the path consumed and the gesture recogniser didn't take, innermost
// first, and runs as its own handler.

// unclaimedTaker records each unclaimed press it's asked about, in its own frame, and takes it when
// take is set. A taker set to capture takes the pointer as it takes the press, and then counts the
// pointer events its capture brings it; one with other set first tries to take the pointer in
// other's name, which the runtime refuses with a panic it keeps.
type unclaimedTaker struct {
	mu       sync.Mutex
	asked    []MouseEvent
	take     bool
	capture  bool
	other    *Context
	captured atomic.Bool
	captive  atomic.Int64
	foreign  atomic.Value // the refusal's text
}

func (u *unclaimedTaker) unclaimed(ctx *Context, e MouseEvent) bool {
	u.mu.Lock()
	u.asked = append(u.asked, e)
	u.mu.Unlock()
	if u.other != nil {
		func() {
			defer func() {
				if p := recover(); p != nil {
					u.foreign.Store(fmt.Sprint(p))
				}
			}()
			u.other.CapturePointer()
		}()
	}
	if u.capture {
		u.captured.Store(ctx.CapturePointer())
	}
	return u.take
}

func (u *unclaimedTaker) seen() []MouseEvent {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]MouseEvent(nil), u.asked...)
}

// captiveEvent counts a pointer event its capture brought it, releasing on the release.
func (u *unclaimedTaker) captiveEvent(ctx *Context, ev Event) bool {
	e, ok := ev.(MouseEvent)
	if !ok || !u.captured.Load() || e.Kind == MousePress {
		return false
	}
	u.captive.Add(1)
	if e.Kind == MouseRelease {
		ctx.ReleasePointer()
		u.captured.Store(false)
	}
	return true
}

// unclaimedRoot is a counter (its children side by side) that is an UnclaimedPressHandler.
type unclaimedRoot struct {
	counter
	unclaimedTaker
}

func (r *unclaimedRoot) HandleUnclaimedPress(e MouseEvent) bool { return r.unclaimed(r.ctx, e) }
func (r *unclaimedRoot) HandleEvent(ev Event) bool {
	if r.captiveEvent(r.ctx, ev) {
		return true
	}
	return r.counter.HandleEvent(ev)
}

// unclaimedLeaf is a leaf that consumes every press when consume is set, and is an
// UnclaimedPressHandler too.
type unclaimedLeaf struct {
	unclaimedTaker
	size    Size
	ctx     *Context
	consume bool
	presses atomic.Int64
}

func (l *unclaimedLeaf) Init(ctx *Context)          { l.ctx = ctx }
func (l *unclaimedLeaf) Layout(cs Constraints) Size { return cs.Constrain(l.size) }
func (l *unclaimedLeaf) Render(Surface)             {}
func (l *unclaimedLeaf) HandleEvent(ev Event) bool {
	if e, ok := ev.(MouseEvent); ok && e.Kind == MousePress {
		l.presses.Add(1)
		return l.consume
	}
	return false
}
func (l *unclaimedLeaf) HandleUnclaimedPress(e MouseEvent) bool { return l.unclaimed(l.ctx, e) }

// gatedControl is a control the runtime asks whether it can activate now (ActivationAvailability),
// so a disabled one starts no gesture.
type gatedControl struct{ *control }

func (g gatedControl) ActivationAvailable() bool { return g.enabled.Load() }

// unclaimedFixture is a 20 by 4 root holding, from the left, a leaf that consumes presses (columns
// 0-3), an Activatable control (4-7) and a leaf that declines them (8-11), each 4 by 2. The rest of
// the root (from column 12, and rows 2-3) is its own.
func unclaimedFixture(t *testing.T) (*harness, *unclaimedRoot, *unclaimedLeaf, *control, *unclaimedLeaf) {
	t.Helper()
	root := &unclaimedRoot{counter: counter{size: Size{W: 20, H: 4}}}
	root.take = true
	consumer := &unclaimedLeaf{size: Size{W: 4, H: 2}, consume: true}
	consumer.take = true
	c := newControl(4, 2)
	decliner := &unclaimedLeaf{size: Size{W: 4, H: 2}}
	root.Add(consumer)
	root.Add(gatedControl{c})
	root.Add(decliner)
	h := startApp(t, root, 20, 4)
	h.sync()
	return h, root, consumer, c, decliner
}

// A press a node on its path consumes is never unclaimed. One the recogniser takes, a click on the
// Activatable control, isn't either, and the control activates. One nothing takes is offered
// innermost first: the declining leaf is asked, then the root, in their own frames. A press on the
// root's own cells reaches it directly. A disabled control's press is nobody's, so it's unclaimed.
// Only a primary press is offered: a right or middle press, a motion and a release never are.
func TestAnUnclaimedPressIsAskedOnlyAfterThePathAndTheRecogniserDecline(t *testing.T) {
	h, root, consumer, c, decliner := unclaimedFixture(t)
	defer h.wait()
	asked := func() (int, int, int) { return len(consumer.seen()), len(decliner.seen()), len(root.seen()) }

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 1, Y: 0})
	waitFor(t, "the consumer's press", func() bool { return consumer.presses.Load() == 1 })
	h.sync()
	if a, b, r := asked(); a+b+r != 0 {
		t.Fatalf("a consumed press was offered as unclaimed: consumer %d, decliner %d, root %d", a, b, r)
	}

	press(h, 5, 1)
	release(h, 5, 1)
	waitFor(t, "the control's click", func() bool { return c.activations.Load() == 1 })
	h.sync()
	if a, b, r := asked(); a+b+r != 0 {
		t.Fatalf("a press the recogniser took was offered as unclaimed: consumer %d, decliner %d, root %d", a, b, r)
	}

	press(h, 9, 1)
	waitFor(t, "the root asked", func() bool { return len(root.seen()) == 1 })
	if d := decliner.seen(); len(d) != 1 || d[0].X != 1 || d[0].Y != 1 {
		t.Fatalf("the leaf under the press was not asked first, in its own frame: %+v", d)
	}
	if r := root.seen(); r[0].X != 9 || r[0].Y != 1 {
		t.Fatalf("the root was asked in another frame than its own: %+v", r[0])
	}

	press(h, 15, 3)
	waitFor(t, "the root asked for its own cell", func() bool { return len(root.seen()) == 2 })

	c.enabled.Store(false)
	h.onLoop(func() {})
	press(h, 5, 0)
	waitFor(t, "a disabled control's press unclaimed", func() bool { return len(root.seen()) == 3 })

	h.inject(MouseEvent{Kind: MousePress, Button: MouseRight, X: 9, Y: 0},
		MouseEvent{Kind: MouseRelease, Button: MouseRight, X: 9, Y: 0},
		MouseEvent{Kind: MousePress, Button: MouseMiddle, X: 15, Y: 3},
		MouseEvent{Kind: MouseRelease, Button: MouseMiddle, X: 15, Y: 3},
		MouseEvent{Kind: MouseMotion, X: 10, Y: 1},
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 9, Y: 1},
		// the fence: input is dispatched in order, so once this one is asked about, so were those
		MouseEvent{Kind: MousePress, Button: MouseLeft, X: 16, Y: 3})
	waitFor(t, "the fence press", func() bool { r := root.seen(); return len(r) > 3 && r[len(r)-1].X == 16 })
	if a, b, r := asked(); a != 0 || b != 1 || r != 4 {
		t.Fatalf("after the right and middle presses, a motion, a release and the fence: consumer %d, decliner %d, root %d asked; want 0, 1, 4", a, b, r)
	}
}

// HandleUnclaimedPress runs as its node's own handler: it may take the pointer, whose motion and
// release then come to that node, but taking it in another node's name is refused, as anywhere.
func TestAnUnclaimedPressTakesThePointerAsItsOwnHandler(t *testing.T) {
	h, root, _, c, _ := unclaimedFixture(t)
	defer h.wait()
	root.capture = true
	h.onLoop(func() { root.other = c.ctx })

	press(h, 15, 3)
	waitFor(t, "the root's capture", func() bool { return root.captured.Load() })
	if f, _ := root.foreign.Load().(string); !strings.Contains(f, "legal only while this component's own") {
		t.Fatalf("taking the pointer in the control's name wasn't refused: %q", f)
	}
	move(h, 2, 0) // over the consumer: the capture brings it to the root
	release(h, 2, 0)
	waitFor(t, "the captured motion and release", func() bool { return root.captive.Load() == 2 })
}
