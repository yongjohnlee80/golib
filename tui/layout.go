package tui

import (
	"fmt"

	"github.com/yongjohnlee80/golib/logger"
)

// Layout — Flutter's box protocol verbatim (ADR-0004 §2.7): constraints
// down, sizes up, parent positions. ONE pass, no measurement round-trips;
// v1 relayouts the whole visible tree whenever any layout dirt exists
// (relayout boundaries are N2's future optimization).

// layoutTree runs the single whole-tree pass: the root receives the
// terminal size as tight constraints; containers recurse via
// Context.LayoutChild/PlaceChild.
func (a *App) layoutTree() {
	root := a.rootNode
	if root == nil {
		return
	}
	a.inLayout = true
	defer func() {
		a.inLayout = false
		a.layingOut = nil
	}()

	resetLayoutFlags(root)
	sz := a.layoutComponent(root, Tight(a.size))
	root.rect = Rect{X: 0, Y: 0, W: sz.W, H: sz.H}
	root.placed = true
	computeAbs(root, 0, 0)
}

// layoutComponent invokes one component's Layout under cc, clamps the
// answer, and records any constraint violation: a Size outside the
// constraints is a component bug, but the framework clamps it so a
// misbehaving widget cannot corrupt sibling geometry — clamp-and-report
// (ADR-0004 §2.7.1 rev 1). TestBackend retains violations for
// ConstraintViolations()/FailOnViolations; production observes WithLogger.
func (a *App) layoutComponent(n *node, cc Constraints) Size {
	prev := a.layingOut
	a.layingOut = n
	got := n.comp.Layout(cc)
	a.layingOut = prev

	clamped := cc.Constrain(got)
	if clamped != got {
		v := ConstraintViolation{Node: n.id, Type: fmt.Sprintf("%T", n.comp), Got: got, C: cc}
		if tb, ok := a.backend.(*TestBackend); ok {
			tb.RecordConstraintViolation(v)
		}
		logger.Warning(a.cfg.logger, nil, map[string]any{
			"tui": "constraint violation (clamped)", "node": uint64(v.Node),
			"type": v.Type, "got": fmt.Sprintf("%+v", v.Got), "constraints": fmt.Sprintf("%+v", v.C),
		})
	}
	n.size = clamped
	n.measured = true
	return clamped
}

// resetLayoutFlags clears the per-pass flags over the subtree: nodes a
// container skips this pass stay invisible (not rendered, not hit-testable,
// not tab stops).
func resetLayoutFlags(n *node) {
	n.measured = false
	n.placed = false
	for _, ch := range n.children {
		resetLayoutFlags(ch)
	}
}

// computeAbs derives every visible node's absolute Rect from the placed
// parent-relative rects — the table mouse hit-testing and the cursor rule
// look up (ADR-0004 §2.5.2, §2.3).
func computeAbs(n *node, ox, oy int) {
	n.absRect = Rect{X: ox + n.rect.X, Y: oy + n.rect.Y, W: n.rect.W, H: n.rect.H}
	for _, ch := range n.children {
		if ch.visible() {
			computeAbs(ch, n.absRect.X, n.absRect.Y)
		}
	}
}

// renderTree paints the visible tree depth-first in document (paint) order:
// each component renders its own chrome; the framework hands every child
// its own sub-Surface (ADR-0004 §2.1 Render contract).
func (a *App) renderTree() {
	root := a.rootNode
	if root == nil {
		return
	}
	a.inRender = true
	defer func() { a.inRender = false }()
	a.renderNode(root, newRootSurface(a.buf, a.rctx))
}

func (a *App) renderNode(n *node, s Surface) {
	n.comp.Render(s)
	for _, ch := range n.children {
		if ch.visible() {
			a.renderNode(ch, s.Sub(ch.rect))
		}
	}
}
