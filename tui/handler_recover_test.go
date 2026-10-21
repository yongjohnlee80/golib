package tui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// handler_recover_test.go: WithHandlerRecover contains a handler's panic at its boundary (an
// event, an action, an Update, a Bus delivery) and the loop goes on; a panic in Layout or Render,
// one that leaves the tree part changed, and one in the callback itself stay fatal.

// contained records the panics a guard was handed; the guard runs on the loop.
type contained struct {
	mu  sync.Mutex
	got []HandlerPanic
}

func (c *contained) guard(hp HandlerPanic) { c.mu.Lock(); c.got = append(c.got, hp); c.mu.Unlock() }

func (c *contained) all() []HandlerPanic {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]HandlerPanic(nil), c.got...)
}

// runEnded is Run's result once it has ended by itself (a fatal panic).
func runEnded(t *testing.T, h *harness) runResult {
	t.Helper()
	select {
	case r := <-h.resc:
		h.stopOnce.Do(func() { h.res = r })
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not end after a fatal panic")
		return runResult{}
	}
}

// A panic in HandleEvent is contained: the guard has its phase, node, value and stack, and the
// next key is delivered as usual.
func TestAContainedEventPanicLeavesTheLoopRunning(t *testing.T) {
	t.Parallel()
	c := &contained{}
	var ys int
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(_ *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok {
			switch k.Code {
			case 'x':
				panic("boom")
			case 'y':
				ys++
				return true
			}
		}
		return false
	}
	h := startApp(t, root, 4, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.inject(keyEv('x'), keyEv('y'))
	waitFor(t, "the key after the panic", func() bool {
		var n int
		h.onLoop(func() { n = ys })
		return n == 1
	})
	got := c.all()
	if len(got) != 1 {
		t.Fatalf("%d panics contained, want 1", len(got))
	}
	hp := got[0]
	var rootID NodeID
	h.onLoop(func() { rootID = h.app.byComp[root].id })
	if hp.Phase != "event" || hp.Node != rootID || hp.Label != "*tui.probe" || hp.Value != "boom" ||
		!strings.Contains(string(hp.Stack), "handler_recover_test.go") {
		t.Errorf("contained %+v (stack %d bytes)", hp, len(hp.Stack))
	}
	if res := h.wait(); res.err != nil || res.rec != nil {
		t.Errorf("Run ended with %v / %v after a contained panic", res.err, res.rec)
	}
}

// panickyControl is an Activatable whose first activation panics, and whose later ones cancel the
// gesture that made them, as a Button disabling itself does (the #271 corner): the phase markers
// a contained panic leaves must be the next activation's own.
type panickyControl struct {
	*control
	panicked bool
}

func (c *panickyControl) Activate(origin ActionOrigin) bool {
	if !c.panicked {
		c.panicked = true
		panic("activation failed")
	}
	if !c.control.Activate(origin) {
		return false
	}
	c.ctx.CancelGesture()
	return true
}

// A panic in an activation (the action phase) is contained, the pointer it held is released, and
// the next click activates, its own gesture cancel legal in its own action phase.
func TestAContainedActionPanicLeavesThePhaseBalanced(t *testing.T) {
	t.Parallel()
	c := &contained{}
	root := &counter{size: Size{W: 20, H: 4}}
	pc := &panickyControl{control: newControl(8, 2)}
	root.Add(pc)
	h := startApp(t, root, 20, 4, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.sync()

	press(h, 2, 1)
	release(h, 2, 1)
	waitFor(t, "the activation's panic contained", func() bool { return len(c.all()) == 1 })
	var held NodeID
	var handler NodeID
	h.onLoop(func() { held, handler = h.app.capture.captureOwner, h.app.phase.handlerNode })
	if held != 0 || handler != 0 {
		t.Fatalf("after the contained panic: capture %d, handler phase %d; want neither", held, handler)
	}
	if hp := c.all()[0]; hp.Phase != "action" || hp.Value != "activation failed" {
		t.Errorf("contained %+v", hp)
	}

	press(h, 2, 1)
	release(h, 2, 1)
	waitFor(t, "the next click activates", func() bool { return pc.activations.Load() == 1 })
	h.sync()
	if n := len(c.all()); n != 1 {
		t.Errorf("%d panics after a normal click, want the first only", n)
	}
	if res := h.wait(); res.err != nil || res.rec != nil {
		t.Errorf("Run ended with %v / %v", res.err, res.rec)
	}
}

// panickyHandler handles an action by panicking.
type panickyHandler struct{ *control }

func (panickyHandler) HandleAction(ActionInvocation) bool { panic("action handler failed") }

// A panic in HandleAction itself is the action phase's too.
func TestAContainedHandleActionPanic(t *testing.T) {
	t.Parallel()
	c := &contained{}
	ph := panickyHandler{newControl(8, 2)}
	h := startApp(t, ph, 8, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.sync()
	h.onLoop(func() { ph.ctx.DoAction(ActivateAction{}) })
	if got := c.all(); len(got) != 1 || got[0].Phase != "action" || got[0].Value != "action handler failed" {
		t.Fatalf("contained %+v", got)
	}
	if res := h.wait(); res.err != nil || res.rec != nil {
		t.Errorf("Run ended with %v / %v", res.err, res.rec)
	}
}

// A panic in an App.Update callback is contained, and the loop answers the next Update.
func TestAContainedUpdatePanic(t *testing.T) {
	t.Parallel()
	c := &contained{}
	h := startApp(t, &probe{name: "root", pref: Size{W: 4, H: 2}}, 4, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.app.Update(func() { panic("update failed") })
	h.sync() // the loop answers after it
	if got := c.all(); len(got) != 1 || got[0].Phase != "update" || got[0].Node != 0 || got[0].Label != "" {
		t.Fatalf("contained %+v", got)
	}
	if res := h.wait(); res.err != nil || res.rec != nil {
		t.Errorf("Run ended with %v / %v", res.err, res.rec)
	}
}

type busPanicEvent struct{}

// A panic in a Bus subscriber is contained, and the next subscriber of the same delivery still
// hears it.
func TestAContainedBusPanicLeavesTheOtherSubscribers(t *testing.T) {
	t.Parallel()
	c := &contained{}
	h := startApp(t, &probe{name: "root", pref: Size{W: 4, H: 2}}, 4, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	var heard int
	Subscribe(h.app.Bus(), func(busPanicEvent) { panic("subscriber failed") })
	Subscribe(h.app.Bus(), func(busPanicEvent) { heard++ })
	h.onLoop(func() { h.app.Bus().Publish(busPanicEvent{}) })
	waitFor(t, "the second subscriber", func() bool {
		var n int
		h.onLoop(func() { n = heard })
		return n == 1
	})
	if got := c.all(); len(got) != 1 || got[0].Phase != "bus" || got[0].Value != "subscriber failed" {
		t.Fatalf("contained %+v", got)
	}
}

// A contained panic cancels the pointer capture a gesture held, whoever's handler panicked: the
// control is disarmed and holds nothing.
func TestAContainedPanicReleasesTheCapture(t *testing.T) {
	t.Parallel()
	c := &contained{}
	root := panicOnX{&counter{size: Size{W: 20, H: 4}}}
	ctl := newControl(8, 2)
	root.Add(ctl)
	h := startApp(t, root, 20, 4, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.sync()
	press(h, 2, 1)
	waitFor(t, "armed", func() bool { return ctl.armed.Load() })
	h.inject(keyEv('x'))
	waitFor(t, "the panic contained", func() bool { return len(c.all()) == 1 })
	var held NodeID
	h.onLoop(func() { held = h.app.capture.captureOwner })
	if held != 0 || ctl.armed.Load() {
		t.Errorf("after a contained panic: capture %d, armed %v", held, ctl.armed.Load())
	}
}

// panicOnX is a container whose own key handler panics on x.
type panicOnX struct{ *counter }

func (p panicOnX) HandleEvent(ev Event) bool {
	if k, ok := ev.(KeyEvent); ok && k.Code == 'x' {
		panic("key handler failed")
	}
	return p.counter.HandleEvent(ev)
}

// A panic in Render is fatal even with the guard: Run returns it under PanicReturn, the
// PanicError carrying its stack, and the guard is never called.
func TestARenderPanicStaysFatal(t *testing.T) {
	t.Parallel()
	c := &contained{}
	root := &renderPanic{probe: &probe{name: "root", pref: Size{W: 4, H: 2}}}
	h := startApp(t, root, 4, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.onLoop(func() { root.armed = true; root.ctx.MarkDirty() })
	res := runEnded(t, h)
	var pe *PanicError
	if !errors.Is(res.err, ErrPanic) || !errors.As(res.err, &pe) || pe.Value != "render failed" ||
		!strings.Contains(string(pe.Stack), "Render") {
		t.Fatalf("Run returned %v (PanicError %+v)", res.err, pe)
	}
	if n := len(c.all()); n != 0 {
		t.Errorf("the guard contained %d Render panics", n)
	}
}

type renderPanic struct {
	*probe
	armed bool
}

func (r *renderPanic) Render(s Surface) {
	if r.armed {
		panic("render failed")
	}
	r.probe.Render(s)
}

// A child whose Init panics as a handler mounts it leaves the tree part changed: the App is
// poisoned and the panic is fatal, contained by nothing.
func TestAPanicMidMountStaysFatal(t *testing.T) {
	t.Parallel()
	c := &contained{}
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'm' {
			p.ctx.Mount(&probe{name: "child", onInit: func(*probe, *Context) { panic("init failed") }})
			return true
		}
		return false
	}
	h := startApp(t, root, 4, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.inject(keyEv('m'))
	res := runEnded(t, h)
	var pe *PanicError
	if !errors.As(res.err, &pe) || pe.Value != "init failed" {
		t.Fatalf("Run returned %v", res.err)
	}
	if n := len(c.all()); n != 0 {
		t.Errorf("the guard contained %d mid-mount panics", n)
	}
}

// golib's own contract checks raised in a handler fire before any change, and are contained: a
// handler mounting nil is told so, and the program goes on, not poisoned.
func TestAContractCheckInAHandlerIsContained(t *testing.T) {
	t.Parallel()
	c := &contained{}
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'n' {
			p.ctx.Mount(nil)
		}
		return false
	}
	h := startApp(t, root, 4, 2, WithHandlerRecover(c.guard), WithPanicPolicy(PanicReturn))
	h.inject(keyEv('n'))
	waitFor(t, "the check contained", func() bool { return len(c.all()) == 1 })
	var f errs.Fatal
	err, _ := c.all()[0].Value.(error)
	if !errors.As(err, &f) {
		t.Fatalf("contained %v, want the errs.Fatal", c.all()[0].Value)
	}
	var poisoned bool
	h.onLoop(func() { poisoned = h.app.poisoned })
	if poisoned {
		t.Error("a check that changed nothing poisoned the App")
	}
	if res := h.wait(); res.err != nil || res.rec != nil {
		t.Errorf("Run ended with %v / %v", res.err, res.rec)
	}
}

// A panic in the guard itself is fatal.
func TestAPanicInTheGuardIsFatal(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(_ *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'x' {
			panic("handler failed")
		}
		return false
	}
	h := startApp(t, root, 4, 2, WithHandlerRecover(func(HandlerPanic) { panic("guard failed") }), WithPanicPolicy(PanicReturn))
	h.inject(keyEv('x'))
	res := runEnded(t, h)
	var pe *PanicError
	if !errors.As(res.err, &pe) || pe.Value != "guard failed" {
		t.Fatalf("Run returned %v", res.err)
	}
}

// Without the guard every handler panic reaches Run, as before; under PanicReturn its error
// carries the stack (PanicError), and a panicked error stays reachable through it.
func TestWithoutTheGuardRunReturnsThePanicWithItsStack(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	root.onEvent = func(p *probe, ev Event) bool {
		if k, ok := ev.(KeyEvent); ok && k.Code == 'n' {
			p.ctx.Mount(nil) // an errs.Fatal
		}
		return false
	}
	h := startApp(t, root, 4, 2, WithPanicPolicy(PanicReturn))
	h.inject(keyEv('n'))
	res := runEnded(t, h)
	var pe *PanicError
	var f errs.Fatal
	if !errors.Is(res.err, ErrPanic) || !errors.As(res.err, &pe) || !strings.Contains(string(pe.Stack), "handler_recover_test.go") ||
		!errors.As(res.err, &f) || !strings.Contains(res.err.Error(), "nil component") {
		t.Fatalf("Run returned %v", res.err)
	}
}
