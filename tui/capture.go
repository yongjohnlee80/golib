package tui

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// POINTER CAPTURE.
//
// Routing hit-tests every pointer event afresh, which is right for clicks and
// wrong for drags: the moment the pointer leaves the widget that started one,
// motion and release stop arriving and the gesture is stranded with no way to
// finish. Capture is the narrow, opt-in exception — while it is held, press,
// motion and release go to the owner wherever the pointer travels.
//
// The runtime owns the bookkeeping end to end, so a widget that forgets to
// release cannot strand the pointer. Every way a gesture can die releases it:
// the owner unmounting, becoming invisible, focus leaving the owner's subtree,
// a newly active focus trap excluding the owner, the owner cancelling, the
// terminal losing focus, or the App shutting down.
//
// Capture may be taken from a component's own HandleEvent or its own
// HandleAction, and a captured event runs the same per-node sequence an
// uncaptured one runs — so a drag begun as a semantic action continues as one
// rather than degrading into raw events halfway through.

// CaptureKind distinguishes who owns a capture, which decides how a captured
// event is routed.
type CaptureKind uint8

const (
	// CaptureRaw is held by a component that asked for it directly. Its events
	// still run the owner's full per-node sequence — policy gate, resolver,
	// action, then raw — and skip only gesture recognition. "Raw" names who
	// took the capture, not what the owner is delivered.
	CaptureRaw CaptureKind = iota

	// CaptureGesture is held by the runtime on a component's behalf, so that
	// captured events can be interpreted before the owner sees them. Nothing
	// acquires it yet; it exists so that adding the interpreter later does not
	// have to change the meaning of an already-published constant.
	CaptureGesture
)

// CaptureLostReason says why a capture ended without the owner releasing it.
type CaptureLostReason uint8

const (
	// CaptureLostUnmount: the owner left the tree.
	CaptureLostUnmount CaptureLostReason = iota
	// CaptureLostHidden: the owner is no longer laid out and on screen.
	CaptureLostHidden
	// CaptureLostFocusChange: focus moved outside the owner's subtree.
	CaptureLostFocusChange
	// CaptureLostFocusScope: a trapping focus scope became active that does
	// not contain the owner.
	CaptureLostFocusScope
	// CaptureLostCancelled: the owner called Context.CancelGesture.
	CaptureLostCancelled
	// CaptureLostBackend: the terminal window itself lost focus, so no further
	// pointer input is coming.
	CaptureLostBackend
	// CaptureLostShutdown: the App is stopping.
	CaptureLostShutdown
)

// String names the reason in the same words the constants use, so a trace line
// or a test failure reads without a lookup table.
func (r CaptureLostReason) String() string {
	switch r {
	case CaptureLostUnmount:
		return "unmount"
	case CaptureLostHidden:
		return "hidden"
	case CaptureLostFocusChange:
		return "focus-change"
	case CaptureLostFocusScope:
		return "focus-scope"
	case CaptureLostCancelled:
		return "cancelled"
	case CaptureLostBackend:
		return "backend"
	case CaptureLostShutdown:
		return "shutdown"
	}
	return "unknown"
}

// PointerCaptureLostEvent tells the capture owner that its capture ended for a
// reason other than the owner releasing it, so it can discard whatever gesture
// state it was keeping — the grabbed edge, the drag origin, the geometry it
// sampled when the drag began.
//
// It is addressed to the owner and never bubbles: another widget's gesture
// ending is not an ancestor's business, and a container that happened to sit
// above two draggable children could not tell which one it was hearing about.
//
// Capture is cleared before this is delivered, so a handler sees
// HasPointerCapture() == false and cannot re-enter the loss path.
type PointerCaptureLostEvent struct {
	Owner  NodeID
	Reason CaptureLostReason
}

func (PointerCaptureLostEvent) isEvent() {}

