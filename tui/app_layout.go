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
	computeAbs(root, 0, 0, Rect{W: a.size.W, H: a.size.H})
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
	if hidden(n.comp) {
		// A hidden component takes the least its constraints allow — none,
		// under loose ones — and its subtree is not laid out, so nothing in
		// it is visible this frame (visibility.go).
		n.size = cc.Constrain(Size{})
		n.measured = true
		return n.size
	}
	prev := a.layingOut
	a.layingOut = n
	got := n.comp.Layout(cc)
	a.layingOut = prev

	clamped := cc.Constrain(got)
	if clamped != got {
		v := ConstraintViolation{Node: n.id, Type: fmt.Sprintf("%T", n.comp), Got: got, C: cc}
		if r, ok := a.backend.(ViolationRecorder); ok {
			r.RecordConstraintViolation(v)
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
//
// Declared anchor regions are per-pass too, and for the same reason. A region
// names a sub-area of THIS frame's layout — a menu row at a particular offset —
// so one that is not redeclared no longer exists, and keeping it would let a
// popup stay anchored to a row that has scrolled away or been removed from the
// model. Dropping them here is what turns "absent from this layout" into the
// anchor loss the host commits on.
func resetLayoutFlags(n *node) {
	n.measured = false
	n.placed = false
	n.regions = nil
	for _, ch := range n.children {
		resetLayoutFlags(ch)
	}
}

// computeAbs derives every visible node's absolute Rect from the placed
// parent-relative rects — the table mouse hit-testing and the cursor rule
// look up — and its visRect: that rect cut by parentVis, the part its ancestors
// and the screen let show.
func computeAbs(n *node, ox, oy int, parentVis Rect) {
	n.absRect = Rect{X: ox + n.rect.X, Y: oy + n.rect.Y, W: n.rect.W, H: n.rect.H}
	n.visRect = n.absRect.Intersect(parentVis)
	for _, ch := range n.children {
		if ch.visible() {
			computeAbs(ch, n.absRect.X, n.absRect.Y, n.visRect)
		}
	}
}
