package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TILE MEMBERS — a window a TileManager governs without owning its node (widget.GovernedWindow): a
// Drawer or a FloatWindow declared in the manager. Its node stays in the overlay. While governed it
// is anchored live over its tile and its core takes the tile's target; released, it is back at its
// own place, which governing never touched.

// tileMember is a window a TileManager can govern: its tile's key (its id, "" for an anonymous
// one), and told its tile's edge before it is added, for the docked anchor it falls back to while
// its tile has no cells.
type tileMember interface {
	widget.GovernedWindow
	tileKey() string
	setTileEdge(tui.DockEdge)
}

// tileGovernance is a member's side of the governance, embedded by each member node: its key (the
// node's id, set by its builder), the tile's rect while governed (nil while the window is its own),
// the tile's target, kept so a core the node builds again takes it too, and the edge of its tile.
type tileGovernance struct {
	govKey    string
	gov       func() (tui.Rect, bool)
	govTarget any
	govEdge   tui.DockEdge
}

func (g *tileGovernance) tileKey() string { return g.govKey }

// setTileEdge is the TileManager's: the member's tile is at e.
func (g *tileGovernance) setTileEdge(e tui.DockEdge) { g.govEdge = e }

// govern and release record the governance. The node then anchors its Float and retargets its core.
func (g *tileGovernance) govern(rect func() (tui.Rect, bool), target any) {
	g.gov, g.govTarget = rect, target
}

func (g *tileGovernance) release() { g.gov, g.govTarget = nil, nil }

// governed reports whether a TileManager places the window now.
func (g *tileGovernance) governed() bool { return g.gov != nil }

// governedAnchor is the Float's anchor while governed: over the tile, and while it has no cells
// docked at the tile's edge (a corner for the fill member's), so a governed window reads docked and
// never floats with a shadow.
func (g *tileGovernance) governedAnchor() widget.Anchor {
	return widget.AtLive(g.gov, edgeAnchorOf(g.govEdge, widget.TopLeft))
}

// edgeAnchorOf is the anchor at edge e, centre for tui.DockCenter.
func edgeAnchorOf(e tui.DockEdge, centre widget.Anchor) widget.Anchor {
	switch e {
	case tui.DockRight:
		return widget.Right
	case tui.DockTop:
		return widget.Top
	case tui.DockBottom:
		return widget.Bottom
	case tui.DockCenter:
		return centre
	default:
		return widget.Left
	}
}
