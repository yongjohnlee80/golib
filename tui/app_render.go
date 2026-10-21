package tui

// app_render.go: the render pass, painting the tree into the frame buffer.

// renderTree paints the mounted, visible component tree depth-first in document order.
//
// # Architectural Invariants & Execution Guarantees
//
//  1. Single-Owner Loop Goroutine:
//     renderTree executes strictly on the App loop goroutine. It sets the a.phase.inRender
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
	a.phase.inRender = true
	defer func() { a.phase.inRender = false }()
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
	native := -1
	if a.buf.stamping {
		a.buf.order++
		native = a.noteNative(n)
	}
	n.drawnNatively = native >= 0
	if f, ok := n.comp.(CellFacer); ok {
		if family := f.CellFace(); family != "" {
			// its subtree is painted in its face; the node painted next, in the one before
			defer func(face uint16) { a.buf.face = face }(a.buf.face)
			a.buf.face = a.faceIndex(family)
		}
	}
	n.comp.Render(s)
	a.noteImage(n)
	subtree := native >= 0 && a.native.natives[native].Scope == ScopeSubtree
	if subtree {
		a.native.styledDepth++
	}
	for _, ch := range n.children {
		if ch.visible() {
			a.renderNode(ch, s.Sub(ch.rect))
		}
	}
	if subtree {
		a.native.styledDepth--
	}
	if native >= 0 {
		a.native.natives[native].End = a.buf.order
	}
}
