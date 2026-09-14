package tui

import (
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// Event Routing Pipeline
//
// The TUI event system operates on a single-threaded "target-then-bubble"
// paradigm (no capture phase). All external terminal input (Lane A: keys,
// mouse, paste, window resize, terminal focus) and internal asynchronous
// worker/timer/program events (Lane B: Post, Go TaskResult, TaskProgress,
// TickEvent) funnel sequentially into dispatch() on the loop goroutine.
//
//               ┌─────────────────────────────────────────┐
//               │       Lane A: Terminal Input            │
//               │   (Keys, Mouse, Paste, Focus, Resize)   │
//               └────────────────────┬────────────────────┘
//                                    │
//               ┌────────────────────┴────────────────────┐
//               │       Lane B: Program & Async           │
//               │   (Post, TaskResult, Progress, Tick)    │
//               └────────────────────┬────────────────────┘
//                                    ▼
//                      App.dispatch(ev Event)
//                                    │
//        ┌───────────────────────────┼───────────────────────────┐
//        ▼                           ▼                           ▼
//   [Key / Paste]              [Mouse Event]            [Addressed / System]
//        │                           │                           │
//   Target: Focused            1. Hit-Test (Topmost)             ├─ ResizeEvent:
//   (Fallback: Root)              Reverse paint order;           │    Update size,
//        │                        Z-order Stack topmost          │    dirty layout+render,
//   Bubble Up:                    wins pointer hit.              │    Publish to Bus.
//   Target -> Parent -> ...          │                           │
//        │                     2. Primary Left Press:            ├─ FocusEvent:
//        ├─ Consumed: Return      Focus candidate pane           │    Bubble to focused,
//        │                        (focusFromPointer).            │    Publish to Bus.
//        └─ Unconsumed Key:       If focus unmounts/redirects    │
//           Fallback to           target, press is skipped!      ├─ TickEvent /
//           App.globalKey(e)         │                           │  TaskResult /
//           (Tab traversal)   3. Ordinal Commit:                 │  TaskProgress:
//                                 pressOrdinal (1=single,        │    Direct delivery
//                                 2=double, 3=triple click).     │    to Owner node.
//                                    │                           │    NO BUBBLING.
//                             4. Bubble & Local Coords:          │    Dead-letter if
//                                 Rewrite (X, Y) relative        │    unmounted.
//                                 to n.absRect at each hop.      │
//                                 Target -> Parent -> ...        │
//
// Core Invariants:
//  1. Single-Threaded Dispatch: dispatch() runs strictly on the main loop
//     goroutine. Handlers never need internal mutexes to guard component
//     state during HandleEvent.
//  2. Topmost Hit-Testing: Mouse clicks are evaluated against laid-out
//     absolute rectangles (n.absRect) in reverse paint order (last child
//     first). Stack overlays and modal dialogs reliably intercept clicks.
//  3. Coordinate Sandboxing: Mouse coordinates are rewritten relative to
//     n.absRect at every hop during bubble traversal. A component receives
//     (0,0) when clicked at its top-left corner regardless of screen position.
//  4. Pointer Focus Precedes Delivery: Clicking a pane moves focus into that
//     pane before the MousePress is delivered. If focus change unmounts or
//     redirects the target, delivery of the press is aborted to prevent
//     phantom clicks on unmeasured or inactive nodes.
//  5. Isolated Addressed Deliveries: Tasks (TaskResult, TaskProgress) and
//     Ticks are private to the owning node. They never bubble to ancestors,
//     preventing implementation leaks and accidental child-task interception.

// dispatch routes one event on the loop goroutine. Both lanes funnel through
// it, so input and program events are ordered against each other by the order
// they are drained rather than by which produced them.
func (a *App) dispatch(ev Event) {
	switch e := ev.(type) {
	case KeyEvent:
		// Target = the focused node. With nothing focused the fallback is the
		// active trapping scope if there is one, and only otherwise the root:
		// repairFocus legitimately leaves focused == 0 while a trap survives
		// with no focusable descendant (see focus.go), and defaulting to root
		// there would hand the key to the controls the trap is covering.
		limit := a.confinement()
		target := a.confinedTarget(a.nodes[a.focused], limit)
		if target != nil && a.bubbleWithin(target, limit, ev) {
			return
		}
		// Unconsumed key falls through to the App's global keymap — which is how
		// framework Tab traversal works: a component that consumes Tab (e.g. a
		// text area inserting \t) thereby opts out of traversal for that press.
		//
		// globalKey stays reachable INSIDE a trap on purpose. It is the only
		// implementation of Tab/Shift-Tab, so skipping it would leave a generic
		// scope with no traversal at all; and it is safe, because focusStep
		// walks the ring of the current scope and cannot move focus out of one.
		// Every other unconsumed key is simply dropped at the ceiling.
		a.globalKey(e)

	case PasteEvent:
		limit := a.confinement()
		if target := a.confinedTarget(a.nodes[a.focused], limit); target != nil {
			a.bubbleWithin(target, limit, ev)
		}

	case UserEvent:
		// Consumer-defined input routes exactly like a key: to the focused
		// node, bubbling within the active scope. It is input, so it is
		// confined by a trap for the same reason a keystroke is — a gamepad
		// must not reach the controls a dialog is covering. Anything else
		// would make UserEvent a way around the scope rules.
		limit := a.confinement()
		if target := a.confinedTarget(a.nodes[a.focused], limit); target != nil {
			a.bubbleWithin(target, limit, ev)
		}

	case MouseEvent:
		// A count belongs to presses only. Canonicalise every other kind to zero
		// rather than passing a producer's value through. Count is documented
		// as 0 on every non-press kind, and rewriting only presses left that
		// promise dependent on the producer: an injected MouseWheel{Count: 99}
		// was delivered with 99 intact.
		if e.Kind != MousePress {
			e.Count = 0
			ev = e
		}
		// A HELD CAPTURE PRE-EMPTS EVERYTHING BELOW. Press, motion and release
		// go straight to the owner: no hit-test, no focus step, no bubbling.
		// That is the whole point — the pointer has left the widget that owns
		// the drag, so every mechanism that asks "what is under the pointer?"
		// now answers with the wrong widget.
		//
		// The wheel is deliberately NOT captured. Scrolling addresses the pane
		// under the pointer without taking focus, and capturing it would freeze
		// scrolling for the duration of a drag.
		if a.captureOwner != 0 && e.Kind != MouseWheel {
			if a.deliverCaptured(e) {
				return
			}
			// The owner vanished between acquisition and this event. Fall
			// through and route normally rather than dropping the event.
		}
		// Target by hit-testing laid-out absolute rects, topmost first (reverse
		// paint order — Stack z-order); coordinates are rewritten LOCAL to each
		// receiving node at every hop.
		//
		// While a scope traps, the search is ROOTED AT THAT SCOPE, so a position
		// outside its subtree resolves to no target and the event is dropped.
		// Refusing the focus change is not enough on its own: focusFromPointer
		// already declines to move focus outside the trap, but the press was
		// still being delivered to whatever sat under the pointer.
		//
		// Dropping rather than retargeting to the scope is deliberate. A click on
		// the dimmed area behind a dialog means "nothing"; synthesising a
		// delivery to the dialog would invent an interaction the user did not
		// make.
		limit := a.confinement()
		var target *node
		if limit != nil {
			target = hitTestNode(limit, e.X, e.Y)
		} else {
			target = a.hitTest(e.X, e.Y)
		}
		if target == nil && limit != nil {
			a.trace(TraceEvent{Kind: TraceScope, Node: limit.id,
				Detail: "pointer dropped: outside the active focus scope"})
			return
		}
		// A PRIMARY PRESS focuses before it is delivered: one gesture both moves
		// focus into the clicked pane and acts on it. Motion, wheel and release
		// deliberately do not, so scrolling over an unfocused pane never steals
		// the keyboard.
		if target != nil && e.Kind == MousePress && e.Button == MouseLeft {
			focused := a.focusFromPointer(target)
			// Focus handlers run arbitrary component code synchronously and may
			// unmount the very node this press was addressed to. A replacement
			// mounted during dispatch has measured=false/placed=false and is not
			// hit-testable until the next layout pass, so there is nothing
			// correct to re-target: the press is SKIPPED. The focus change
			// stands, and the user's next click lands on the rebuilt tree.
			if !target.mounted {
				a.trace(TraceEvent{Kind: TraceUnmount, Node: target.id,
					Detail: "pointer press skipped: focus handling unmounted the target"})
				return
			}
			// A handler can also REDIRECT focus while leaving the target mounted.
			// The target would then receive the press unfocused, which is the very
			// thing focus-before-delivery exists to prevent, so mounted-ness alone
			// is not enough: the candidate must still own focus.
			if focused != nil && a.focused != focused.id {
				a.trace(TraceEvent{Kind: TraceFocus, Node: a.focused, Prev: focused.id,
					Detail: "pointer press skipped: focus was redirected away from the target"})
				return
			}
		}
		// The press ORDINAL is committed HERE — after hit-testing, after the focus
		// step, and immediately before delivery — not on arrival.
		//
		// Committing on arrival made a SKIPPED press advance the run: the two
		// early returns above deliver to nobody, yet the run continued, so the
		// widget that replaced an unmounted target saw Count == 2 as its FIRST
		// delivered press. Count drives activation, so a press nobody received
		// must not count.
		//
		// Continuity is keyed on the DELIVERED TARGET as well as button, cell and
		// window: a press landing on a different node is a different gesture even
		// at the same coordinates, which happens whenever the tree under the
		// pointer changes between clicks.
		if e.Kind == MousePress {
			e.Count = a.pressOrdinal(e, target)
			ev = e
		}
		consumed := false
		for n := target; n != nil; n = n.parent {
			local := e
			local.X = e.X - n.absRect.X
			local.Y = e.Y - n.absRect.Y
			if a.routeToNode(n, local) {
				consumed = true
				break
			}
			if n == limit {
				break // same ceiling as the keyboard path
			}
		}
		// STEP 6 — the gesture recogniser, last and only on what nobody wanted.
		//
		// It sees a primary press ONLY after every node on the path has declined
		// it, so a widget that handles its own presses is never second-guessed;
		// the recogniser exists for the ones that would rather describe what
		// they do than when they were clicked.
		if !consumed && target != nil && e.Kind == MousePress && e.Button == MouseLeft {
			// An OPTED-IN target, which means two things: it implements
			// Activatable, and its effective pointer policy allows the mouse.
			//
			// The Activatable half is not decoration. A gesture whose only
			// possible outcome is an activation is meaningless on a component
			// that cannot be activated, and engaging anyway is actively
			// harmful: the recogniser takes a capture on the press, and that
			// capture then pre-empts routing for every pointer event until the
			// release. Without this condition a plain container swallowed the
			// second click of a double-click and a press aimed into a nested
			// trap never arrived — two existing tests caught exactly that.
			// Also skipped when the control says it cannot currently be
			// activated. Without this a disabled button still starts a gesture
			// and holds the pointer until release: it never arms and never
			// activates, so it looks right, but it silently swallows every
			// pointer event in between — the same harm as engaging on a
			// component that is not Activatable at all.
			if _, activatable := target.comp.(Activatable); activatable &&
				activationAvailable(target) {
				if r := a.recognizerFor(); r != nil &&
					effectivePointerPolicy(target) != PointerDisabled {
					local := e
					local.X = e.X - target.absRect.X
					local.Y = e.Y - target.absRect.Y
					a.runRecognizer(r, target, local)
				}
			}
		}

	case ResizeEvent:
		// Not routed through the tree: update root constraints, mark layout dirt
		// + full render dirt (never diff across a size change), publish on the
		// Bus for components that care about raw dimensions.
		a.size = Size{W: e.W, H: e.H}
		a.layoutDirty = true
		a.renderDirty = true
		a.bus.Publish(e)
		a.queue.wakeUp()

	case FocusEvent:
		// Terminal focus in/out (mode 1004): delivered to the focused component
		// and published on the Bus. Component focus changes do not pass through
		// dispatch — setFocus bubbles them directly.
		//
		// The TERMINAL WINDOW losing focus ends any capture. No further motion
		// or release is coming while another window has the pointer, so the
		// owner would otherwise be left mid-drag with no event able to finish
		// it.
		//
		// Terminal is required, not just Gained==false. FocusEvent carries both
		// component focus and terminal focus, and only the terminal kind means
		// input has stopped arriving; treating a component focus-loss as a
		// backend loss would cancel drags for a reason that never happened and
		// report the wrong one.
		if e.Terminal && !e.Gained {
			a.loseCapture(CaptureLostBackend)
		}
		if n := a.nodes[a.focused]; n != nil {
			a.bubble(n, ev)
		}
		a.bus.Publish(e)

	case TickEvent:
		a.deliverAddressed(e.Owner, ev)
	case TaskResult:
		a.deliverAddressed(e.Owner, ev)
	case TaskProgress:
		a.deliverAddressed(e.Owner, ev)
	}
}

// globalKey is the App-level fallback for keys no component consumed:
// framework-owned Tab / Shift-Tab focus traversal.
//
// Key releases and modified tabs (other than Shift) are ignored. If Shift is
// held, focus steps backward (-1); otherwise forward (+1). Components that
// consume Tab (e.g. text editors inserting \t) return true from HandleEvent,
// naturally opting out of global focus stepping.
func (a *App) globalKey(e KeyEvent) {
	if e.Kind == KeyRelease {
		return
	}
	if e.Code != KeyTab || e.Mods&^ModShift != 0 {
		return
	}
	if e.Mods&ModShift != 0 {
		a.focusStep(-1)
		return
	}
	a.focusStep(1)
}

// bubble walks n's ancestor chain delivering ev until a handler consumes
// it (returns true). Returns whether any handler consumed the event.
//
// If an event bubbles all the way to the root without being consumed,
// bubble returns false, allowing callers to apply fallbacks (such as globalKey).
// It delivers RAW only, running no resolvers. Its callers are notifications —
// the runtime telling components that focus moved — rather than input, and an
// action names what the USER meant. Resolving a focus notification into an
// intent would invent one nobody expressed, and would also consult every
// resolver on the path each time focus changed.
func (a *App) bubble(n *node, ev Event) bool {
	start := n
	for ; n != nil; n = n.parent {
		if a.deliverTo(n, ev) {
			a.traceRouted(ev, start, n.id)
			return true
		}
	}
	a.traceRouted(ev, start, 0)
	return false
}

// bubbleWithin is bubble with a ceiling: it delivers from n upward and STOPS
// after limit, which is delivered to and then not passed. A nil limit means no
// ceiling, which is ordinary bubble.
//
// The ceiling exists because a trapping focus scope has to confine input, and
// choosing a different node to start from cannot do that — bubbling walks to
// the root from wherever it begins. Without a ceiling, a key delivered inside
// an open dialog reaches the controls behind it on the very next hop.
//
// Delivery order and the consumed/unconsumed result are otherwise identical to
// bubble, so an unconsumed event still lets the caller apply a fallback.
func (a *App) bubbleWithin(n, limit *node, ev Event) bool {
	start := n
	for ; n != nil; n = n.parent {
		if a.routeToNode(n, ev) {
			a.traceRouted(ev, start, n.id)
			return true
		}
		if n == limit {
			break // limit is delivered to, then bubbling stops
		}
	}
	a.traceRouted(ev, start, 0)
	return false
}

// routeToNode runs the per-node input sequence for ONE node and reports whether
// that node consumed the event. It is the whole of what any single node does
// with an event; the bubble loop above just walks it up the tree.
//
//  1. POLICY GATE — pointer input to a node whose effective policy is disabled
//     skips the node ENTIRELY, semantic and raw alike, and carries on to the
//     parent. Gating only one path would half-disable composite widgets.
//  2. RESOLVE      — first match over consumer resolvers, then defaults.
//  3. SEMANTIC     — dispatch the resolved action.
//  4. RAW          — if no action was produced, or the one produced went
//     unhandled, deliver the raw event to the same node.
//
// SEMANTIC COMES FIRST, and that ordering is forced rather than chosen. With
// raw first, a widget that reads MouseEvent directly would swallow the press
// that its own resolver was meant to turn into a begin-drag action, so mouse
// and keyboard would take different paths through the same widget — which is
// the duplication actions exist to remove.
//
// An unhandled action falls through to raw on the SAME node, but does NOT try
// later resolvers: first match wins, so a resolver claiming an event and then
// producing something the node ignores does not hand the event to the next
// resolver in line.
func (a *App) routeToNode(n *node, ev Event) bool {
	if pointerDerived(ev) && effectivePointerPolicy(n) == PointerDisabled {
		return false
	}
	if act, ok := a.resolveFor(n, ev); ok {
		if a.dispatchAction(n, ActionInvocation{
			Action: act,
			Origin: originFor(ev),
			Source: ev,
		}) {
			return true
		}
	}
	return a.deliverTo(n, ev)
}

// confinement reports the ceiling for keyboard and pointer routing: the
// innermost trapping focus scope when one is active, else nil for "no ceiling".
//
// currentScope returns the root when nothing traps, and the root is not a
// ceiling — every event may legally reach it — so that case is normalised to
// nil here rather than at each call site.
func (a *App) confinement() *node {
	s := a.currentScope()
	if s == nil || s == a.rootNode {
		return nil
	}
	return s
}

// confinedTarget picks where a focus-routed event (key, paste) starts.
//
// It is the defensive backstop for the confinement rule: with a live trap,
// an event NEVER
// starts outside it, even if focus is somehow already outside. bubbleWithin
// only stops when it meets the ceiling, so a walk that begins outside the
// subtree can never meet it and would run to the root — the confinement would
// silently evaporate exactly when a focus transition had gone wrong.
//
// Without a trap this is just "the focused node, else the root", unchanged.
func (a *App) confinedTarget(focused, limit *node) *node {
	if limit == nil {
		if focused != nil {
			return focused
		}
		return a.rootNode
	}
	if focused == nil || !withinScope(focused, limit) {
		return limit
	}
	return focused
}

// traceRouted records which node consumed a key (0 = nobody).
// Only key presses are traced: mouse motion/press and paste traffic would
// overwhelm the ring buffer without providing actionable debugging insight.
func (a *App) traceRouted(ev Event, from *node, consumer NodeID) {
	if !a.tracing() {
		return
	}
	k, ok := ev.(KeyEvent)
	if !ok || k.Kind == KeyRelease {
		return
	}
	fromID := NodeID(0)
	if from != nil {
		fromID = from.id
	}
	a.trace(TraceEvent{Kind: TraceKey, Node: consumer, Prev: fromID,
		Detail: k.describe()})
}

// deliverAddressed hands an addressed event (TickEvent / TaskResult /
// TaskProgress) directly to its owner node — no bubbling: these are private
// deliveries targeted specifically to the node that initiated them.
// Propagating them to ancestors would violate encapsulation and leak child
// implementation details.
//
// If the owner has unmounted before delivery:
//   - TaskResult / TaskProgress: dead-lettered (dropped, counted in
//     a.async.deadLetters, and logged as a warning).
//   - TickEvent: silently dropped as ticks are idempotent and time-bound.
func (a *App) deliverAddressed(owner NodeID, ev Event) {
	n := a.nodes[owner]
	if n == nil {
		switch ev.(type) {
		case TaskResult, TaskProgress:
			a.async.deadLetters.Add(1)
			logger.Warning(a.cfg.logger, nil, map[string]any{
				"tui": "dead-lettered addressed event", "event": typeNameAddressed(ev),
				"owner": uint64(owner),
			})
		}
		return
	}
	a.deliverTo(n, ev) // unconsumed = silently done
}

// deliverCaptured hands one pointer event to the node holding the capture and
// reports whether it was able to. A false result means the capture is stale —
// the owner is gone from the tree — and the caller should route normally.
//
// Coordinates are made local to the owner and are NOT clamped to it. Negative
// and past-the-edge values are the expected case, not a fault: a drag that has
// travelled off the widget is precisely what capture exists to keep delivering,
// and clamping would report the pointer as parked on the border instead of
// where it actually is.
//
// Nothing bubbles. An ancestor did not ask for this gesture and cannot tell it
// apart from one of its own.
//
// The owner runs the SAME per-node sequence an uncaptured event runs — policy
// gate, resolve, semantic dispatch, then raw — just without the parent walk. A
// drag begun as a semantic action therefore continues as one; delivering
// captured motion straight to HandleEvent would have forced every widget to
// keep a second state machine for the captured half of its own gesture.
//
// There is deliberately NO policy check here. A capture and a disabled policy
// can meet in exactly two ways, and both are handled where they happen: the
// policy changing under a live capture cancels it at the setter, and a node
// whose pointer input is disabled is refused the capture in the first place.
// A third check here would be one nothing can reach, which is worse than
// absent — it reads as a guarded case and can never be shown to work.
func (a *App) deliverCaptured(e MouseEvent) bool {
	owner := a.nodes[a.captureOwner]
	if owner == nil || !owner.mounted {
		return false
	}
	// A GESTURE capture belongs to the recogniser, which took it and is the
	// only thing that can finish it. Routing these to the owner instead is the
	// exact mistake that stranded an earlier design: the recogniser captures on
	// the press, so sending its own motion and release past it left it unable
	// to ever re-arm, abandon or activate.
	if a.captureKind == CaptureGesture {
		if r := a.recognizerFor(); r != nil {
			local := e
			local.X = e.X - owner.absRect.X
			local.Y = e.Y - owner.absRect.Y
			a.runRecognizer(r, owner, local)
			a.traceRouted(e, owner, owner.id)
			return true
		}
		// The recogniser was removed mid-gesture. Nothing can complete it, so
		// end it rather than leave the target armed forever.
		a.endGesture()
		return false
	}
	// The press ordinal is still committed here, for the same reason it is on
	// the uncaptured path: Count drives activation, and a captured press is a
	// press the owner really did receive.
	if e.Kind == MousePress {
		e.Count = a.pressOrdinal(e, owner)
	}
	local := e
	local.X = e.X - owner.absRect.X
	local.Y = e.Y - owner.absRect.Y
	a.routeToNode(owner, local)
	a.traceRouted(e, owner, owner.id)
	return true
}

// deliverTo is the ONE place a component's HandleEvent is entered, so that the
// "a handler is running" phase is exactly the set of moments a component can
// observe and not one instant wider.
//
// Nesting is real and must not clear the flag early: a handler can post work,
// change focus, or unmount a subtree, any of which can deliver a further event
// synchronously. Restoring the previous value rather than clearing outright
// keeps the flag true for the remainder of the outer handler.
func (a *App) deliverTo(n *node, ev Event) bool {
	prev, prevAction := a.handlerNode, a.actionHandlerNode
	a.handlerNode = n.id
	// Raw delivery is NOT an action delivery, and says so. The runtime reaches
	// here from inside an action handler on the same node whenever an action
	// goes unhandled, and leaving the action marker standing would let this raw
	// handler forward a child action carrying the outer invocation's
	// provenance — a real origin, belonging to an event the child never saw.
	a.actionHandlerNode = 0
	defer func() { a.handlerNode, a.actionHandlerNode = prev, prevAction }()
	return n.comp.HandleEvent(ev)
}

// typeNameAddressed names addressed events for the dead-letter log line.
func typeNameAddressed(ev Event) string {
	switch ev.(type) {
	case TaskResult:
		return "TaskResult"
	case TaskProgress:
		return "TaskProgress"
	default:
		return "TickEvent"
	}
}

// hitTest finds the deepest visible node whose absolute Rect contains the
// point (x, y). Traversal descends into children in reverse paint order
// (last child first) so that topmost layers (e.g. Stack overlays, popup menus,
// floating modals) win pointer events over occluded siblings.
func (a *App) hitTest(x, y int) *node {
	if a.rootNode == nil {
		return nil
	}
	return hitTestNode(a.rootNode, x, y)
}

// hitTestNode recursively inspects n and its visible children for containment.
func hitTestNode(n *node, x, y int) *node {
	if !n.visible() || !n.absRect.Contains(x, y) {
		return nil
	}
	for i := len(n.children) - 1; i >= 0; i-- {
		if t := hitTestNode(n.children[i], x, y); t != nil {
			return t
		}
	}
	return n
}

// pressOrdinal returns the ordinal count of this press: 1 for a single press,
// 2 for the second press of a double-click, 3 for triple-click, etc.
//
// A press continues the run only when ALL four criteria match:
//  1. Button matches the previous press.
//  2. Exact cell coordinates (X, Y) match (terminal cells are 1-cell high; drift is disallowed).
//  3. Target NodeID matches (clicking a different widget at the same position starts a new run).
//  4. Elapsed time since the prior press is within doubleClickWindow.
//
// A MouseRelease between presses is expected and does not interrupt the run.
// Any mismatch resets the ordinal count back to 1.
func (a *App) pressOrdinal(e MouseEvent, target *node) int {
	window := a.cfg.doubleClickWindow
	now := time.Now()
	var id NodeID
	if target != nil {
		id = target.id
	}
	continues := window > 0 &&
		a.lastPressCount > 0 &&
		e.Button == a.lastPressButton &&
		e.X == a.lastPressX && e.Y == a.lastPressY &&
		id == a.lastPressTarget &&
		now.Sub(a.lastPressAt) <= window

	count := 1
	if continues {
		count = a.lastPressCount + 1
	}
	a.lastPressAt, a.lastPressX, a.lastPressY = now, e.X, e.Y
	a.lastPressButton, a.lastPressCount, a.lastPressTarget = e.Button, count, id
	return count
}
