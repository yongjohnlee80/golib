package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// RESERVING DRAWERS — golib's: reserve: true makes a Drawer take its strip of the Window from the
// page while it is docked and shown, as a split pane would, where an ordinary Drawer lies over the
// page. The strip sits between the Window's own docked children (a MenuBar, a StatusBar), and the
// page lays out beside it. Floating, minimized or closed, the Drawer gives it back; maximized, it
// keeps it, so the page beneath never reflows.
//
//	Window {
//	    MenuBar { Dock.edge: Tui.Top; … }
//	    Frame { Editor { id: query } }
//	    StatusBar { Dock.edge: Tui.Bottom; … }
//	    Drawer { edge: Tui.Left; size: 25; reserve: true; Frame { TreeView { … } } }
//	    Drawer { edge: Tui.Bottom; size: 30; reserve: true; Frame { TableView { … } } }
//	}
//
// Reserving Drawers take their strips in declaration order: the left one above has the height
// between the bars, and the bottom one the width beside it. length does not apply while a Drawer
// reserves (its strip is the whole edge); it is kept, for when the Drawer floats. reserve is a
// construction property: a document changes it by declaring the Drawer again (a reload rebuilds
// it), not by a binding. A keyboard move or resize (moveBy, resizeBy) starts from the strip.

// reserveSlot is a reserving Drawer's strip of its Window: pinned to the Drawer's edge in the
// Window's Dock, after the Window's own docked children, the Drawer's docked extent while it
// reserves, nothing otherwise. It draws nothing: the Drawer is over it, anchored where the Dock
// placed it this pass (Dock.OnPlaced, widget.AtLive).
type reserveSlot struct {
	widget.Base
	d   *drawerNode
	win tui.Size // the Window's size, set by the Window before each layout
}

func newReserveSlot(d *drawerNode) *reserveSlot { return &reserveSlot{d: d} }

// placedInDock records the strip for the Drawer's Float (its liveSlot).
func (s *reserveSlot) placedInDock(r tui.Rect) { s.d.slot = r }

// slotEdge is the Dock edge a slot is pinned to: the Drawer's, or the left for a centred Drawer,
// which reserves nothing (pinned to the centre, the slot would take the page's place).
func slotEdge(d *drawerNode) tui.DockEdge {
	if d.edge == tui.DockCenter {
		return tui.DockLeft
	}
	return d.edge
}

// Layout is the strip: the Drawer's docked size across its edge while it reserves, the Dock's
// offer along it; nothing otherwise.
func (s *reserveSlot) Layout(c tui.Constraints) tui.Size {
	if !s.d.reserving() || s.win.W <= 0 || s.win.H <= 0 {
		return c.Constrain(tui.Size{})
	}
	cells := s.d.dockedCells(s.win)
	if s.d.across() {
		return c.Constrain(tui.Size{W: cells.W, H: c.MaxH})
	}
	return c.Constrain(tui.Size{W: c.MaxW, H: cells.H})
}

func (s *reserveSlot) Render(tui.Surface) {}

// reserveSlots are the strips of the reserving Drawers among overlaid, to dock after the Window's
// own children. A Drawer keeps its slot across a reload; one no longer reserving loses it.
func (w *windowNode) reserveSlots(overlaid []Overlaid) []docked {
	var items []docked
	slots := map[*drawerNode]*reserveSlot{}
	for _, o := range overlaid {
		d, ok := o.(*drawerNode)
		if !ok || !d.reserve {
			continue
		}
		s := w.slots[d]
		if s == nil {
			s = newReserveSlot(d)
		}
		slots[d] = s
		d.onEdge = func(tui.DockEdge) {
			if w.dock != nil {
				w.dock.SetEdge(s, slotEdge(d))
			}
		}
		items = append(items, docked{s, slotEdge(d)})
	}
	w.slots = slots
	return items
}
