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
// look up.
func computeAbs(n *node, ox, oy int) {
	n.absRect = Rect{X: ox + n.rect.X, Y: oy + n.rect.Y, W: n.rect.W, H: n.rect.H}
	for _, ch := range n.children {
		if ch.visible() {
			computeAbs(ch, n.absRect.X, n.absRect.Y)
		}
	}
}

// renderTree paints the mounted, visible component tree depth-first in document order.
//
// # Architectural Invariants & Execution Guarantees
//
//  1. Single-Owner Loop Goroutine:
//     renderTree executes strictly on the App loop goroutine. It sets the a.inRender
//     guard flag for the duration of the traversal, ensuring that any illicit tree
//     mutation (Mount, Unmount, Move) attempted during Render triggers a fail-loud panic.
//
//  2. Root Surface Initialization:
//     Traverses from a.rootNode using a freshly instantiated root Surface (newRootSurface)
//     bound to the terminal back-buffer (a.buf) and render context (a.rctx).
//
//  3. Painter's Algorithm & Sandboxed Clipping:
//     Traverses depth-first so parent containers paint their chrome/background first,
//     followed by their children in document order. Children paint into isolated sub-surfaces
//     (s.Sub), enforcing bounding box clipping and local (0, 0) coordinate spaces.
func (a *App) renderTree() {
	root := a.rootNode
	if root == nil {
		return
	}
	a.inRender = true
	defer func() { a.inRender = false }()
	a.renderNode(root, newRootSurface(a.buf, a.rctx))
}

// renderNode recursively paints node n and all its visible children.
//
// # Traversal & Sandboxing Mechanics
//
//  1. Self Paint:
//     n.comp.Render(s) paints the current component onto Surface s. The coordinates
//     in s are relative to n's own origin (0, 0 to n.rect.W x n.rect.H).
//
//  2. Visibility Pruning:
//     Iterates over n.children in document order. Any child where ch.visible() is
//     false (such as unplaced nodes, hidden tabs, or zero-dimension components)
//     is pruned immediately from rendering along with its entire subtree.
//
//  3. Sub-Surface Isolation:
//     Visible children are recursively rendered via s.Sub(ch.rect). This constructs
//     a clipped sub-surface viewport: a child cannot draw outside its assigned
//     parent-relative rectangle, preventing rogue components from overwriting
//     neighboring sibling or parent cells.
func (a *App) renderNode(n *node, s Surface) {
	n.comp.Render(s)
	for _, ch := range n.children {
		if ch.visible() {
			a.renderNode(ch, s.Sub(ch.rect))
		}
	}
}
