package tui

import (
	"fmt"
	"iter"
	"slices"
)

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
	items []dockItem
	ctx   *Context // non-nil exactly while mounted
}

var _ Container = (*Dock)(nil)

// NewDock builds an empty Dock.
func NewDock() *Dock { return &Dock{} }

// Pin appends a child pinned to edge (declaration order is consumption
// order).
func (d *Dock) Pin(edge DockEdge, child Component) {
	if child == nil {
		panic("tui: Dock.Pin: nil child")
	}
	if edge > DockCenter {
		panic("tui: Dock.Pin: invalid edge")
	}
	d.items = append(d.items, dockItem{comp: child, edge: edge})
	if d.ctx != nil {
		d.ctx.Mount(child)
	}
}

// Add appends center children (Container contract): they fill the rect the
// pinned children leave over.
func (d *Dock) Add(children ...Component) {
	for _, c := range children {
		d.Pin(DockCenter, c)
	}
}

// Move relocates child to index to (Container contract), preserving its
// mount — no unmount/Init cycle. Declaration order is pin-consumption
// order, so moving a child past a differently-pinned sibling changes
// which chrome reserves space first (same-edge moves change paint/focus
// order only; edges themselves are unaffected). An unmounted Dock
// reorders items only; the framework mirror happens at Init.
func (d *Dock) Move(child Component, to int) {
	for i, it := range d.items {
		if it.comp == child {
			if to < 0 || to >= len(d.items) {
				panic(fmt.Sprintf("tui: Dock.Move: index %d out of range [0,%d)", to, len(d.items)))
			}
			if i == to {
				return
			}
			d.items = slices.Insert(slices.Delete(d.items, i, i+1), to, it)
			if d.ctx != nil {
				d.ctx.Move(child, to)
			}
			return
		}
	}
}

// Remove unmounts child (cascade) and forgets it.
func (d *Dock) Remove(child Component) {
	for i, it := range d.items {
		if it.comp == child {
			d.items = append(d.items[:i], d.items[i+1:]...)
			if d.ctx != nil {
				d.ctx.Unmount(child)
			}
			return
		}
	}
}

// Children enumerates in document order == focus order == paint order.
func (d *Dock) Children() iter.Seq[Component] {
	return func(yield func(Component) bool) {
		for _, it := range d.items {
			if !yield(it.comp) {
				return
			}
		}
	}
}

// Init mounts the deferred children. Re-entrant across remounts.
func (d *Dock) Init(ctx *Context) {
	d.ctx = ctx
	ctx.OnUnmount(func() { d.ctx = nil })
	for _, it := range d.items {
		ctx.Mount(it.comp)
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

	for _, it := range d.items {
		switch it.edge {
		case DockTop:
			sz := d.ctx.LayoutChild(it.comp, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.ctx.PlaceChild(it.comp, Rect{X: rem.X, Y: rem.Y, W: rem.W, H: sz.H})
			rem.Y += sz.H
			rem.H -= sz.H
		case DockBottom:
			sz := d.ctx.LayoutChild(it.comp, Constraints{MinW: rem.W, MaxW: rem.W, MinH: 0, MaxH: rem.H})
			d.ctx.PlaceChild(it.comp, Rect{X: rem.X, Y: rem.Y + rem.H - sz.H, W: rem.W, H: sz.H})
			rem.H -= sz.H
		case DockLeft:
			sz := d.ctx.LayoutChild(it.comp, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.ctx.PlaceChild(it.comp, Rect{X: rem.X, Y: rem.Y, W: sz.W, H: rem.H})
			rem.X += sz.W
			rem.W -= sz.W
		case DockRight:
			sz := d.ctx.LayoutChild(it.comp, Constraints{MinW: 0, MaxW: rem.W, MinH: rem.H, MaxH: rem.H})
			d.ctx.PlaceChild(it.comp, Rect{X: rem.X + rem.W - sz.W, Y: rem.Y, W: sz.W, H: rem.H})
			rem.W -= sz.W
		}
	}
	for _, it := range d.items {
		if it.edge != DockCenter {
			continue
		}
		d.ctx.LayoutChild(it.comp, Tight(Size{W: rem.W, H: rem.H}))
		d.ctx.PlaceChild(it.comp, rem)
	}
	return c.Constrain(Size{W: w, H: h})
}

// Render paints nothing: a Dock is pure geometry.
func (d *Dock) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (d *Dock) HandleEvent(Event) bool { return false }
