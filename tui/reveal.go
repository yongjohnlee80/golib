package tui

// REVEAL — bringing the focused node into view inside the scrolling containers around it.
// The runtime owns it, not each container: a focus change records one request, and after each settled layout the runtime asks the innermost Revealer around the focused
// node to show it, measured from that layout. A Revealer that moves causes another pass, which
// serves the next one out from the new placements, so nested scrolling views converge instead of
// each moving on the others' old geometry. When none moves, the request is cleared: a wheel
// scroll afterwards is the user's, and is never pulled back.

// Revealer is a component that scrolls its content: Reveal moves its view the least that shows r
// (the focused node's rect, in the Revealer's own coordinates, from the last layout) and reports
// whether it moved. The runtime lays out again when it did.
type Revealer interface {
	Component
	Reveal(r Rect) (moved bool)
}

// maxRevealSteps bounds the reveal steps one frame serves, each a Revealer that moved and the
// layout that follows it: a deep nesting is served over several frames, never in one frame's work
// without end.
const maxRevealSteps = 6

// requestReveal records a reveal for n, newly focused, when a Revealer is around it, and asks for
// the layout that serves it. Focus that no Revealer contains costs nothing.
func (a *App) requestReveal(n *node) {
	for p := n.parent; p != nil; p = p.parent {
		if _, ok := p.comp.(Revealer); ok {
			a.reveal = true
			a.layoutDirty = true
			a.queue.wakeUp()
			return
		}
	}
}

// revealStep asks the innermost Revealer around the focused node that has to move to show it, and
// reports whether one moved. Nothing to show (focus gone, the node unplaced or removed), or no
// Revealer that moves, clears the request.
func (a *App) revealStep() bool {
	n := a.nodes[a.focused]
	if n == nil || !n.mounted || !n.visible() {
		a.reveal = false
		return false
	}
	for p := n.parent; p != nil; p = p.parent {
		rv, ok := p.comp.(Revealer)
		if !ok || !p.visible() {
			continue
		}
		r := Rect{X: n.absRect.X - p.absRect.X, Y: n.absRect.Y - p.absRect.Y, W: n.absRect.W, H: n.absRect.H}
		if rv.Reveal(r) {
			return true
		}
	}
	a.reveal = false
	return false
}
