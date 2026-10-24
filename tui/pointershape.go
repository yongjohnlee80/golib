package tui

// POINTER SHAPES — the shape a window's pointer takes over a component.
//
// A backend that shows a pointer (a golib/gui window) implements PointerShapeHost. After each
// frame's paint, before Flush, the App hands it the cell regions that shape the pointer: each
// PointerShaper's regions, as they show. A region is cut to its node's visible cells, and to the
// cells painted after its node (a dialog, a menu, a child drawn over it), so a covered cell never
// shows a shape from beneath. Every other cell takes the backend's default.
//
// On a backend that is not a PointerShapeHost, none of this runs: a terminal has no pointer shapes.

// PointerShape is a shape the window's pointer takes.
type PointerShape uint8

const (
	PointerDefault    PointerShape = iota // the platform's arrow
	PointerText                           // an I-beam, over text to select or type
	PointerHand                           // a pointing hand, over a link
	PointerCrosshair                      // a crosshair, for a precise point
	PointerMove                           // arrows in four directions, for something to move
	PointerResizeEW                       // a left-right arrow, for a width
	PointerResizeNS                       // an up-down arrow, for a height
	PointerNotAllowed                     // a struck circle, for what can't be done here
	PointerResizeNWSE                     // a diagonal arrow, top-left to bottom-right, for a corner
	PointerResizeNESW                     // a diagonal arrow, top-right to bottom-left, for a corner
)

// PointerRegion is a rect of a component's cells, in its own frame, and the shape over it.
type PointerRegion struct {
	Rect  Rect
	Shape PointerShape
}

// PointerShaper is a component that shapes the pointer over parts of itself. PointerShapes
// appends its regions to dst, in its own frame, after it paints, each frame; the cells it lists
// none for take the default (an editor's gutter, a field's border). A later region wins where two
// overlap.
type PointerShaper interface {
	PointerShapes(dst []PointerRegion) []PointerRegion
}

// PointerShapePlacement is one run of cells on a row, in absolute cells, that shapes the pointer.
// Layer is its node's paint order, as NativePlacement's is.
type PointerShapePlacement struct {
	X, Y, W int
	Shape   PointerShape
	Layer   uint32
}

// PointerShapeHost is a Backend that shapes the pointer. PlacePointerShapes is called once per
// frame, before Flush, with every shaped run in paint order (none, when nothing shapes it).
type PointerShapeHost interface {
	PlacePointerShapes(ps []PointerShapePlacement)
}

// shapeRegion is one region a node gave this frame: absolute, cut to the node's visible cells.
type shapeRegion struct {
	rect  Rect
	shape PointerShape
	order uint32
}

// shapeState is this frame's regions, and scratch reused across frames.
type shapeState struct {
	regions []shapeRegion
	local   []PointerRegion
	out     []PointerShapePlacement
}

// noteShapes records n's regions, if it shapes the pointer: called after n paints, with its paint
// order current.
func (a *App) noteShapes(n *node, order uint32) {
	sh, ok := n.comp.(PointerShaper)
	if !ok || a.caps.shapes == nil || n.visRect.Empty() {
		return
	}
	a.shape.local = sh.PointerShapes(a.shape.local[:0])
	for _, r := range a.shape.local {
		abs := r.Rect.Translate(n.absRect.X, n.absRect.Y).Intersect(n.visRect)
		if abs.Empty() || r.Shape == PointerDefault {
			continue
		}
		a.shape.regions = append(a.shape.regions, shapeRegion{rect: abs, shape: r.Shape, order: order})
	}
}

// applyShapes hands the frame's shaped runs to the host: each region's cells that no node painted
// over after its own, one run per row.
func (a *App) applyShapes() {
	host := a.caps.shapes
	if host == nil {
		return
	}
	out := a.shape.out[:0]
	for _, r := range a.shape.regions {
		for y := r.rect.Y; y < r.rect.Y+r.rect.H; y++ {
			start := -1
			for x := r.rect.X; x <= r.rect.X+r.rect.W; x++ {
				shows := x < r.rect.X+r.rect.W && a.buf.stamp[y*a.buf.w+x] <= r.order
				switch {
				case shows && start < 0:
					start = x
				case !shows && start >= 0:
					out = append(out, PointerShapePlacement{X: start, Y: y, W: x - start, Shape: r.shape, Layer: r.order})
					start = -1
				}
			}
		}
	}
	a.shape.out = out
	a.shape.regions = a.shape.regions[:0]
	host.PlacePointerShapes(out)
}
