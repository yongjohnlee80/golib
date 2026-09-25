package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// MenuBar is a PLACEMENT SHELL over a Menu, not a parallel widget.
//
// It owns one thing: where the root level's rows sit — along an edge instead of
// down a column — and which way its dropdowns open. Everything else is the
// Menu's: the model, the selection, the open levels, activation, the keys. That
// is deliberate, and it is the difference between one lifecycle and two. A
// MenuBar with its own model would need its own SetModel, its own selection
// repair and its own close path, and the day those disagreed with the Menu's
// would be the day a dropdown outlived the bar that opened it.
type MenuBar struct {
	Base
	menu      *Menu
	placement BarPlacement
}

// BarPlacement is the edge a MenuBar occupies.
type BarPlacement uint8

const (
	// BarPlacementTop is the default and the conventional place for a menu bar.
	BarPlacementTop BarPlacement = iota
	BarPlacementBottom
	BarPlacementLeft
	BarPlacementRight
)

// String names the placement for traces and test failures.
func (p BarPlacement) String() string {
	switch p {
	case BarPlacementTop:
		return "top"
	case BarPlacementBottom:
		return "bottom"
	case BarPlacementLeft:
		return "left"
	case BarPlacementRight:
		return "right"
	}
	return "unknown"
}

// Valid reports whether p is one of the declared placements. Exported for the
// same reason the other Valid methods are: a construction option has to reject
// an invalid value before storing it, and duplicating the bound is how two
// checks come to disagree.
func (p BarPlacement) Valid() bool { return p <= BarPlacementRight }

// horizontal reports whether this placement lays rows along a line.
func (p BarPlacement) horizontal() bool { return p == BarPlacementTop || p == BarPlacementBottom }

// dropSide is where a bar's first dropdown opens: AWAY from the bar's own edge,
// because a dropdown opening back across the bar would cover the row that opened
// it.
func (p BarPlacement) dropSide() PlacementSide {
	switch p {
	case BarPlacementBottom:
		return PlacementAbove
	case BarPlacementLeft:
		return PlacementRight
	case BarPlacementRight:
		return PlacementLeft
	default: // BarPlacementTop
		return PlacementBelow
	}
}

// MenuBarOption configures a MenuBar at construction.
type MenuBarOption func(*MenuBar)

// NewMenuBar wraps a Menu as a bar. The Menu keeps ownership of everything
// except layout.
func NewMenuBar(m *Menu, opts ...MenuBarOption) *MenuBar {
	if m == nil {
		panic(fatalOf("widget: NewMenuBar", "nil Menu",
			"a bar is a placement shell over a menu and has nothing to show without one"))
	}
	b := &MenuBar{menu: m}
	for _, o := range opts {
		if o != nil {
			o(b)
		}
	}
	// The orientation is applied at MOUNT, not here. Writing it into the Menu at
	// construction made building a bar a mutation of somebody else's widget:
	// constructing a second bar around the same Menu re-laid-out the first one,
	// which was already mounted and had nothing to do with the new shell, and a
	// Menu that had ever been in a bar stayed horizontal when later mounted on
	// its own. A shell configures the thing it owns for as long as it owns it.
	return b
}

// WithBarPlacement sets which edge the bar occupies. An invalid value is refused
// at construction rather than stored.
func WithBarPlacement(p BarPlacement) MenuBarOption {
	return func(b *MenuBar) {
		if !p.Valid() {
			panic(tuiFatal("widget: WithBarPlacement",
				"value outside the declared BarPlacement set", int(p)))
		}
		b.placement = p
	}
}

// Menu returns the menu this bar places. The model, the selection and the
// open/close lifecycle all live there — this is the handle for all of it, rather
// than a parallel API on the bar that could drift.
func (b *MenuBar) Menu() *Menu { return b.menu }

// Placement reports which edge the bar occupies.
func (b *MenuBar) Placement() BarPlacement { return b.placement }

// WithPointerPolicy delegates to the Menu, which is the node that owns the rows
// a pointer would land on.
func (b *MenuBar) WithPointerPolicy(p tui.PointerPolicy) *MenuBar {
	b.menu.WithPointerPolicy(p)
	return b
}

// Init applies this bar's layout to the Menu for the bar's lifetime, then
// mounts it.
//
// The orientation is applied BEFORE the mount so the Menu's first layout is
// already the bar's, and it is UNDONE on unmount so the Menu leaves the bar the
// way it arrived. A bare Menu stacks its rows; one that had been in a bar and
// stayed horizontal would render a menu nobody asked for, in a layout that no
// longer exists.
func (b *MenuBar) Init(ctx *tui.Context) {
	b.Base.Init(ctx)
	prevH, prevSide := b.menu.horizontal, b.menu.dropSide
	b.menu.horizontal = b.placement.horizontal()
	b.menu.dropSide = b.placement.dropSide()
	ctx.OnUnmount(func() {
		b.menu.horizontal, b.menu.dropSide = prevH, prevSide
	})
	ctx.Mount(b.menu)
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the bar is not a tab stop; the
// Menu inside it is, and a focusable shell would insert a stop that does
// nothing between the application and its menu.

// Layout takes one line across for a horizontal bar, or one column down for a
// vertical one, and fills it with the menu.
//
// THE BAR DOES NOT POSITION ITSELF, and cannot: a component's parent decides
// where it goes. A bar that returned a one-row size and then tried to place its
// menu at the bottom of the screen would place it outside its own rect, where it
// is clipped away entirely — which is exactly what an earlier version of this
// did. So BarPlacement means ORIENTATION and DROPDOWN DIRECTION; put the bar at
// the bottom of the screen by putting it at the bottom of the layout that owns
// it, as with any other component.
func (b *MenuBar) Layout(cs tui.Constraints) tui.Size {
	ctx := b.Context()
	if ctx == nil {
		return cs.Constrain(tui.Size{})
	}
	full := tui.Size{W: cs.MaxW, H: cs.MaxH}
	got := ctx.LayoutChild(b.menu, tui.Loose(full))

	size := tui.Size{W: full.W, H: got.H} // a line across
	if !b.placement.horizontal() {
		size = tui.Size{W: got.W, H: full.H} // a column down
	}
	size = cs.Constrain(size)
	ctx.PlaceChild(b.menu, tui.Rect{X: 0, Y: 0, W: size.W, H: size.H})
	return size
}

// Render paints nothing: the bar is placement, and the menu paints its own rows.
func (b *MenuBar) Render(tui.Surface) {}
