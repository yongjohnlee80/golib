package tui

// DockEdge names the side a Dock child pins to. DockCenter children fill
// whatever the pinned children leave over (ADR-0004 §2.7.3).
type DockEdge uint8

const (
	DockTop DockEdge = iota
	DockBottom
	DockLeft
	DockRight
	DockCenter
)

// dockItem is one Dock child with its pinned edge.
type dockItem struct {
	comp Component
	edge DockEdge
}

// Dock is the chrome container (ADR-0004 §2.7.3): children pin to
// Top/Bottom/Left/Right in declaration order, each measured (loose on its
// pinned axis, tight on the other) and consuming its extent from the
// remaining rect; center children fill what is left under tight
// constraints. Status bars, side panels, command logs — the lazygit chrome —
// are Dock+Flex compositions.
type Dock struct {
	MultiChild[dockItem] // order (== pin-consumption order), mount mirror, Remove/Move/Children/Init
}

var _ Container = (*Dock)(nil)

// NewDock builds an empty Dock.
func NewDock() *Dock {
	d := &Dock{}
	d.compOf = func(it dockItem) Component { return it.comp }
	d.label = "Dock"
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
	d.MultiChild.Add(dockItem{comp: child, edge: edge})
}

// Add appends center children (Container contract): they fill the rect the
// pinned children leave over.
func (d *Dock) Add(children ...Component) {
	for _, c := range children {
		d.Pin(DockCenter, c)
	}
}

// Layout implements ADR-0004 §2.7.3: pinned children in declaration order,
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
		switch it.edge {
		case DockTop:
			sz := d.Ctx().LayoutChild(it.comp, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.Ctx().PlaceChild(it.comp, Rect{X: rem.X, Y: rem.Y, W: rem.W, H: sz.H})
			rem.Y += sz.H
			rem.H -= sz.H
		case DockBottom:
			sz := d.Ctx().LayoutChild(it.comp, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.Ctx().PlaceChild(it.comp, Rect{X: rem.X, Y: rem.Y + rem.H - sz.H, W: rem.W, H: sz.H})
			rem.H -= sz.H
		case DockLeft:
			sz := d.Ctx().LayoutChild(it.comp, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.Ctx().PlaceChild(it.comp, Rect{X: rem.X, Y: rem.Y, W: sz.W, H: rem.H})
			rem.X += sz.W
			rem.W -= sz.W
		case DockRight:
			sz := d.Ctx().LayoutChild(it.comp, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.Ctx().PlaceChild(it.comp, Rect{X: rem.X + rem.W - sz.W, Y: rem.Y, W: sz.W, H: rem.H})
			rem.W -= sz.W
		}
	}
	for _, it := range d.Items() {
		if it.edge != DockCenter {
			continue
		}
		d.Ctx().LayoutChild(it.comp, Tight(Size{W: rem.W, H: rem.H}))
		d.Ctx().PlaceChild(it.comp, rem)
	}
	return c.Constrain(Size{W: w, H: h})
}

// Render paints nothing: a Dock is pure geometry.
func (d *Dock) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (d *Dock) HandleEvent(Event) bool { return false }
