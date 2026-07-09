package tui

import "github.com/yongjohnlee80/golib/logger"

// Event routing — target-then-bubble, no capture phase (ADR-0004 §2.5): the
// runtime resolves a single target node per routed event, calls its
// HandleEvent, and — while handlers return false — walks parent links to
// the root. The first true consumes the event and stops the walk.

// dispatch routes one event on the loop goroutine (ADR-0004 §2.5;
// ADR-0005's loop calls it for both lanes).
func (a *App) dispatch(ev Event) {
	switch e := ev.(type) {
	case KeyEvent:
		// Target = the focused node; none → root (ADR-0004 §2.5.1).
		target := a.nodes[a.focused]
		if target == nil {
			target = a.rootNode
		}
		if target != nil && a.bubble(target, ev) {
			return
		}
		// Unconsumed key at the root falls through to the App's global
		// keymap — which is how framework Tab traversal works: a
		// component that consumes Tab (e.g. a text area inserting \t)
		// thereby opts out of traversal for that press (ADR-0004 §2.6.2).
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
		// Target by hit-testing laid-out absolute rects, topmost first
		// (reverse paint order — Stack z-order); coordinates are rewritten
		// LOCAL to each receiving node at every hop (ADR-0004 §2.5.2).
		target := a.hitTest(e.X, e.Y)
		for n := target; n != nil; n = n.parent {
			local := e
			local.X = e.X - n.absRect.X
			local.Y = e.Y - n.absRect.Y
			if n.comp.HandleEvent(local) {
				break
			}
		}

	case ResizeEvent:
		// Not routed through the tree: update root constraints, mark
		// layout dirt + full render dirt (never diff across a size
		// change), publish on the Bus for components that care about raw
		// dimensions (ADR-0004 §2.5.5, §2.7.5).
		a.size = Size{W: e.W, H: e.H}
		a.layoutDirty = true
		a.renderDirty = true
		a.bus.Publish(e)
		a.queue.wakeUp()

	case FocusEvent:
		// Terminal focus in/out (mode 1004): delivered to the focused
		// component and published on the Bus (ADR-0005 §2.5). Component
		// focus changes do not pass through dispatch — setFocus bubbles
		// them directly.
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
// framework-owned Tab / Shift-Tab traversal (ADR-0004 §2.6.2).
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

// bubble walks n's ancestor chain delivering ev until a handler consumes it
// (ADR-0004 §2.5). Returns whether anything consumed.
func (a *App) bubble(n *node, ev Event) bool {
	for ; n != nil; n = n.parent {
		if n.comp.HandleEvent(ev) {
			return true
		}
	}
	return false
}

// deliverAddressed hands an addressed event (TickEvent / TaskResult /
// TaskProgress) directly to its owner — no bubbling: these are private
// deliveries; propagating them to ancestors would leak implementation
// detail (ADR-0004 §2.5.4). An unmounted owner dead-letters task traffic
// (drop, count, log — ADR-0005 §2.8.2); a stale tick is silently done.
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
// Stack layer wins the mouse (ADR-0004 §2.5.2, §2.7.4).
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
