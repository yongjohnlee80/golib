package decl

import (
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
// swap into the centre. A TileManager has at most one child at Tui.Center.
//
// A window declared in it (a Drawer, a FloatWindow) is a member (tilemember.go): its node stays in
// the Window's overlay, and while it wants to and fits the manager places it over a tile. Its tile
// is in its TileManager.* attached properties, the Tile's own names: TileManager.edge (default
// Tui.Left), .edges, .size, .minimumSize, and .tiled (default true), its Layout › Tiled. All but
// edges take a change while built, so a bound one moves the tile, or tiles and releases the window,
// rather than rebuilding it. tileChanged(key, operation, edge, size, tiled) reports a member's
// completed operation. A closed or minimized member takes no cells, and stays governed.
//
//	TileManager {
//	    Drawer { id: explorer; windowResize: true; TileManager.edge: Tui.Left; TileManager.size: 25
//	             TileManager.tiled: App.explorerTiled; … }
//	    FloatWindow { id: editor; TileManager.edge: Tui.Center; … }
//	}

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
	n := &tileNode{content: b.Children[0], key: b.ID,
		resized: b.EmitterWith("resized"), docked: b.EmitterWith("docked"), changed: b.EmitterWith("changed")}
	spec := tilePlaceSpec{}
	fields := spec.fields()
	fields["minimizable"], fields["key"] = into(&spec.place.Minimizable, boolOf), into(&n.key, stringOf)
	consumed, err := b.ReadProps(fields)
	if err != nil {
		return nil, nil, err
	}
	if n.place, err = spec.done("a Tile"); err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	return n, consumed, nil
}

// tilePlaceSpec is a tile's place as a document writes it: a Tile's own edge, edges, size and
// minimumSize, or a member's TileManager.* attached ones, read by the same fields.
type tilePlaceSpec struct {
	place         widget.TilePlace
	edges         string
	size, minimum float64
}

func (s *tilePlaceSpec) fields() map[string]field {
	return map[string]field{
		"edge": into(&s.place.Edge, drawerEdges.read), "edges": into(&s.edges, stringOf),
		"size": into(&s.size, numberOf), "minimumSize": into(&s.minimum, numberOf),
	}
}

// done is the place read: its edges parsed, its size and minimum finite and at least 0. who names
// what wrote it, for the error.
func (s *tilePlaceSpec) done(who string) (widget.TilePlace, error) {
	var err error
	if s.place.Edges, err = tileEdges(s.edges); err != nil {
		return widget.TilePlace{}, fmt.Errorf("%s: %w", who, err)
	}
	if !finite(s.size) || !finite(s.minimum) {
		return widget.TilePlace{}, fmt.Errorf("%s: size and minimumSize are finite numbers", who)
	}
	s.place.Size, s.place.Min = max(s.size, 0), max(int(s.minimum), 0)
	return s.place, nil
}

