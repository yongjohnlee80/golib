package tui

// THE GESTURE RECOGNISER.
//
// Press-arm, release-activate is the behaviour every clickable control shares:
// pressing arms it, releasing inside triggers it, releasing outside abandons
// it, and dragging out disarms without ending the gesture so that dragging back
// in re-arms. Written per widget that is a capture, four state transitions and
// a bounds test each time — and each widget ends up with a subtly different
// version, usually one that forgets the drag-out-and-back case users rely on to
// change their mind mid-click.
//
// The recogniser is that logic once, owned by the runtime. A component
// implements Activate and SetArmed and gets the whole behaviour, identically
// across the terminal, the web backend and TestBackend, without doing any
// coordinate arithmetic of its own.
//
// WHY CAPTURE OWNERSHIP IS TYPED. An earlier design sent every held-capture
// event straight to the owner — but the recogniser TAKES the capture on the
// press, so its own motion and release bypassed it forever and it could never
// re-arm, cancel or activate. Its own capture made completion impossible.
// CaptureGesture routes to the recogniser and CaptureRaw to the owner, so the
// two cannot be confused.

// GestureState is the state of the gesture in flight.
//
// The runtime holds it and passes it in and out of OnPointer, so one recogniser
// value serves every node in the tree without keeping per-node bookkeeping —
// and so a recogniser cannot accumulate state the runtime is unable to discard
// when the gesture dies.
type GestureState uint8

const (
	// GestureIdle: nothing in progress.
	GestureIdle GestureState = iota
	// GestureArmed: pressed, and would activate if released now.
	GestureArmed
)

// String names the state for traces and test failures.
func (g GestureState) String() string {
	switch g {
	case GestureIdle:
		return "idle"
	case GestureArmed:
		return "armed"
	}
	return "unknown"
}

// GestureCtx is everything a recogniser is given, and everything it is allowed
// to know. It never reaches into the tree, so it cannot depend on geometry or
// state that belongs to somebody else.
type GestureCtx struct {
	// Target is the node the gesture belongs to, fixed when the press started
	// it. A recogniser cannot move a gesture to a different node mid-flight.
	Target NodeID
	// Local is the pointer in target-local cells, unclamped: negative and
	// past-the-edge values are the normal case once the pointer has left.
	Local Point
	// Inside reports whether Local lies within the target's laid-out rect.
	//
	// The RUNTIME computes it. A local coordinate alone cannot answer the
	// question without the target's size, and handing the recogniser that size
	// — or letting it ask the tree — is the coupling this whole type exists to
	// avoid.
	Inside bool
	// State is the gesture state carried forward from the previous call.
	State GestureState
}

// GestureRecognizer interprets a stream of pointer events as a gesture.
//
// OnPointer returns the action to dispatch (nil for none), whether the event is
// consumed, and the next state. It never touches the tree, and it does not
// state provenance: the runtime wraps whatever it returns, so a recogniser
// cannot claim an activation came from somewhere it did not.
type GestureRecognizer interface {
	OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState)
}

// WithGestureRecognizer replaces the App's default gesture recogniser.
//
// Passing nil disables gesture recognition entirely, which is the honest way to
// say "this app interprets its own pointer input" — every press then falls
// through to ordinary routing.
//
// Installing one while a gesture is in flight CANCELS that gesture first: the
// capture is released and the target left disarmed. No gesture ever spans two
// recognisers, because the second one would be asked to finish a sequence whose
// beginning it never saw.
func WithGestureRecognizer(r GestureRecognizer) AppOption {
	return func(c *appConfig) {
		c.recognizer = r
		c.recognizerSet = true
	}
}

// pressActivateRecognizer is the default: press arms, release inside activates,
// release outside abandons, and leaving the bounds disarms WITHOUT ending the
// gesture so that coming back re-arms it.
//
// That last part is the behaviour users rely on to change their mind after
// pressing, and it is the piece hand-written implementations most often drop.
type pressActivateRecognizer struct{}

// OnPointer implements GestureRecognizer.
func (pressActivateRecognizer) OnPointer(ev MouseEvent, gc GestureCtx) (Action, bool, GestureState) {
	switch ev.Kind {
	case MousePress:
		// Only a primary press inside the target begins a gesture.
		if ev.Button != MouseLeft || !gc.Inside {
			return nil, false, GestureIdle
		}
		return nil, true, GestureArmed

	case MouseMotion:
		// No state guard here, deliberately. Motion reaches a recogniser ONLY
		// while a gesture holds the pointer — step 6 engages on presses alone —
		// so "the state is Idle" here means the pointer has been dragged out of
		// the control, not that there is no gesture. Guarding on it made a
		// disarmed gesture permanently unable to re-arm, which is the whole
		// drag-out-and-back behaviour.
		//
		// Armed simply follows the pointer: out disarms, back in re-arms, and
		// the gesture stays alive either way until the release.
		if gc.Inside {
			return nil, true, GestureArmed
		}
		return nil, true, GestureIdle

	case MouseRelease:
		if ev.Button != MouseLeft {
			return nil, false, gc.State
		}
		// Activation needs BOTH that it was armed and that the release landed
		// inside. Either one alone is a press the user abandoned.
		if gc.State == GestureArmed && gc.Inside {
			return ActivateAction{}, true, GestureIdle
		}
		return nil, true, GestureIdle
	}
	return nil, false, gc.State
}