// CapturePointer routes subsequent pointer press, motion and release to this
// node until it is released or lost, and reports whether it was granted.
//
// Legal only while this node's own HandleEvent or HandleAction is running, and
// a panic otherwise: capture with no gesture in hand is meaningless, and one
// taken during Init or Layout would outlive the thing that caused it.
//
// Both input phases qualify because both are runtime-dispatched with a live
// gesture in hand, and because a drag begun semantically must be able to take
// the pointer: a resize handle resolves a press into a begin-drag action and
// captures from HandleAction, which is what lets keyboard and pointer share one
// path instead of each having its own.
//
// It returns false and changes nothing when another node already holds the
// capture — a capture is never stolen, because the widget that owns the drag
// in progress is the one that can finish it — or when the caller sits outside
// the active trapping focus scope, which would otherwise be a way to reach
// around a modal. Re-acquiring from the node that already holds it is
// idempotent and returns true.
func (c *Context) CapturePointer() bool {
	a := c.app
	// The running handler must be THIS node's. A Context outlives the handler
	// that was given it, so a component holding a reference to another's could
	// otherwise take the pointer in that node's name from inside its own
	// handler — and every loss check would then be measured against the wrong
	// subtree. Checking identity rather than "some handler is running" is what
	// makes the promise in this method's doc comment true.
	if a.handlerNode != c.node.id {
		panic(errs.Fatal{
			Op:   "tui: Context.CapturePointer",
			Rule: "legal only while this component's own HandleEvent or HandleAction is running",
			Detail: fmt.Sprintf("requested for node %d while node %d is handling",
				c.node.id, a.handlerNode),
		})
	}
	if !c.node.mounted {
		return false
	}
	if a.captureOwner == c.node.id {
		return true
	}
	// Both refusals are traced. "My drag never started" is the symptom either
	// one produces, and without a record the widget's own silence is the only
	// evidence there is.
	if a.captureOwner != 0 {
		a.trace(TraceEvent{Kind: TraceCapture, Node: c.node.id, Prev: a.captureOwner,
			Detail: "capture refused: already held by another node"})
		return false
	}
	if scope := a.confinement(); scope != nil && !withinScope(c.node, scope) {
		a.trace(TraceEvent{Kind: TraceCapture, Node: c.node.id, Prev: scope.id,
			Detail: "capture refused: caller is outside the active focus scope"})
		return false
	}
	// A node that may not RECEIVE pointer input may not HOLD the pointer. Only
	// pointer events are gated by policy, so without this a node under a
	// disabled ancestor could still take the capture from a key-driven handler
	// and then sit holding a gesture whose every event the gate discards.
	if effectivePointerPolicy(c.node) == PointerDisabled {
		a.trace(TraceEvent{Kind: TraceCapture, Node: c.node.id,
			Detail: "capture refused: pointer input is disabled for this node"})
		return false
	}
	a.setCapture(c.node.id, CaptureRaw)
	return true
}

// ReleasePointer ends a capture this node holds. It delivers nothing, because
// the caller already knows. Releasing when this node holds nothing is a no-op.
//
// Legal from a component's HandleEvent or HandleAction, from an App.Update
// callback, and from the commit phase; illegal inside Init, Layout and Render.
//
// The programmatic transitions that most need to release — hiding an overlay,
// zooming a pane, resizing from a setter — run outside event handling, so
// restricting this to handlers would leave them no way to end a drag they have
// just invalidated.
func (c *Context) ReleasePointer() {
	a := c.app
	a.assertReleasablePhase("ReleasePointer")
	if a.captureOwner == c.node.id {
		a.clearCapture()
	}
}

// CancelGesture ends a capture this node holds AND tells it so, by delivering
// PointerCaptureLostEvent with CaptureLostCancelled.
//
// It is the sanctioned way for a programmatic transition to abort a gesture the
// runtime cannot infer is over. A zoomed pane stays mounted, visible and
// focused while its divider drag has to end, so none of the automatic losses
// fire; without this the widget would keep drag state the user can no longer
// see.
//
// A no-op when this node holds nothing, which also means calling it from inside
// the owner's own loss handler does nothing: capture is already cleared by
// then, so cancellation cannot recurse.
func (c *Context) CancelGesture() {
	a := c.app
	a.assertReleasablePhase("CancelGesture")
	if a.captureOwner == c.node.id {
		a.loseCapture(CaptureLostCancelled)
	}
}

// HasPointerCapture reports whether this node currently holds the pointer.
// Read-only, and legal from every phase including Layout and Render, so a
// component may render itself differently while dragging.
func (c *Context) HasPointerCapture() bool {
	return c.app.captureOwner == c.node.id
}

// assertReleasablePhase admits the phases in which ending a capture is
// meaningful and safe, and rejects everything else.
//
// The legal set is stated POSITIVELY. An earlier version listed the forbidden
// phases instead and named only Layout and Render, which silently admitted two
// more: Init, where the node cannot yet hold a capture at all, and any callback
// reached outside an Update — including one running on another goroutine, where
// touching loop-owned capture state is a data race rather than merely a
// contract breach. A negative list has to be complete to be correct, and this
// one was not.
//
// An input handler — HandleEvent or HandleAction — or an Update callback is
// the whole legal set. The commit phase joins it when that layer exists.
//
// Layout and Render VETO regardless of what encloses them. They are always
// reached from somewhere — usually an Update or a handler — so treating the
// admitting phases as sufficient would let a Layout that an Update triggered
// mutate dispatch state mid-walk. Being inside a legal phase is necessary, not
// sufficient.
func (a *App) assertReleasablePhase(op string) {
	if a.inLayout || a.inRender {
		panic(errs.Fatal{
			Op:   fmt.Sprintf("tui: Context.%s", op),
			Rule: "illegal inside Layout or Render, which must not mutate dispatch state",
		})
	}
	if a.handlerNode != 0 || a.updateDepth > 0 {
		return
	}
	panic(errs.Fatal{
		Op:   fmt.Sprintf("tui: Context.%s", op),
		Rule: "legal only from a component's HandleEvent or HandleAction, or an App.Update callback",
	})
}

