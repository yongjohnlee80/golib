package tui

import (
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// Event routing — target-then-bubble, no capture phase: the runtime
// resolves a single target node per routed event, calls its HandleEvent,
// and — while handlers return false — walks parent links to the root. The
// first true consumes the event and stops the walk.

// dispatch routes one event on the loop goroutine. Both lanes funnel through
// it, so input and program events are ordered against each other by the order
// they are drained rather than by which produced them.
func (a *App) dispatch(ev Event) {
	switch e := ev.(type) {
	case KeyEvent:
		// Target = the focused node; none → root.
		target := a.nodes[a.focused]
		if target == nil {
			target = a.rootNode
		}
		if target != nil && a.bubble(target, ev) {
			return
		}
		// Unconsumed key at the root falls through to the App's global keymap —
		// which is how framework Tab traversal works: a component that consumes
		// Tab (e.g. a text area inserting \t) thereby opts out of traversal for
		// that press.
		a.globalKey(e)

	case PasteEvent:
		target := a.nodes[a.focused]
		if target == nil {
			target = a.rootNode
		}
		if target != nil {
			a.bubble(target, ev)
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
		// Target by hit-testing laid-out absolute rects, topmost first (reverse
		// paint order — Stack z-order); coordinates are rewritten LOCAL to each
		// receiving node at every hop.
		target := a.hitTest(e.X, e.Y)
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
		for n := target; n != nil; n = n.parent {
			local := e
			local.X = e.X - n.absRect.X
			local.Y = e.Y - n.absRect.Y
			if n.comp.HandleEvent(local) {
				break
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
// framework-owned Tab / Shift-Tab traversal.
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
// it. Returns whether anything consumed.
func (a *App) bubble(n *node, ev Event) bool {
	start := n
	for ; n != nil; n = n.parent {
		if n.comp.HandleEvent(ev) {
			a.traceRouted(ev, start, n.id)
			return true
		}
	}
	a.traceRouted(ev, start, 0)
	return false
}

// traceRouted records which node consumed a key (0 = nobody). Only keys:
// mouse and paste traffic would drown the trace without adding much.
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
// TaskProgress) directly to its owner — no bubbling: these are private
// deliveries; propagating them to ancestors would leak implementation
// detail. An unmounted owner dead-letters task traffic (drop, count,
// log); a stale tick is silently done.
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
	n.comp.HandleEvent(ev) // unconsumed = silently done
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
// point, descending into children in reverse paint order so the topmost
// Stack layer wins the mouse.
func (a *App) hitTest(x, y int) *node {
	if a.rootNode == nil {
		return nil
	}
	return hitTestNode(a.rootNode, x, y)
}

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

// pressOrdinal returns the ordinal of this press: 1 for a single press, 2 for the
// second press of a double-click, and so on.
//
// A press continues the run only when the button, the CELL and the window all
// match. Same cell rather than "near": a terminal row is one cell tall, so a
// one-cell drift is a different row, and tolerating it would activate a row the
// user did not click. A release between the two presses is normal and does not
// interrupt the run; a press of a different button, or on another cell, restarts
// it at 1.
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
