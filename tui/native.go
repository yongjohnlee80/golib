package tui

// NATIVE VIEWS — a component drawn natively by the backend, over its cells.
//
// A backend that draws more than cells (a golib/gui window) implements NativeHost. After each
// frame's paint, before Flush, the App hands it every visible component that is drawn natively:
// one that reports a view itself (NativeReporter), or one the backend offers to draw
// (NativeStyler: a native style of a tui primitive). The component still lays out in cells and
// still paints them; on any other backend its cells are what shows.
//
// A native view sits in the paint order: whatever tui paints after it covers it. While a
// NativeHost is attached, every cell write is stamped with the paint order of the node that made
// it (implicit writes too: freeing half of a wide character), and each placement lists the cells
// inside it painted later (Covered). The host composites in placement order: each view under
// what was painted after it, and a later view over an earlier one.
//
// On a backend that is not a NativeHost, none of this runs: no stamps, no placements.

// NativeScope is what a native view draws for, and so what covers it.
type NativeScope uint8

const (
	// ScopeChrome: the node's own look, beneath its children (a frame, a dialog's surface). Its
	// descendants are painted and offered as usual, and their cells cover it.
	ScopeChrome NativeScope = iota
	// ScopeSubtree: the node's whole subtree, drawn as one control (a checkbox and the button
	// inside it). Its descendants are never offered or reported, and only cells painted after the
	// whole subtree cover it.
	ScopeSubtree
)

// NativeReporter is a component that a NativeHost draws natively over its cells.
type NativeReporter interface {
	Component
	// NativeView is what the host draws; its type is the host's business. ok=false: nothing
	// natively this frame.
	NativeView() (view any, ok bool)
}

// NativeScoper is an optional NativeReporter capability: the scope of its view. A reporter
// without it is ScopeChrome.
type NativeScoper interface {
	NativeScope() NativeScope
}

// NativeStyler is an optional NativeHost capability: the backend offers to draw a component
// natively although it did not ask to be, as a native style of a tui primitive. Asked once per
// visible node per frame, outside subtree-scoped views.
type NativeStyler interface {
	NativeFor(c Component) (view any, scope NativeScope, ok bool)
}

// NativePlacement is one native view this frame, over the screen cells X, Y to X+Cols, Y+Rows.
type NativePlacement struct {
	View             any
	Node             NodeID
	Scope            NativeScope
	Layer            uint32 // the node's paint order
	End              uint32 // the last paint order inside its subtree
	X, Y, Cols, Rows int
	Covered          []Rect // cells inside it painted after it (after its subtree, for ScopeSubtree)
}

// NativeHost is a Backend that draws native views. PlaceNatives is called once per frame, before
// Flush, with every placement in paint order (none, when nothing is native), so the host can
// remove what is gone.
type NativeHost interface {
	PlaceNatives(ps []NativePlacement)
}

// beginNatives starts a frame's native placements, when the backend hosts them.
func (a *App) beginNatives() {
	_, hosts := a.backend.(NativeHost)
	a.buf.stamping = hosts
	a.natives = a.natives[:0]
	a.styledDepth = 0
	a.nativeStyler = nil
	if hosts {
		a.buf.resetStamps()
		a.nativeStyler, _ = a.backend.(NativeStyler)
	}
}

// noteNative records n's native placement, if it has one, and returns its index in a.natives, or
// -1. Called with n's paint order current, before n paints.
func (a *App) noteNative(n *node) int {
	if a.styledDepth > 0 {
		return -1 // inside a subtree-scoped view: the owner draws it all
	}
	var (
		view  any
		scope NativeScope
		ok    bool
	)
	if r, is := n.comp.(NativeReporter); is {
		view, ok = r.NativeView()
		if sc, has := n.comp.(NativeScoper); has {
			scope = sc.NativeScope()
		}
	} else if a.nativeStyler != nil {
		view, scope, ok = a.nativeStyler.NativeFor(n.comp)
	}
	if !ok {
		return -1
	}
	r := n.absRect.Intersect(Rect{W: a.size.W, H: a.size.H})
	if r.Empty() {
		return -1
	}
	a.natives = append(a.natives, NativePlacement{
		View: view, Node: n.id, Scope: scope, Layer: a.buf.order,
		X: r.X, Y: r.Y, Cols: r.W, Rows: r.H,
	})
	return len(a.natives) - 1
}

// applyNatives works out each placement's Covered cells and hands the frame's placements to the
// host.
func (a *App) applyNatives() {
	host, ok := a.backend.(NativeHost)
	if !ok {
		return
	}
	for i := range a.natives {
		p := &a.natives[i]
		after := p.Layer
		if p.Scope == ScopeSubtree {
			after = p.End
		}
		p.Covered = a.coveredAfter(Rect{X: p.X, Y: p.Y, W: p.Cols, H: p.Rows}, after)
	}
	host.PlaceNatives(a.natives)
}

// coveredAfter is the cells of r stamped later than order, as one Rect per run on each row.
func (a *App) coveredAfter(r Rect, order uint32) []Rect {
	var out []Rect
	for y := r.Y; y < r.Y+r.H; y++ {
		start := -1
		for x := r.X; x <= r.X+r.W; x++ {
			later := x < r.X+r.W && a.buf.stamp[y*a.buf.w+x] > order
			switch {
			case later && start < 0:
				start = x
			case !later && start >= 0:
				out = append(out, Rect{X: start, Y: y, W: x - start, H: 1})
				start = -1
			}
		}
	}
	return out
}
