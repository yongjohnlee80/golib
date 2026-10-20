package decl

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TILES — golib's tile manager (widget.Tiles, ADR 1792525673) as a document writes it: a
// TileManager whose children are Tiles. The tiles share the manager's whole rect; it owns their
// sizes. Alt-right drag in a tile moves the shared border nearest the press, Alt-left drag re-docks
// it or swaps it with another, a dock hides and shows it (WithWindowCollector), and nothing floats.
//
//	TileManager {
//	    Tile { id: explorer; edge: Tui.Left; edges: "left right"; size: 25; minimumSize: 20
//	           onResized: App.keep(size); TreeView { … } }
//	    Tile { id: query; edge: Tui.Center; Editor { … } }
//	    Tile { id: results; edge: Tui.Bottom; edges: "bottom top"; size: 40; TableView { … } }
//	}
//
// edges names the edges a re-dock may take, as panelgeometry spells them; "center" lets a tile
// swap into the centre. A TileManager has exactly one Tile at Tui.Center.

// tileNode is a Tile: its content, where it starts, and its signals. It is what the manager's
// window wraps, so it is mounted in the tree under its id, and a program finds its window through
// it (WindowBehavior).
type tileNode struct {
	widget.Base
	content tui.Component
	key     string
	place   widget.TilePlace
	tiles   *widget.Tiles // the manager it was added to; nil until the TileManager is built
	tile    *widget.Tile
	resized func(args ...qml.SpecValue)
	docked  func(args ...qml.SpecValue)
	changed func(args ...qml.SpecValue)
}

func buildTile(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, fmt.Errorf("a Tile holds exactly one child, its content, got %d (at %s)", len(b.Children), b.Pos)
	}
	n := &tileNode{content: b.Children[0], key: b.ID,
		resized: b.EmitterWith("resized"), docked: b.EmitterWith("docked"), changed: b.EmitterWith("changed")}
	var edges string
	var size, minimum float64
	consumed, err := b.ReadProps(map[string]field{
		"edge": into(&n.place.Edge, drawerEdges.read), "edges": into(&edges, stringOf),
		"size": into(&size, numberOf), "minimumSize": into(&minimum, numberOf),
		"minimizable": into(&n.place.Minimizable, boolOf), "key": into(&n.key, stringOf),
	})
	if err != nil {
		return nil, nil, err
	}
	if n.place.Edges, err = tileEdges(edges); err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	if !finite(size) || !finite(minimum) {
		return nil, nil, fmt.Errorf("a Tile's size and minimumSize are finite numbers (at %s)", b.Pos)
	}
	n.place.Size, n.place.Min = max(size, 0), max(int(minimum), 0)
	return n, consumed, nil
}

