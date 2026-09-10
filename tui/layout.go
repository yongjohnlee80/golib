package tui

import (
	"fmt"

	"github.com/yongjohnlee80/golib/logger"
)

// Layout implements the Flutter box protocol verbatim: constraints down,
// sizes up, parent positions.
//
// # Layout Protocol & Geometry Flow
//
// The framework executes layout in a single whole-tree pass with zero measurement
// round-trips:
//
//	                 ┌─────────────────────────────┐
//	                 │     Parent Container        │
//	                 └──────────────┬──────────────┘
//	                                │
//	       1. Constraints Down      │  3. PlaceChild(child, rect)
//	       (MinW, MaxW, MinH, MaxH) │  (Parent positions child)
//	                                ▼
//	                 ┌─────────────────────────────┐
//	                 │        Child Widget         │
//	                 └──────────────┬──────────────┘
//	                                │
//	                                │ 2. Size Up (W, H)
//	                                │ (Child reports chosen dimensions)
//	                                ▼
//
// 1. Constraints Down: The parent computes allowed bounds (e.g. Tight or Loose)
//    and calls ctx.LayoutChild(child, constraints).
// 2. Sizes Up: The child computes its layout and returns its chosen Size within
//    those bounds.
// 3. Parent Positions: The parent assigns the child's final parent-relative Rect
//    via ctx.PlaceChild(child, rect).
//
// Whole-tree relayout occurs whenever layout dirt exists (a node calls ctx.RequestLayout
// or the terminal window resizes). Every node placed in the pass is flagged as placed
// and measured, which drives hit-testing visibility and tab traversal rings.

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

// layoutComponent invokes one component's Layout under cc, clamps the returned
// Size, and records any constraint violation.
//
// Clamp-and-Report Invariant:
// Returning a Size outside constraints is a component bug. However, the framework
// clamps the result so a misbehaving child cannot corrupt sibling geometry or cause
// buffer overflows. TestBackend retains recorded violations for test assertions
// (via tb.ConstraintViolations() and tb.FailOnViolations()), while production runs
// log them via WithLogger.
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
// look up.
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
// its own sub-Surface, so a child cannot paint outside the rect it was given.
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