// tileEdges reads edges: edge names, as panelgeometry spells them, apart by spaces or commas.
func tileEdges(s string) ([]tui.DockEdge, error) {
	var out []tui.DockEdge
	for _, name := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' }) {
		e, err := drawerEdges.read(strValue(name))
		if err != nil {
			return nil, fmt.Errorf("edges: %w", err)
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

// tileWindow is a WindowMod method run on a Tile's window, as the WindowMod's own.
var tileWindow = windowMethod((*tileNode).window)

// tileManagerNode is a TileManager: golib's manager, mounted as its one child (as the Window mounts
// its host, rather than embedding it), and the windows it governs, which stay in the Window's
// overlay: the Window collects them (OverlayContainer) and gives each its overlay. origin is where
// the Window's Dock placed it, the frame its members' tile rects are offset into.
type tileManagerNode struct {
	widget.Base
	tiles   *widget.Tiles
	members []Overlaid
	origin  tui.Point
	changed func(args ...qml.SpecValue)
}

func (m *tileManagerNode) Init(ctx *tui.Context) {
	m.Base.Init(ctx)
	ctx.Mount(m.tiles)
}

// Layout gives the manager the whole of what it is offered.
func (m *tileManagerNode) Layout(c tui.Constraints) tui.Size {
	s := m.Context().LayoutChild(m.tiles, c)
	m.Context().PlaceChild(m.tiles, tui.Rect{W: s.W, H: s.H})
	return s
}

func (m *tileManagerNode) Render(tui.Surface) {}

var (
	_ OverlayContainer = (*tileManagerNode)(nil)
	_ dockPlaced       = (*tileManagerNode)(nil)
)

// OverlaidChildren implements OverlayContainer: the windows the manager governs.
func (m *tileManagerNode) OverlaidChildren() []Overlaid { return m.members }

func (m *tileManagerNode) placedInDock(r tui.Rect) { m.origin = tui.Point{X: r.X, Y: r.Y} }

// allowedIn: a manager with members is the Window's own child, the one place its members' overlay
// and their frame (the Dock's) are known.
func (m *tileManagerNode) allowedIn(self, parent string) error {
	if len(m.members) == 0 || parent == windowType.Name {
		return nil
	}
	return fmt.Errorf("a %s that governs windows is a Window's child, and %s", self, notOne(parent))
}

// buildTileManager adds each Tile, and each window (a Drawer, a FloatWindow) as a member, to a new
// manager, in declaration order: the order strips take their depth in.
func buildTileManager(b Build) (tui.Component, []string, error) {
	n := &tileManagerNode{changed: b.EmitterWith("tileChanged")}
	byKey := map[string]*tileNode{}
	opts := []widget.TilesOption{
		widget.WithTilesChanged(func(e widget.TilesChangedEvent) {
			if t := byKey[e.Key]; t != nil {
				t.emit(e)
				return
			}
			n.memberChanged(e)
		}),
		widget.WithTilesOrigin(func() tui.Point { return n.origin }),
	}
	if b.WindowCollector != nil {
		opts = append(opts, widget.WithTilesCollector(b.WindowCollector))
	}
	n.tiles = widget.NewTiles(opts...)
	keys, centre := map[string]bool{}, false
	for i, c := range b.Children {
		m, err := n.adopt(b, i, c, byKey)
		if err != nil {
			return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
		}
		if keys[m.key] {
			return nil, nil, fmt.Errorf("a TileManager's children have distinct ids, %q twice (at %s)", m.key, b.Pos)
		}
		if m.place.Edge == tui.DockCenter && centre {
			return nil, nil, fmt.Errorf("a TileManager has at most one child at Tui.Center (at %s)", b.Pos)
		}
		keys[m.key], centre = true, centre || m.place.Edge == tui.DockCenter
		n.add(b, i, c, m)
	}
	return n, nil, nil
}

// adopt reads child i: a Tile's key and place, or a member's from its TileManager.* attached
// properties, its key its id ("tile<i>" for an anonymous child, as a Tile's).
func (n *tileManagerNode) adopt(b Build, i int, c tui.Component, byKey map[string]*tileNode) (memberSpec, error) {
	if t, ok := c.(*tileNode); ok {
		for prop := range b.ChildAttached[i] {
			if strings.HasPrefix(prop, tileManagerName+".") {
				return memberSpec{}, fmt.Errorf("a Tile's place is its own edge, edges, size and minimumSize; %s is a window's (child %d)", prop, i)
			}
		}
		if t.key == "" {
			t.key = "tile" + strconv.Itoa(i)
		}
		byKey[t.key] = t
		return memberSpec{key: t.key, place: t.place}, nil
	}
	w, ok := c.(tileMember)
	if !ok {
		return memberSpec{}, fmt.Errorf("a TileManager holds Tiles and windows (a Drawer, a FloatWindow); child %d is neither", i)
	}
	if w.WindowBehavior() == nil {
		return memberSpec{}, fmt.Errorf("a TileManager's window child %d has no window: a Drawer there needs its window controls "+
			"(windowResize, movable, maximizable, minimizable or closable)", i)
	}
	if d, ok := c.(*drawerNode); ok && (d.reserve || d.grip != nil) {
		return memberSpec{}, fmt.Errorf("a TileManager's Drawer (child %d) is placed and sized by its tile: no reserve, and "+
			"windowResize rather than resizable's grip", i)
	}
	spec := tilePlaceSpec{place: widget.TilePlace{Edge: tui.DockLeft}}
	m := memberSpec{key: w.tileKey(), tiled: true}
	fields := spec.fields()
	fields["tiled"] = into(&m.tiled, boolOf)
	for name, f := range fields {
		if v, ok := b.Attached(i, tileManagerName+"."+name); ok {
			if err := f(v); err != nil {
				return memberSpec{}, fmt.Errorf("%s.%s: %w", tileManagerName, name, err)
			}
		}
	}
	var err error
	if m.place, err = spec.done(tileManagerName + ".*"); err != nil {
		return memberSpec{}, err
	}
	if m.key == "" {
		m.key = "tile" + strconv.Itoa(i)
	}
	return m, nil
}

// memberSpec is a child as the manager adds it: its key, its place, and for a member whether it
// wants to tile.
type memberSpec struct {
	key   string
	place widget.TilePlace
	tiled bool
}

// add puts child i in the manager: a Tile as a Tile, a member as a member that wants to tile as its
// TileManager.tiled says, with a receiver for its live TileManager.* properties (LiveAttached).
func (n *tileManagerNode) add(b Build, i int, c tui.Component, m memberSpec) {
	if t, ok := c.(*tileNode); ok {
		t.tiles, t.tile = n.tiles, n.tiles.Add(m.key, t, m.place)
		return
	}
	w := c.(tileMember)
	w.setTileEdge(m.place.Edge)
	n.members = append(n.members, c.(Overlaid))
	n.tiles.AddMember(m.key, w, m.place, m.tiled)
	b.OnAttached(i, func(prop string, v qml.SpecValue) error { return n.memberSet(m.key, w, prop, v) })
}

// memberSet is a live change of a member's TileManager.* property, read by the fields its build
// read it with. An edge the manager refuses (the centre's, one off its edges) leaves it where it is.
func (n *tileManagerNode) memberSet(key string, w tileMember, prop string, v qml.SpecValue) error {
	name := strings.TrimPrefix(prop, tileManagerName+".")
	if name == "tiled" {
		on, err := boolOf(v)
		if err == nil {
			n.tiles.SetWants(key, on)
		}
		return err
	}
	spec := tilePlaceSpec{place: n.tiles.Tile(key).Place()}
	spec.size, spec.minimum = spec.place.Size, float64(spec.place.Min)
	f, ok := spec.fields()[name]
	if !ok {
		return fmt.Errorf("%s takes no change while built", prop)
	}
	if err := f(v); err != nil {
		return fmt.Errorf("%s: %w", prop, err)
	}
	place, err := spec.done(prop)
	if err != nil {
		return err
	}
	switch name {
	case "edge":
		if n.tiles.SetEdge(key, place.Edge) {
			w.setTileEdge(place.Edge)
		}
	case "size":
		n.tiles.SetSize(key, place.Size)
	case "minimumSize":
		n.tiles.SetMin(key, place.Min)
	}
	return nil
}

// memberChanged raises tileChanged for a member's completed operation: its key, the operation, its
// tile's edge and size, and whether it is tiled now.
func (n *tileManagerNode) memberChanged(e widget.TilesChangedEvent) {
	tiled := false
	if t := n.tiles.Tile(e.Key); t != nil {
		tiled = t.Governed()
	}
	n.changed(strValue(e.Key), strValue(e.Operation.String()), strValue(edgeName(e.Edge)), floatValue(e.Size), boolValue(tiled))
}

// emit raises the Tile's signals for a completed operation: resized for a border it owns, docked
// for a re-dock or swap, and changed for every one.
func (n *tileNode) emit(e widget.TilesChangedEvent) {
	switch e.Operation {
	case widget.WindowResize:
		n.resized(floatValue(e.Size))
	case widget.WindowMove:
		n.docked(strValue(edgeName(e.Edge)))
	}
	n.changed(strValue(e.Operation.String()), boolValue(e.Maximized), boolValue(e.Hidden))
}

// tileManagerName is the TileManager's type name, and its attaching schema's: TileManager.tiled.
const tileManagerName = "TileManager"

var tileManagerType = Type{
	Name: tileManagerName, Build: buildTileManager,
	Signals: map[string][]string{"tileChanged": {"key", "operation", "edge", "size", "tiled"}},
}

var tileType = Type{
	Name: "Tile", Children: Exactly(1).Because("its content"),
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
