package tui

// DockEdge names the side a Dock child pins to. DockCenter children fill
// whatever the pinned children leave over.
type DockEdge uint8

const (
	DockTop DockEdge = iota
	DockBottom
	DockLeft
	DockRight
	DockCenter
)

// Pinned edges live in a side table keyed by the child; absent = a center
// child filling the remainder.

// Dock is the window chrome and edge-framing container:
//
// # Geometry Model & Edge Consumption
//
// Dock pins children to the perimeter edges in declaration order, deducting each
// pinned child's size from the remaining available area:
//
//	┌────────────────────────────────────────────────────────┐
//	│                 DockTop (e.g. MenuBar)                 │
//	├──────────┬──────────────────────────────────┬──────────┤
//	│          │                                  │          │
//	│ DockLeft │       DockCenter (Body Area)     │DockRight │
//	│ (Sidebar)│       (Fills All Remainder)      │(Inspector│
//	│          │                                  │          │
//	├──────────┴──────────────────────────────────┴──────────┤
//	│                DockBottom (e.g. StatusBar)             │
//	└────────────────────────────────────────────────────────┘
//
// The edge-consumption algorithm, in the order children are laid out:
//
//  1. Pinned Children: Children pinned via Pin (DockTop, DockBottom, DockLeft, DockRight)
//     are laid out in declaration order. Pinned children receive loose constraints along
//     their pinned axis and tight constraints across the orthogonal axis. Their measured
//     extent is deducted from the remaining Rect.
//  2. Center Children: Children added without an edge (via Add or Pin with DockCenter)
//     fill whatever area remains under tight constraints.
//
// Status bars, side navigation drawers, search toolbars, and inspector panels
// are easily composed using Dock in combination with Flex and Box.
type Dock struct {
	MultiChild // order (== pin-consumption order), mount mirror, Move/Children/Init
	edges      map[Component]DockEdge
	placed     func(child Component, r Rect)
}

// OnPlaced has fn told where each child is placed, Dock-relative, as the Dock lays it out: one
// call per child per pass, in placement order. It runs inside Layout, so fn must only record what
// it is told (a slot's rectangle, for a layer to follow in the same pass); a tree mutation or a
// RequestLayout from it is the layout phase's forbidden mutation. nil stops the calls.
func (d *Dock) OnPlaced(fn func(child Component, r Rect)) { d.placed = fn }

// place positions child at r and tells OnPlaced's fn.
func (d *Dock) place(child Component, r Rect) {
	d.Ctx().PlaceChild(child, r)
	if d.placed != nil {
		d.placed(child, r)
	}
}

var _ Container = (*Dock)(nil)

// NewDock builds an empty Dock.
func NewDock() *Dock {
	d := &Dock{}
	d.Label("Dock")
	return d
}

// Pin appends a child pinned to edge (declaration order is consumption
// order).
func (d *Dock) Pin(edge DockEdge, child Component) {
	if child == nil {
		panic("tui: Dock.Pin: nil child")
	}
	if edge > DockCenter {
		panic("tui: Dock.Pin: invalid edge")
	}
	d.MultiChild.Add(child)
	if edge != DockCenter {
		if d.edges == nil {
			d.edges = make(map[Component]DockEdge)
		}
		d.edges[child] = edge
	}
}

// SetEdge pins child, already in the Dock, to edge from the next layout on: DockCenter makes it a
// center child. Its place in the consumption order is kept.
func (d *Dock) SetEdge(child Component, edge DockEdge) {
	if edge > DockCenter {
		panic("tui: Dock.SetEdge: invalid edge")
	}
	if edge == DockCenter {
		delete(d.edges, child)
	} else {
		if d.edges == nil {
			d.edges = make(map[Component]DockEdge)
		}
		d.edges[child] = edge
	}
	if d.Ctx() != nil {
		d.Ctx().RequestLayout()
	}
}

// Remove unmounts child (cascade) and forgets it, edge entry included.
func (d *Dock) Remove(child Component) {
	delete(d.edges, child)
	d.MultiChild.remove(child)
}

// Add appends center children (Container contract): they fill the rect the
// pinned children leave over.
func (d *Dock) Add(children ...Component) {
	for _, c := range children {
		d.Pin(DockCenter, c)
	}
}

// Layout places pinned children in declaration order,
// then center children fill the remainder tight.
func (d *Dock) Layout(c Constraints) Size {
	w, h := c.MaxW, c.MaxH
	if w == Unbounded {
		w = c.MinW
	}
	if h == Unbounded {
		h = c.MinH
	}
	rem := Rect{X: 0, Y: 0, W: w, H: h}

	for _, it := range d.All() {
		// Center children have NO edges entry — a zero-value lookup would
		// read as DockTop (0); unpin by presence, not by value.
		edge, pinned := d.edges[it]
		if !pinned {
			continue
		}
		switch edge {
		case DockTop:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.place(it, Rect{X: rem.X, Y: rem.Y, W: rem.W, H: sz.H})
			rem.Y += sz.H
			rem.H -= sz.H
		case DockBottom:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.place(it, Rect{X: rem.X, Y: rem.Y + rem.H - sz.H, W: rem.W, H: sz.H})
			rem.H -= sz.H
		case DockLeft:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.place(it, Rect{X: rem.X, Y: rem.Y, W: sz.W, H: rem.H})
			rem.X += sz.W
			rem.W -= sz.W
		case DockRight:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.place(it, Rect{X: rem.X + rem.W - sz.W, Y: rem.Y, W: sz.W, H: rem.H})
			rem.W -= sz.W
		}
	}
	for _, it := range d.All() {
		if _, pinned := d.edges[it]; pinned {
			continue
		}
		d.Ctx().LayoutChild(it, Tight(Size{W: rem.W, H: rem.H}))
		d.place(it, rem)
	}
	return c.Constrain(Size{W: w, H: h})
}

// Render paints nothing: a Dock is pure geometry.
func (d *Dock) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (d *Dock) HandleEvent(Event) bool { return false }