// ControlActivatedEvent reports that an Activatable was successfully triggered.
// It is published on the Bus, so anything can observe activations without the
// activating widget needing to know who is listening.
//
// It is published only for an activation that actually happened: a refused
// Activate — disabled, unmounted — publishes nothing.
type ControlActivatedEvent struct {
	// Owner is the node that was activated.
	Owner NodeID
	// Origin is where the activation came from, as the runtime observed it.
	Origin ActionOrigin
}

// recognizerFor returns the App's recogniser, or nil when gesture recognition
// is switched off.
func (a *App) recognizerFor() GestureRecognizer { return a.cfg.recognizer }

// gestureCtxFor builds the recogniser's view of one event against a target.
//
// Inside is computed here, from the target's own laid-out rect, because it is
// the one question the recogniser cannot answer from a local coordinate alone.
func (a *App) gestureCtxFor(n *node, local MouseEvent) GestureCtx {
	return GestureCtx{
		Target: n.id,
		Local:  Point{X: local.X, Y: local.Y},
		Inside: local.X >= 0 && local.Y >= 0 &&
			local.X < n.rect.W && local.Y < n.rect.H,
		State: a.gestureState,
	}
}

// runRecognizer feeds one pointer event to the recogniser on behalf of n and
// applies everything it asks for: the armed transition, the capture, and any
// action it produced. It reports whether the event was consumed.
//
// The arming, the capture and the dispatch are applied HERE rather than by the
// recogniser, which is what keeps the recogniser a pure function of the event
// and keeps every way a gesture can end in one place.
func (a *App) runRecognizer(r GestureRecognizer, n *node, local MouseEvent) bool {
	act, consumed, next := r.OnPointer(local, a.gestureCtxFor(n, local))

	// A gesture that has begun holds the pointer, so the recogniser keeps
	// seeing motion and release after they leave the target. Taken here and
	// never by the recogniser: the runtime is what has to be able to release it
	// on every death path.
	if next == GestureArmed && a.captureOwner == 0 {
		a.setCapture(n.id, CaptureGesture)
	}
	a.gestureState = next

	// ARMED-NESS AND ALIVENESS ARE DIFFERENT THINGS, and conflating them was a
	// real bug: GestureIdle means "would not activate if released now", which
	// is exactly the state of a pointer that has been dragged OUT of the
	// control — the gesture is very much still running, and ending it there
	// made dragging back in impossible.
	//
	// A gesture is alive while its capture is held, and ends on the release
	// that completes or abandons it.
	if act != nil {
		// Dispatch BEFORE the disarm, so the widget observes arm, activate,
		// disarm in that order. Disarming first would make anything repainting
		// on the callbacks flash through an unpressed frame before firing.
		//
		// The runtime supplies provenance. A recogniser only ever produces the
		// action itself, so it cannot label a pointer gesture as anything else.
		a.dispatchAction(n, ActionInvocation{
			Action: act,
			Origin: OriginPointer,
			Source: local,
		})
		consumed = true
	}
	a.setGestureArmed(n, next == GestureArmed)

	if local.Kind == MouseRelease {
		a.endGesture()
	}
	return consumed
}

// setGestureArmed drives the target's armed look, and is the only thing that
// does.
//
// TRANSITIONS ONLY, and only for an Activatable target. Arming when already
// armed is a no-op, and a raw capture held by something that cannot be
// activated — a resize handle, a pane divider — is never called at all. The
// runtime tracks the armed flag itself rather than asking the component,
// because it is the runtime that has to guarantee the component ends up
// disarmed, and a guarantee needs a value to compare against.
func (a *App) setGestureArmed(n *node, armed bool) {
	if a.gestureArmed == armed {
		return
	}
	act, ok := n.comp.(Activatable)
	if !ok {
		return
	}
	act.SetArmed(armed)
	a.gestureArmed = armed
	a.trace(TraceEvent{Kind: TraceAction, Node: n.id,
		Detail: "armed: " + boolWord(armed)})
}

// endGesture returns the runtime to the no-gesture state, leaving the target
// disarmed and holding nothing.
//
// It is called from every way a gesture can finish — activation, an abandoned
// release, and every capture-loss path — so that "the target is left disarmed"
// is true by construction rather than by each caller remembering.
func (a *App) endGesture() {
	if owner := a.nodes[a.captureOwner]; owner != nil && a.captureKind == CaptureGesture {
		a.setGestureArmed(owner, false)
		a.clearCapture()
	}
	a.gestureState = GestureIdle
	a.gestureArmed = false
}

// gestureLost disarms a target whose gesture is ending through a capture loss.
// It runs BEFORE the capture is cleared, so the owner is still known.
func (a *App) gestureLost() {
	if a.captureKind != CaptureGesture {
		return // a raw capture owner was never armed by the runtime
	}
	if owner := a.nodes[a.captureOwner]; owner != nil {
		a.setGestureArmed(owner, false)
	}
	a.gestureState = GestureIdle
	a.gestureArmed = false
}

// boolWord renders an armed transition for the trace without pulling strconv in
// for one word.
func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
