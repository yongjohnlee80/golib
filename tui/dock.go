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

// Dock is the chrome container: children pin to Top/Bottom/Left/Right in
// declaration order, each measured (loose on its pinned axis, tight on the
// other) and consuming its extent from the remaining rect; center children
// fill what is left under tight constraints. Status bars, side panels,
// command logs — the lazygit chrome — are Dock+Flex compositions.
type Dock struct {
	MultiChild // order (== pin-consumption order), mount mirror, Move/Children/Init
	edges      map[Component]DockEdge
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

	for _, it := range d.Items() {
		// Center children have NO edges entry — a zero-value lookup would
		// read as DockTop (0); unpin by presence, not by value.
		edge, pinned := d.edges[it]
		if !pinned {
			continue
		}
		switch edge {
		case DockTop:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.Ctx().PlaceChild(it, Rect{X: rem.X, Y: rem.Y, W: rem.W, H: sz.H})
			rem.Y += sz.H
			rem.H -= sz.H
		case DockBottom:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.Ctx().PlaceChild(it, Rect{X: rem.X, Y: rem.Y + rem.H - sz.H, W: rem.W, H: sz.H})
			rem.H -= sz.H
		case DockLeft:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.Ctx().PlaceChild(it, Rect{X: rem.X, Y: rem.Y, W: sz.W, H: rem.H})
			rem.X += sz.W
			rem.W -= sz.W
		case DockRight:
			sz := d.Ctx().LayoutChild(it, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.Ctx().PlaceChild(it, Rect{X: rem.X + rem.W - sz.W, Y: rem.Y, W: sz.W, H: rem.H})
			rem.W -= sz.W
		}
	}
	for _, it := range d.Items() {
		if _, pinned := d.edges[it]; pinned {
			continue
		}
		d.Ctx().LayoutChild(it, Tight(Size{W: rem.W, H: rem.H}))
		d.Ctx().PlaceChild(it, rem)
	}
	return c.Constrain(Size{W: w, H: h})
}

// Render paints nothing: a Dock is pure geometry.
func (d *Dock) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (d *Dock) HandleEvent(Event) bool { return false }
