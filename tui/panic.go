package tui

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// PanicPolicy selects what Run does after a loop/handler panic has been
// recovered and the terminal restored.
type PanicPolicy uint8

const (
	// PanicRepanic (the default) propagates the recovered panic with its
	// original value after the terminal is restored — golib fail-loud.
	PanicRepanic PanicPolicy = iota
	// PanicReturn converts the recovered panic into an error wrapping
	// ErrPanic returned from Run.
	PanicReturn
)

var (
	// ErrPanic is wrapped by Run's returned error under PanicReturn.
	ErrPanic = errors.New("tui: recovered panic")

	// ErrTaskPanic is wrapped by TaskResult.Err when the task panicked.
	ErrTaskPanic = errors.New("tui: task panicked")
)

// PanicError is the panic Run recovered under PanicReturn: its value, and the stack it was raised
// on, for a crash report. errors.As finds it in Run's error; it unwraps to the value when the
// value is an error, so a panicked errs.Fatal stays reachable too.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprint(e.Value) }

// Unwrap is the panicked value, when it is an error.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// HandlerPanic is a panic a handler boundary contained (WithHandlerRecover): where, whose, what,
// and the stack it was raised on.
type HandlerPanic struct {
	// Phase is the boundary: "event" (HandleEvent, a task's result, a timer's tick), "action"
	// (HandleAction, a control's Activate), "update" (an App.Update callback) or "bus" (a Bus
	// subscriber).
	Phase string
	Node  NodeID // the node whose handler panicked; 0 for an update or a bus delivery
	Label string // that node's component type, as %T prints it; "" with no node
	Value any
	Stack []byte
}

// WithHandlerRecover contains a panic at the handler boundaries, rather than ending Run: one
// handler failing does not take the program down. A panic raised in HandleEvent, HandleAction (a
// control's Activate too), an App.Update callback or a Bus subscriber is recovered there, fn is
// called on the loop with it, any pointer capture is cancelled (the input that made the gesture is
// suspect, and a capture left held would take every later click), and the loop goes on. The phase
// markers are restored by the boundaries as on any return, so the next event is delivered as
// usual.
//
// Some panics stay fatal, contained or not, and reach Run's PanicPolicy: one in Layout or Render;
// one raised while the tree was part way through a change (a component's Init panicking as it
// mounts, an unmount's hooks), after which the App is not safe to go on; and one raised by fn
// itself. golib's own contract checks (errs.Fatal) raised in a handler are contained: they fire
// before any change.
//
// Off by default: unset, every panic reaches Run, as before.
func WithHandlerRecover(fn func(HandlerPanic)) AppOption {
	return func(c *appConfig) { c.handlerRecover = fn }
}

// contain is the boundaries' deferred recover: a handler's panic, contained when the program asked
// for it (WithHandlerRecover) and the App is safe to go on; otherwise the panic goes on to Run.
// handled, when given, is set: a handler that panicked has had the event.
func (a *App) contain(phase string, n *node, handled *bool) {
	fn := a.cfg.handlerRecover
	if fn == nil {
		return // no recover: the panic goes on to Run, as it always did
	}
	rec := recover()
	if rec == nil {
		return
	}
	if a.phase.inLayout || a.phase.inRender || a.structural > 0 || a.poisoned {
		panic(rec) // not safe to go on: Run's policy decides
	}
	hp := HandlerPanic{Phase: phase, Value: rec, Stack: debug.Stack()}
	if n != nil {
		hp.Node, hp.Label = n.id, fmt.Sprintf("%T", n.comp)
	}
	if handled != nil {
		*handled = true
	}
	if a.capture.captureOwner != 0 {
		a.loseCapture(CaptureLostCancelled)
	}
	fn(hp)
}

// leaveStructural ends a tree change begun with a.structural++. A panic leaving the change part
// made poisons the App: no boundary contains a panic after that, since the tree may be half
// mounted.
func (a *App) leaveStructural() {
	a.structural--
	if rec := recover(); rec != nil {
		a.poisoned = true
		panic(rec)
	}
}