// setCapture records a new owner. Callers have already checked eligibility.
func (a *App) setCapture(owner NodeID, kind CaptureKind) {
	a.captureOwner = owner
	a.captureKind = kind
	// Sample the focus now. The owner need not be focusable at all — a pane
	// divider is not — so "the owner lost focus" would never become true for
	// it; what matters is focus leaving the owner's SUBTREE, and deciding that
	// needs a baseline taken while the capture was still valid.
	a.captureFocus = a.focused
	a.trace(TraceEvent{Kind: TraceCapture, Node: owner, Detail: "pointer captured"})
}

// resetCapture drops the capture state and says nothing. It is shared by the
// explicit and involuntary paths so that each can emit its OWN trace record and
// only that one.
func (a *App) resetCapture() {
	a.captureOwner = 0
	a.captureKind = CaptureRaw
	a.captureFocus = 0
}

// clearCapture forgets the capture without telling the owner, which is what an
// explicit release wants, and records that a release is what happened.
func (a *App) clearCapture() {
	if a.captureOwner == 0 {
		return
	}
	a.trace(TraceEvent{Kind: TraceCapture, Node: a.captureOwner, Detail: "pointer released"})
	a.resetCapture()
}

// loseCapture ends a capture involuntarily and notifies the owner exactly once.
//
// Ownership is cleared BEFORE the notification is delivered, and that ordering
// is what makes it exactly-once rather than merely usually-once: a handler that
// reacts by releasing or cancelling finds nothing to release, and a second loss
// for the same acquisition is impossible because there is no longer an owner to
// lose one.
func (a *App) loseCapture(reason CaptureLostReason) {
	owner := a.captureOwner
	if owner == 0 {
		return
	}
	n := a.nodes[owner]
	// resetCapture, not clearCapture: the owner did NOT release, and emitting a
	// "released" record here put an event on the trace that never happened,
	// directly before the record explaining what actually did.
	a.resetCapture()
	a.trace(TraceEvent{Kind: TraceCapture, Node: owner,
		Detail: "pointer capture lost: " + reason.String()})
	if n != nil && n.mounted {
		a.deliverTo(n, PointerCaptureLostEvent{Owner: owner, Reason: reason})
	}
}

// captureLostOnUnmount is called as the unmount cascade reaches a node and
// BEFORE that node is torn down, so the owner is still legally callable when it
// hears about the loss. Delivering afterwards would be calling a method on an
// unmounted component.
func (a *App) captureLostOnUnmount(n *node) {
	if a.captureOwner != 0 && a.captureOwner == n.id {
		a.loseCapture(CaptureLostUnmount)
	}
}

// captureCheckFocus ends a capture once focus has left the owner's subtree.
//
// Focus moving WITHIN the subtree does not end it: a drag begun on a divider
// while one of the panes it separates holds focus must survive focus moving
// between those panes, which is an ordinary consequence of the drag itself.
//
// The sampled focus taken at acquisition is the transition state this compares
// against, and a retained in-subtree move RE-BASELINES it. Without that the
// sample would only ever describe the instant the drag began, and the field
// would be written and never read — which is what it was.
//
// Focus becoming nothing at all counts as leaving. A non-focusable owner — a
// divider, a resize grip — is the case that makes this matter: its only focused
// descendant can be removed, leaving focus genuinely outside the subtree with
// no new node to point at.
func (a *App) captureCheckFocus() {
	owner := a.nodes[a.captureOwner]
	if owner == nil {
		return
	}
	if a.focused == a.captureFocus {
		return // focus did not actually move
	}
	if fn := a.nodes[a.focused]; fn != nil && withinScope(fn, owner) {
		a.captureFocus = a.focused
		return
	}
	a.loseCapture(CaptureLostFocusChange)
}

// captureCheckScope ends a capture whose owner a newly active trapping scope
// excludes, so an opening dialog cannot leave a drag running behind it.
func (a *App) captureCheckScope() {
	owner := a.nodes[a.captureOwner]
	if owner == nil {
		return
	}
	if scope := a.confinement(); scope != nil && !withinScope(owner, scope) {
		a.loseCapture(CaptureLostFocusScope)
	}
}

// captureCheckVisible ends a capture whose owner is no longer laid out and on
// screen, since there is nothing left for the user to be dragging.
func (a *App) captureCheckVisible() {
	owner := a.nodes[a.captureOwner]
	if owner == nil {
		return
	}
	if !owner.visible() {
		a.loseCapture(CaptureLostHidden)
	}
}