// tileEdges reads edges: edge names, as panelgeometry spells them, apart by spaces or commas.
func tileEdges(s string) ([]tui.DockEdge, error) {
	var out []tui.DockEdge
	for _, name := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		e, err := drawerEdges.read(qml.SpecValue{Kind: qml.SpecValueString, Raw: name})
		if err != nil {
			return nil, fmt.Errorf("a Tile's edges: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// edgeName is an edge as a document and panelgeometry spell it: "left".
func edgeName(e tui.DockEdge) string {
	for name, v := range drawerEdges.values {
		if v == e {
			return constantValue(name)
		}
	}
	return ""
}

func (n *tileNode) Init(ctx *tui.Context) {
	n.Base.Init(ctx)
	ctx.Mount(n.content)
}

func (n *tileNode) Layout(c tui.Constraints) tui.Size {
	s := n.Context().LayoutChild(n.content, c)
	n.Context().PlaceChild(n.content, tui.Rect{W: s.W, H: s.H})
	return s
}

func (n *tileNode) Render(tui.Surface) {}

// WindowBehavior is the tile's window: a dock reads whether it is hidden there.
func (n *tileNode) WindowBehavior() *widget.WindowCore {
	if n.tile == nil {
		return nil
	}
	return n.tile.Window().Core()
}

// setEdge re-docks the tile; an edge its manager refuses (the centre's, one off its edges, the one
// it is at) leaves it where it is.
func (n *tileNode) setEdge(e tui.DockEdge) {
	if n.tiles == nil {
		n.place.Edge = e
		return
	}
	n.tiles.SetEdge(n.key, e)
}

func (n *tileNode) setSize(pct float64) {
	if !finite(pct) {
		return
	}
	if n.tiles == nil {
		n.place.Size = max(pct, 0)
		return
	}
	n.tiles.SetSize(n.key, pct)
}

func (n *tileNode) setMinimumSize(cells float64) {
	if !finite(cells) {
		return
	}
	if n.tiles == nil {
		n.place.Min = max(int(cells), 0)
		return
	}
	n.tiles.SetMin(n.key, int(cells))
}

func (n *tileNode) window() (*widget.WindowMod, error) {
	if n.tile == nil {
		return nil, ErrWindowOperationUnavailable
	}
	return n.tile.Window(), nil
}

func (n *tileNode) show() error {
	if n.tiles == nil || !n.tiles.Show(n.key) {
		return ErrWindowOperationUnavailable
	}
	return nil
}

func (n *tileNode) hide() error {
	if n.tiles == nil || !n.tiles.Hide(n.key) {
		return ErrWindowOperationUnavailable
	}
	return nil
}

func (n *tileNode) toggle() error {
	if n.tile != nil && n.tile.Hidden() {
		return n.show()
	}
	return n.hide()
}

// tileWindow is a Tile's method run on its window, as the WindowMod's own.
func tileWindow(m Method) Method {
	return func(c tui.Component, args []qml.SpecValue) error {
		n, ok := c.(*tileNode)
		if !ok {
			return errors.New("not a Tile")
		}
		w, err := n.window()
		if err != nil {
			return err
		}
		return m(w, args)
	}
}

// buildTileManager adds each Tile to a new manager, in declaration order: the order strips take
// their depth in.
func buildTileManager(b Build) (tui.Component, []string, error) {
	var nodes []*tileNode
	centres := 0
	for i, c := range b.Children {
		n, ok := c.(*tileNode)
		if !ok {
			return nil, nil, fmt.Errorf("a TileManager holds Tiles only; child %d is not one (at %s)", i, b.Pos)
		}
		if n.key == "" {
			n.key = "tile" + strconv.Itoa(i)
		}
		if n.place.Edge == tui.DockCenter {
			centres++
		}
		nodes = append(nodes, n)
	}
	if centres != 1 {
		return nil, nil, fmt.Errorf("a TileManager has exactly one Tile at Tui.Center, got %d (at %s)", centres, b.Pos)
	}
	byKey := map[string]*tileNode{}
	opts := []widget.TilesOption{widget.WithTilesChanged(func(e widget.TilesChangedEvent) {
		if n := byKey[e.Key]; n != nil {
			n.emit(e)
		}
	})}
	if b.WindowCollector != nil {
		opts = append(opts, widget.WithTilesCollector(b.WindowCollector))
	}
	m := widget.NewTiles(opts...)
	for _, n := range nodes {
		if byKey[n.key] != nil {
			return nil, nil, fmt.Errorf("a TileManager's Tiles have distinct ids, %q twice (at %s)", n.key, b.Pos)
		}
		byKey[n.key] = n
		n.tiles, n.tile = m, m.Add(n.key, n, n.place)
	}
	return m, nil, nil
}

// emit raises the Tile's signals for a completed operation: resized for a border it owns, docked
// for a re-dock or swap, and changed for every one.
func (n *tileNode) emit(e widget.TilesChangedEvent) {
	switch e.Operation {
	case widget.WindowResize:
		n.resized(fracNumber(e.Size))
	case widget.WindowMove:
		n.docked(strValue(edgeName(e.Edge)))
	}
	n.changed(strValue(e.Operation.String()), windowBool(e.Maximized), windowBool(e.Hidden))
}

var tileManagerType = Type{Name: "TileManager", Build: buildTileManager}

var tileType = Type{
	Name:    "Tile",
	Build:   buildTile,
	Ctor:    []string{"edges", "minimizable", "key"},
	Signals: map[string][]string{"resized": {"size"}, "docked": {"edge"}, "changed": {"operation", "maximized", "hidden"}},
	Setters: map[string]Setter{
		"edge":        setter("a Tile", drawerEdges.read, (*tileNode).setEdge),
		"size":        setter("a Tile", numberOf, (*tileNode).setSize),
		"minimumSize": setter("a Tile", numberOf, (*tileNode).setMinimumSize),
	},
	Methods: map[string]Method{
		"show":           NoArgMethod((*tileNode).show),
		"hide":           NoArgMethod((*tileNode).hide),
		"toggle":         NoArgMethod((*tileNode).toggle),
		"toggleMaximize": tileWindow(windowInvoke(widget.WindowMaximizeAction{})),
		"resizeBy":       tileWindow(windowStep(false)),
	},
}
