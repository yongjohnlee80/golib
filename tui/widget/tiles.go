package widget

import (
	"math"
	"slices"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TILES — a container whose tiles share its whole rect, as a tiling window manager's do (ADR
// 1792525673). The manager owns every tile's size: a tile asks, the manager clamps and lays out.
//
//	tiles := widget.NewTiles(widget.WithTilesCollector(dock))
//	tiles.Add("explorer", tree, widget.TilePlace{Edge: tui.DockLeft, Edges: []tui.DockEdge{tui.DockLeft, tui.DockRight}, Size: 25, Min: 20, Minimizable: true})
//	tiles.Add("query", editor, widget.TilePlace{Edge: tui.DockCenter, Min: 10})
//	tiles.Add("results", table, widget.TilePlace{Edge: tui.DockBottom, Edges: []tui.DockEdge{tui.DockBottom, tui.DockTop}, Size: 40, Min: 5, Minimizable: true})
//
// The layout is the Dock's rule: edge tiles (strips) take their depth from the remaining rect in
// declaration order, each its Size percent of the manager across its edge, and the centre tile takes
// what is left; with no centre tiled, the last strip does. It is computed by plan, a pure function, rather than by a tui.Dock, because a
// gesture needs the layout a request would give before it is applied (RequestedBounds), and one
// function serves both.
//
// Each tile is a window: its content is wrapped in a WindowMod whose target is the manager, so the
// window gestures are tiling operations. Alt-right drag moves the shared border nearest the press
// (the strip that owns it is resized), Alt-left drag drops the tile on another tile (a swap) or near
// a side of the manager (a re-dock), with the landing rect outlined until the release; minimize
// hides it, its strip going to the tiles beside it; maximize gives it the whole rect. Escape during
// a drag restores the layout from before it. Nothing ever floats.

// MEMBERS. A manager also tiles windows it does not own (ADR 1792859491 §4.4): a GovernedWindow
// added with AddMember keeps its own node, in an overlay, and is anchored over its tile while it
// wants to be tiled and fits. When the tiled set no longer fits, the most recently tiled member is
// released to its own placement. A member's window is resized by the shared border (Alt-right drag)
// and never moved on its own. The centre is a role: at most one tile or member fills the remainder,
// none need, and with nothing tiled the manager lays out and draws nothing.

// GovernedWindow is a window a tile manager places without owning its node. Govern anchors it over
// rect (widget.AtLive, with a docked fallback: the tile's edge, or a corner for the fill member) and
// gives its core the tile's target (WindowCore.Retarget); Release gives both back. Its own state
// (open, minimized, its float rect, content) stays its own.
type GovernedWindow interface {
	Govern(rect func() (tui.Rect, bool), target any)
	Release()
	WindowBehavior() *WindowCore
}

// TilePlace is where a tile starts and where it may go.
type TilePlace struct {
	Edge  tui.DockEdge   // DockCenter for the fill tile: at most one
	Edges []tui.DockEdge // the edges a re-dock may take; nil: Edge only. DockCenter: it may swap into the centre
	Size  float64        // requested percent of the manager across Edge; ignored at the centre
	Min   int            // cells across its edge (a strip), or on both axes (the centre)
	// Minimizable lets it hide (WithTilesCollector's collector lists it); ignored while it is the centre
	Minimizable bool
}

// TilesChangedEvent is published once per completed operation, never per drag motion. Key is the
// tile whose state changed: for a border drag, the strip that owns the border, whichever tile the
// drag began in (By). A swap publishes one per tile.
type TilesChangedEvent struct {
	Owner             tui.NodeID
	Key, By           string
	Operation         WindowOperation
	Edge              tui.DockEdge
	Size              float64 // the request, never the clamped size: what a program stores
	Hidden, Maximized bool
}

// TilesOption configures a Tiles under construction.
type TilesOption func(*Tiles)

// WithTilesCollector is where a minimizable tile goes while hidden: a program's dock lists it
// there. Without it the manager keeps a private one.
func WithTilesCollector(c WindowCollector) TilesOption { return func(t *Tiles) { t.collector = c } }

// WithTilesOrigin is the manager's origin in its members' overlay frame, read each time a member is
// placed: a member is anchored at its tile's rect offset by it. The default is the zero point, a
// manager at the overlay's origin. A program whose manager sits below other docked content (a decl
// Window's centre) reports where its layout placed it.
func WithTilesOrigin(fn func() tui.Point) TilesOption { return func(t *Tiles) { t.origin = fn } }

// WithTilesChanged has fn told each TilesChangedEvent, as well as the bus.
func WithTilesChanged(fn func(TilesChangedEvent)) TilesOption {
	return func(t *Tiles) { t.onChanged = fn }
}

// Tiles is the tile manager. Its methods are owned by the application loop.
type Tiles struct {
	Base
	tiles     []*Tile // the order strips are taken
	zoomed    *Tile   // the maximized tile: layout only, never a minimize
	collector WindowCollector
	onChanged func(TilesChangedEvent)
	size      tui.Size // as last laid out
	preview   *tilePreview
	pending   *tileOp          // the gesture in progress, staged until it completes
	origin    func() tui.Point // WithTilesOrigin; nil: the zero point
	wantSeq   int              // the last member tiled's stamp: members tile, and release, in this order
}

// Tile is one tile: its content in a WindowMod over the manager.
type Tile struct {
	key    string
	owner  *Tiles
	place  TilePlace  // committed: Edge and Size change by a gesture, SetEdge, SetSize or Swap
	hidden bool       // minimized
	mod    *WindowMod // a Tile child's window; nil for a member
	target *tileTarget
	rect   tui.Rect // where the last layout placed it, manager-local
	// a member's: its window, whether it wants to be tiled (its Layout), when it last asked, and
	// whether it is tiled now
	gov      GovernedWindow
	wants    bool
	wantSeq  int
	governed bool
}

// NewTiles builds an empty manager.
func NewTiles(opts ...TilesOption) *Tiles {
	t := &Tiles{preview: &tilePreview{}}
	for _, o := range opts {
		if o != nil { // a nil option is no option
			o(t)
		}
	}
	if t.collector == nil {
		t.collector = NewMinimizedWindows()
	}
	return t
}

// Add appends a tile, before the manager is mounted. A second centre is refused.
func (t *Tiles) Add(key string, content tui.Component, p TilePlace) *Tile {
	if t.ctx != nil {
		panic(fatalOf("widget: Tiles.Add", "manager already mounted", "declare every tile before mounting"))
	}
	t.admit("widget: Tiles.Add", key, p)
	tile := &Tile{key: key, owner: t, place: p}
	tile.target = &tileTarget{t: tile}
	opts := []WindowModOption{WithWindowTarget(tile.target), WithWindowResize(), WithWindowMove(),
		WithWindowMaximize(), WithWindowKey(key), WithWindowLabel(key)}
	if p.Minimizable {
		opts = append(opts, WithWindowMinimize(t.collector))
	}
	tile.mod = NewWindowMod(content, opts...)
	t.tiles = append(t.tiles, tile)
	return tile
}

// admit refuses a place no tile may take: an invalid edge, a second centre or a key in use.
func (t *Tiles) admit(op, key string, p TilePlace) {
	if p.Edge > tui.DockCenter || slices.ContainsFunc(p.Edges, func(e tui.DockEdge) bool { return e > tui.DockCenter }) {
		panic(fatalOf(op, "invalid edge", key))
	}
	if p.Edge == tui.DockCenter && t.centre() != nil {
		panic(fatalOf(op, "a second centre", "a manager has at most one"))
	}
	if t.tile(key) != nil {
		panic(fatalOf(op, "duplicate tile key", key))
	}
}

// AddMember adds a window the manager tiles while it wants to and fits, before or after the mount.
// p.Edge DockCenter makes it the fill member, whose tile takes the remainder: any number of members,
// at most one centre among them and the tiles.
func (t *Tiles) AddMember(key string, w GovernedWindow, p TilePlace, wants bool) *Tile {
	if nilLike(w) {
		panic(fatalOf("widget: Tiles.AddMember", "nil window", key))
	}
	t.admit("widget: Tiles.AddMember", key, p)
	tile := &Tile{key: key, owner: t, place: p, gov: w}
	tile.target = &tileTarget{t: tile}
	t.tiles = append(t.tiles, tile)
	t.SetWants(key, wants)
	return tile
}

// SetWants is a member's Layout, live: true asks to be tiled, stamping the order it asked in (the
// most recent is released first when the tiled set no longer fits); false releases it.
func (t *Tiles) SetWants(key string, wants bool) {
	tile := t.tile(key)
	if tile == nil || tile.gov == nil || tile.wants == wants {
		return
	}
	tile.wants = wants
	if wants {
		t.wantSeq++
		tile.wantSeq = t.wantSeq
	}
	t.RequestLayout()
	t.MarkDirty()
}

// Wants reports whether a member asks to be tiled; Governed whether it is tiled now.
func (tile *Tile) Wants() bool    { return tile.wants }
func (tile *Tile) Governed() bool { return tile.governed }

// core is a tile's window behaviour: its WindowMod's, or a member's own.
func (tile *Tile) core() *WindowCore {
	if tile.gov != nil {
		return tile.gov.WindowBehavior()
	}
	return tile.mod.Core()
}

// node is the component a tile's focus lives in: its WindowMod, or a member's window content (nil
// before the member's window is bound).
func (tile *Tile) node() tui.Component {
	if tile.gov != nil {
		return tile.core().content
	}
	return tile.mod
}

// inPlay reports whether a tile takes part in the layout: a Tile child always, a member while tiled.
func (tile *Tile) inPlay() bool { return tile.gov == nil || tile.governed }

// Key is the tile's key.
func (tile *Tile) Key() string { return tile.key }

// Window is a Tile child's window: its WindowCore drives every gesture, minimize and maximize. A
// member's is its own (nil here).
func (tile *Tile) Window() *WindowMod { return tile.mod }

// Place is the tile's committed place: its edge and requested size.
func (tile *Tile) Place() TilePlace { return tile.place }

// Hidden reports whether the tile is minimized.
func (tile *Tile) Hidden() bool { return tile.hidden }

// Tile is the tile by key, or nil.
func (t *Tiles) Tile(key string) *Tile { return t.tile(key) }

func (t *Tiles) tile(key string) *Tile {
	for _, tile := range t.tiles {
		if tile.key == key {
			return tile
		}
	}
	return nil
}

func (t *Tiles) centre() *Tile {
	for _, tile := range t.tiles {
		if tile.place.Edge == tui.DockCenter {
			return tile
		}
	}
	return nil
}

// Init mounts every Tile child and the drop preview over them. A member's node is its own.
func (t *Tiles) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	t.pending = nil
	for _, tile := range t.tiles {
		if tile.mod != nil {
			ctx.Mount(tile.mod)
			tile.mod.SetVisible(t.shown(tile))
		}
	}
	ctx.Mount(t.preview) // after the tiles: a parent renders before its children, the last child last
}

// listChildren is the focus walk's view: only the tiles on screen.
func (t *Tiles) listChildren() []tui.Component {
	var out []tui.Component
	for _, tile := range t.tiles {
		if tile.mod != nil && t.shown(tile) {
			out = append(out, tile.mod)
		}
	}
	return out
}

// shown reports whether a tile takes space: in play, not minimized, and the maximized one if any is.
func (t *Tiles) shown(tile *Tile) bool {
	return tile.inPlay() && !tile.hidden && (t.zoomed == nil || t.zoomed == tile)
}

// sync shows exactly the Tile children that take space, and lays out again. A member shows as its
// own window does.
func (t *Tiles) sync() {
	for _, tile := range t.tiles {
		if tile.mod != nil {
			tile.mod.SetVisible(t.shown(tile))
		}
	}
	t.RequestLayout()
	t.MarkDirty()
}

// Layout places every tile where plan puts it. A staged resize is laid out live, so the border
// follows the pointer; a staged drop is only outlined, and moves nothing until it completes.
func (t *Tiles) Layout(c tui.Constraints) tui.Size {
	w, h := boundedMax(c.MaxW, c.MinW), boundedMax(c.MaxH, c.MinH)
	t.size = tui.Size{W: w, H: h}
	t.settle(t.size)
	live := t.pending
	if live != nil && live.resize == nil {
		live = nil
	}
	rects := t.plan(t.size, live)
	for _, tile := range t.tiles {
		r := rects[tile]
		if tile.mod != nil {
			t.ctx.LayoutChild(tile.mod, tui.Tight(tui.Size{W: r.W, H: r.H}))
			t.ctx.PlaceChild(tile.mod, r)
		}
		tile.rect = r
	}
	var at tui.Rect
	if d := t.pending.staged(); d != nil {
		at = d.rect
	}
	t.ctx.LayoutChild(t.preview, tui.Tight(tui.Size{W: at.W, H: at.H}))
	t.ctx.PlaceChild(t.preview, at)
	return c.Constrain(t.size)
}

// Render paints nothing: the tiles and the preview paint themselves.
func (t *Tiles) Render(tui.Surface) {}

// settle tiles the members that fit at size: the wanting ones, earliest asked first, the most
// recently asked released until the rest fit. A member that comes into the set is governed over its
// tile, and one that leaves it is released to its own placement.
func (t *Tiles) settle(size tui.Size) {
	var set []*Tile
	for _, tile := range t.tiles {
		if tile.gov != nil && tile.wants {
			set = append(set, tile)
		}
	}
	slices.SortFunc(set, func(a, b *Tile) int { return a.wantSeq - b.wantSeq })
	for len(set) > 0 && !t.fits(size, set) {
		set = set[:len(set)-1]
	}
	for _, tile := range t.tiles {
		if tile.gov == nil {
			continue
		}
		on := slices.Contains(set, tile)
		switch {
		case on && !tile.governed:
			tile.governed = true
			tile.gov.Govern(tile.overlayRect, tile.target)
		case !on && tile.governed:
			tile.governed = false
			if t.zoomed == tile {
				t.zoomed = nil
			}
			tile.gov.Release()
		}
	}
}

// overlayRect is a governed member's tile in its overlay's frame, as AtLive reads it: false while
// it has no cells or is not tiled.
func (tile *Tile) overlayRect() (tui.Rect, bool) {
	r := tile.rect
	if !tile.governed || r.W <= 0 || r.H <= 0 {
		return tui.Rect{}, false
	}
	if o := tile.owner.origin; o != nil {
		at := o()
		r.X, r.Y = r.X+at.X, r.Y+at.Y
	}
	return r, true
}

// fits reports whether the tiles in play with set as the tiled members keep, at size, every strip
// at or above its Min and the centre at or above its Min: solve's first step, the strips giving
// their cells above Min, is all it may take.
func (t *Tiles) fits(size tui.Size, set []*Tile) bool {
	inPlay := func(tile *Tile) bool { return tile.gov == nil || slices.Contains(set, tile) }
	for _, horizontal := range []bool{true, false} {
		if _, over := t.axis(size, t.tiles, func(tile *Tile) TilePlace { return tile.place }, inPlay, horizontal); over > 0 {
			return false
		}
	}
	return true
}

// plan is where every tile goes in a manager of size, with op's request applied: the strips in
// declaration order, each its clamped depth from the remaining rect, then the centre. A tile that
// takes no space (minimized, or not the maximized one) is an empty rect.
func (t *Tiles) plan(size tui.Size, op *tileOp) map[*Tile]tui.Rect {
	out := make(map[*Tile]tui.Rect, len(t.tiles))
	full := tui.Rect{W: size.W, H: size.H}
	if t.zoomed != nil && !t.zoomed.hidden {
		out[t.zoomed] = full
		return out
	}
	place := func(tile *Tile) TilePlace { return op.placeOf(tile) }
	order := op.order(t.tiles)
	across := t.solve(size, order, place)
	rem := full
	var last *Tile // the last strip placed: it takes the remainder when no centre is tiled
	for _, tile := range order {
		p := place(tile)
		if tile.hidden || !tile.inPlay() || p.Edge == tui.DockCenter {
			continue
		}
		last = tile
		n := across[tile]
		switch p.Edge {
		case tui.DockTop:
			out[tile] = tui.Rect{X: rem.X, Y: rem.Y, W: rem.W, H: n}
			rem.Y, rem.H = rem.Y+n, rem.H-n
		case tui.DockBottom:
			out[tile] = tui.Rect{X: rem.X, Y: rem.Y + rem.H - n, W: rem.W, H: n}
			rem.H -= n
		case tui.DockLeft:
			out[tile] = tui.Rect{X: rem.X, Y: rem.Y, W: n, H: rem.H}
			rem.X, rem.W = rem.X+n, rem.W-n
		case tui.DockRight:
			out[tile] = tui.Rect{X: rem.X + rem.W - n, Y: rem.Y, W: n, H: rem.H}
			rem.W -= n
		}
	}
	centre := false
	for _, tile := range order {
		if place(tile).Edge == tui.DockCenter && tile.inPlay() {
			out[tile], centre = rem, true
		}
	}
	if !centre && last != nil && !rem.Empty() {
		out[last] = union(out[last], rem) // the remainder lies along its inner side
	}
	return out
}

// union is the smallest rect holding a and b.
func union(a, b tui.Rect) tui.Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return tui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// solve is each shown strip's depth across its edge. Per axis (left and right strips share the
// width with the centre, top and bottom ones the height), a strip asks round(Size% of the extent),
// at least its Min. When the strips and the centre's Min don't fit, space is taken back in this
// order until they do: the strips' cells above their Min, last-declared first; then the centre's
// Min; then the strips' Min, last-declared first, down to 0. The centre is the fill, so it gives
// way before a strip drops below the size its content stays legible at. Requests are never
// rewritten: a narrow manager does not overwrite a stored size.
func (t *Tiles) solve(size tui.Size, order []*Tile, place func(*Tile) TilePlace) map[*Tile]int {
	out := make(map[*Tile]int, len(order))
	inPlay := (*Tile).inPlay
	for _, horizontal := range []bool{true, false} {
		strips, over := t.axis(size, order, place, inPlay, horizontal)
		over -= min(max(over, 0), centreMinOf(order, place, inPlay)) // the centre's Min
		for i := len(strips) - 1; i >= 0 && over > 0; i-- {          // the strips' Min
			give := min(over, strips[i].cells)
			strips[i].cells, over = strips[i].cells-give, over-give
		}
		for _, s := range strips {
			out[s.tile] = s.cells
		}
	}
	return out
}

// stripCells is a strip's depth across its edge, as solve takes it.
type stripCells struct {
	tile  *Tile
	cells int
}

// axis is solve's first step on one axis (left and right strips share the width with the centre,
// top and bottom ones the height): each shown strip in play asks round(Size% of the extent), at least
// its Min, and gives back its cells above its Min, last-declared first, while the strips and the
// centre's Min don't fit. over is what is still over after it: more than zero means only taking
// Mins away fits them (fits reads that as no).
func (t *Tiles) axis(size tui.Size, order []*Tile, place func(*Tile) TilePlace, inPlay func(*Tile) bool, horizontal bool) ([]stripCells, int) {
	extent := size.H
	if horizontal {
		extent = size.W
	}
	var strips []stripCells
	var mins []int
	over := centreMinOf(order, place, inPlay) - extent
	for _, tile := range order {
		p := place(tile)
		if tile.hidden || !inPlay(tile) || p.Edge == tui.DockCenter || (p.Edge == tui.DockLeft || p.Edge == tui.DockRight) != horizontal {
			continue
		}
		m := max(p.Min, 0)
		strips = append(strips, stripCells{tile, max(percentCells(p.Size, extent), m)})
		mins = append(mins, m)
		over += strips[len(strips)-1].cells
	}
	for i := len(strips) - 1; i >= 0 && over > 0; i-- { // the strips' cells above their Min
		give := min(over, strips[i].cells-mins[i])
		strips[i].cells, over = strips[i].cells-give, over-give
	}
	return strips, over
}

// centreMinOf is the Min of the centre in play, 0 with none.
func centreMinOf(order []*Tile, place func(*Tile) TilePlace, inPlay func(*Tile) bool) int {
	for _, tile := range order {
		if p := place(tile); p.Edge == tui.DockCenter && inPlay(tile) {
			return max(p.Min, 0)
		}
	}
	return 0
}

// percentCells is pct percent of extent, rounded half up; never negative.
func percentCells(pct float64, extent int) int {
	return max(int(math.Floor(pct*float64(extent)/100+0.5)), 0)
}

// SetEdge re-docks a strip at another of its Edges. The centre never changes edge this way, and
// no strip becomes the centre: that is a Swap.
func (t *Tiles) SetEdge(key string, e tui.DockEdge) bool {
	tile := t.tile(key)
	if tile == nil || e == tui.DockCenter || tile.place.Edge == tui.DockCenter || tile.place.Edge == e || !allows(tile.place, e) {
		return false
	}
	t.cancelGesture()
	tile.place.Edge = e
	t.sync()
	return true
}

// Swap exchanges two Tile children's places (edge, size and order): the only way the centre role
// moves between them. A hidden tile is not swapped, nor one whose new place its Edges don't allow,
// nor a member: a member keeps its own place.
func (t *Tiles) Swap(a, b string) bool {
	ta, tb := t.tile(a), t.tile(b)
	if ta == nil || tb == nil || ta == tb || ta.gov != nil || tb.gov != nil || ta.hidden || tb.hidden || !allows(ta.place, tb.place.Edge) || !allows(tb.place, ta.place.Edge) {
		return false
	}
	t.cancelGesture()
	t.swap(ta, tb)
	t.sync()
	return true
}

func (t *Tiles) swap(a, b *Tile) {
	a.place.Edge, b.place.Edge = b.place.Edge, a.place.Edge
	a.place.Size, b.place.Size = b.place.Size, a.place.Size
	i, j := slices.Index(t.tiles, a), slices.Index(t.tiles, b)
	t.tiles[i], t.tiles[j] = b, a
}

// allows reports whether a tile placed at p may go to edge e.
func allows(p TilePlace, e tui.DockEdge) bool {
	if e == p.Edge {
		return true
	}
	return slices.Contains(p.Edges, e)
}

// SetMin sets the cells a tile keeps across its edge (a strip), or on both axes (the centre): held
// at layout, as the clamp holds every Min.
func (t *Tiles) SetMin(key string, cells int) {
	if tile := t.tile(key); tile != nil && tile.place.Min != max(cells, 0) {
		tile.place.Min = max(cells, 0)
		t.RequestLayout()
	}
}

// SetSize sets a strip's requested size, a percent of the manager across its edge: kept as asked,
// clamped only where laid out.
func (t *Tiles) SetSize(key string, pct float64) {
	if tile := t.tile(key); tile != nil && tile.place.Size != pct && !math.IsNaN(pct) {
		t.cancelGesture()
		tile.place.Size = max(pct, 0)
		t.RequestLayout()
	}
}

// Hide minimizes a tile through its window, the one owner of a tile's hidden state: false for the
// centre, a tile that isn't minimizable, or one already hidden.
func (t *Tiles) Hide(key string) bool {
	tile := t.tile(key)
	return tile != nil && tile.inPlay() && tile.core().Minimize()
}

// Show restores a hidden tile through its window.
func (t *Tiles) Show(key string) bool {
	tile := t.tile(key)
	return tile != nil && tile.inPlay() && tile.core().Restore()
}

// Maximize gives one tile the whole rect, or Restore gives every tile back its place; both through
// the tile's window, so the window's state and its event follow.
func (t *Tiles) Maximize(key string) bool {
	tile := t.tile(key)
	return tile != nil && tile.inPlay() && t.zoomed != tile && tile.core().ToggleMaximize()
}

// Restore ends a maximize.
func (t *Tiles) Restore() bool { return t.zoomed != nil && t.zoomed.core().ToggleMaximize() }

// Zoomed is the maximized tile, or nil.
func (t *Tiles) Zoomed() *Tile { return t.zoomed }

// cancelGesture ends a drag in progress before the policy changes under it.
func (t *Tiles) cancelGesture() {
	if t.pending == nil {
		return
	}
	g := t.pending.tile
	t.pending = nil
	g.core().HandleAction(tui.ActionInvocation{Action: WindowDragCancelAction{}, Origin: tui.OriginProgrammatic})
}

// publish tells the program and the bus.
func (t *Tiles) publishChange(tile *Tile, by *Tile, op WindowOperation) {
	e := TilesChangedEvent{Owner: t.NodeID(), Key: tile.key, By: by.key, Operation: op, Edge: tile.place.Edge,
		Size: tile.place.Size, Hidden: tile.hidden, Maximized: t.zoomed == tile}
	if t.onChanged != nil {
		t.onChanged(e)
	}
	t.publish(e)
}

// ---- a gesture: staged until its window says it completed ----

// tileOp is a gesture's staged effect: a border's new request, or a drop.
type tileOp struct {
	tile   *Tile // the tile the gesture began in
	resize *tileBorder
	size   float64 // resize: the owning strip's new request
	drop   *tileDrop
}

// placeOf is a tile's place with the op applied.
func (op *tileOp) placeOf(tile *Tile) TilePlace {
	p := tile.place
	if op == nil {
		return p
	}
	if op.resize != nil && op.resize.strip == tile {
		p.Size = op.size
	}
	if d := op.drop; d != nil {
		switch {
		case d.swap != nil && tile == op.tile:
			p.Edge, p.Size = d.swap.place.Edge, d.swap.place.Size
		case d.swap != nil && tile == d.swap:
			p.Edge, p.Size = op.tile.place.Edge, op.tile.place.Size
		case d.swap == nil && tile == op.tile:
			p.Edge = d.edge
		}
	}
	return p
}

// order is the declaration order with the op applied: a swap exchanges the two tiles' places in it.
func (op *tileOp) order(tiles []*Tile) []*Tile {
	if op == nil || op.drop == nil || op.drop.swap == nil {
		return tiles
	}
	out := slices.Clone(tiles)
	i, j := slices.Index(out, op.tile), slices.Index(out, op.drop.swap)
	out[i], out[j] = out[j], out[i]
	return out
}

func (op *tileOp) staged() *tileDrop {
	if op == nil {
		return nil
	}
	return op.drop
}

// tileBorder is a border a tile shares, from the last layout's rects.
type tileBorder struct {
	side  Handle // which side of the tile the gesture began in
	strip *Tile  // the strip whose inner side it is: it owns the border, its request changes
	outer int    // that strip's outer coordinate, where its depth is measured from
}

// tileDrop is where a move would land.
type tileDrop struct {
	edge tui.DockEdge // re-dock to this edge, or
	swap *Tile        // exchange places with this tile
	rect tui.Rect     // where the dragged tile would land: the preview
}

// sharedBorders are the borders tile shares with a neighbour in the given rects: each side lying on
// a shown strip's inner side, and a strip's own inner side when anything is beyond it. A pointer
// gesture reads the placed rects, the ones under the pointer (placedRects); a keyboard step reads
// the requested ones (plan), since steps before a layout build on each other.
func (t *Tiles) sharedBorders(tile *Tile, rects map[*Tile]tui.Rect) []tileBorder {
	r := rects[tile]
	if r.Empty() {
		return nil
	}
	var out []tileBorder
	for _, side := range []Handle{HandleLeft, HandleRight, HandleTop, HandleBottom} {
		line := sideOf(r, side)
		if tile.place.Edge == innerEdge(side) && line > 0 && line < t.extent(side) {
			out = append(out, tileBorder{side: side, strip: tile, outer: outerOf(r, tile.place.Edge)})
			continue
		}
		for _, s := range t.tiles {
			sr := rects[s]
			if s == tile || !t.shown(s) || s.place.Edge != outerEdge(side) || sr.Empty() {
				continue
			}
			if sideOf(sr, opposite(side)) == line && overlaps(r, sr, side) {
				out = append(out, tileBorder{side: side, strip: s, outer: outerOf(sr, s.place.Edge)})
				break
			}
		}
	}
	return out
}

// placedRects are where the last layout placed every tile.
func (t *Tiles) placedRects() map[*Tile]tui.Rect {
	out := make(map[*Tile]tui.Rect, len(t.tiles))
	for _, tile := range t.tiles {
		out[tile] = tile.rect
	}
	return out
}

// sideOf is the coordinate of a rect's side: its left, right (exclusive), top or bottom (exclusive).
func sideOf(r tui.Rect, side Handle) int {
	switch side {
	case HandleLeft:
		return r.X
	case HandleRight:
		return r.X + r.W
	case HandleTop:
		return r.Y
	}
	return r.Y + r.H
}

func opposite(side Handle) Handle {
	switch side {
	case HandleLeft:
		return HandleRight
	case HandleRight:
		return HandleLeft
	case HandleTop:
		return HandleBottom
	}
	return HandleTop
}

// innerEdge is the edge of a strip whose inner side is a tile's side: a left strip's inner side is
// its right side.
func innerEdge(side Handle) tui.DockEdge {
	switch side {
	case HandleRight:
		return tui.DockLeft
	case HandleLeft:
		return tui.DockRight
	case HandleBottom:
		return tui.DockTop
	}
	return tui.DockBottom
}

// outerEdge is the edge of a strip on the other side of a tile's side: across a tile's left side
// lies a left strip.
func outerEdge(side Handle) tui.DockEdge {
	switch side {
	case HandleLeft:
		return tui.DockLeft
	case HandleRight:
		return tui.DockRight
	case HandleTop:
		return tui.DockTop
	}
	return tui.DockBottom
}

// outerOf is a strip's outer coordinate: where its Dock offer started.
func outerOf(r tui.Rect, e tui.DockEdge) int {
	switch e {
	case tui.DockLeft:
		return r.X
	case tui.DockRight:
		return r.X + r.W
	case tui.DockTop:
		return r.Y
	}
	return r.Y + r.H
}

// overlaps reports whether two rects meeting on a vertical (left/right) or horizontal side share
// some of it.
func overlaps(a, b tui.Rect, side Handle) bool {
	if side == HandleLeft || side == HandleRight {
		return max(a.Y, b.Y) < min(a.Y+a.H, b.Y+b.H)
	}
	return max(a.X, b.X) < min(a.X+a.W, b.X+b.W)
}

// extent is the manager's size along a side's axis.
func (t *Tiles) extent(side Handle) int {
	if side == HandleLeft || side == HandleRight {
		return t.size.W
	}
	return t.size.H
}

// dropAt is where a move with the pointer at p would land; nil for nowhere. A swap with the tile
// under p wins; else a band within a quarter of the manager from a side whose edge the tile may
// take, the nearer side at a corner and left/right on a tie.
func (t *Tiles) dropAt(tile *Tile, p tui.Point) *tileDrop {
	for _, o := range t.tiles {
		if o != tile && o.gov == nil && t.shown(o) && o.rect.Contains(p.X, p.Y) && allows(tile.place, o.place.Edge) && allows(o.place, tile.place.Edge) {
			return &tileDrop{swap: o, rect: o.rect}
		}
	}
	if tile.place.Edge == tui.DockCenter { // the centre moves only by a swap
		return nil
	}
	best, bestDist := tui.DockCenter, math.MaxInt
	for _, e := range []tui.DockEdge{tui.DockLeft, tui.DockRight, tui.DockTop, tui.DockBottom} {
		if e == tile.place.Edge || !allows(tile.place, e) {
			continue
		}
		var dist, band int
		switch e {
		case tui.DockLeft:
			dist, band = p.X, t.size.W/4
		case tui.DockRight:
			dist, band = t.size.W-1-p.X, t.size.W/4
		case tui.DockTop:
			dist, band = p.Y, t.size.H/4
		case tui.DockBottom:
			dist, band = t.size.H-1-p.Y, t.size.H/4
		}
		if dist >= 0 && dist < band && dist < bestDist {
			best, bestDist = e, dist
		}
	}
	if best == tui.DockCenter {
		return nil
	}
	d := &tileDrop{edge: best}
	d.rect = t.plan(t.size, &tileOp{tile: tile, drop: d})[tile]
	return d
}

// ---- the tile's window target ----

// tileTarget is a tile's window target: geometry, zoom and visibility owned by the manager, and
// the handle and gesture facets that make the window gestures tiling operations.
type tileTarget struct {
	t *Tile
	// the gesture in progress, from GestureBegan
	op      WindowOperation
	border  *tileBorder
	pointer bool
	at      tui.Point // the press, manager-local
	start   tui.Rect  // the tile's rect at the press
}

func (g *tileTarget) owner() *Tiles { return g.t.owner }

func (g *tileTarget) Bounds() tui.Rect { return g.t.rect }

func (g *tileTarget) Area() tui.Rect { return tui.Rect{W: g.owner().size.W, H: g.owner().size.H} }

// RequestedBounds is the rect the requests give, the staged gesture's first: never the placed rect,
// so a second keyboard step before a layout builds on the first.
func (g *tileTarget) RequestedBounds() tui.Rect {
	m := g.owner()
	return m.plan(m.size, m.pending)[g.t]
}

// SetBounds stages a gesture's motion: a resize's new border, or a move's drop.
func (g *tileTarget) SetBounds(r tui.Rect) bool {
	m := g.owner()
	switch {
	case g.op == WindowResize && g.border != nil:
		b := g.border
		depth := sideOf(r, b.side) - b.outer
		if depth < 0 {
			depth = -depth
		}
		extent := m.extent(b.side)
		if extent <= 0 {
			return false
		}
		pct := 100 * float64(min(depth, extent)) / float64(extent)
		if cur := m.pending; cur != nil && cur.resize != nil && cur.size == pct {
			return false
		}
		if m.pending == nil && b.strip.place.Size == pct {
			return false
		}
		m.pending = &tileOp{tile: g.t, resize: b, size: pct}
	case g.op == WindowMove && g.pointer:
		p := tui.Point{X: g.at.X + r.X - g.start.X, Y: g.at.Y + r.Y - g.start.Y}
		d := m.dropAt(g.t, p)
		if prev := m.pending.staged(); (prev == nil && d == nil) || (prev != nil && d != nil && *prev == *d) {
			return false
		}
		if d == nil {
			m.pending = &tileOp{tile: g.t} // staged, landing nowhere: a release changes nothing
		} else {
			m.pending = &tileOp{tile: g.t, drop: d}
		}
	default:
		return false
	}
	m.RequestLayout()
	m.MarkDirty()
	return true
}

// Checkpoint restores what a gesture changes, every tile's edge and requested size, the order and
// the zoom, and drops the staged gesture. A tile's Min is a live constraint, not a placement: one
// set while a drag is held (SetMin, a minimumSize binding) survives the drag's cancellation.
func (g *tileTarget) Checkpoint() func() {
	m := g.owner()
	type placed struct {
		edge tui.DockEdge
		size float64
	}
	places := make(map[*Tile]placed, len(m.tiles))
	for _, tile := range m.tiles {
		places[tile] = placed{tile.place.Edge, tile.place.Size}
	}
	order, zoomed := slices.Clone(m.tiles), m.zoomed
	return func() {
		for tile, p := range places {
			tile.place.Edge, tile.place.Size = p.edge, p.size
		}
		m.tiles, m.zoomed, m.pending = order, zoomed, nil
		m.sync()
	}
}

// HandleAt is the side of the shared border nearest p; ties go to left/right, then to the owning
// strip's declaration order.
func (g *tileTarget) HandleAt(p tui.Point) (Handle, bool) {
	m := g.owner()
	best, bestDist, bestRank := Handle(0), math.MaxInt, math.MaxInt
	found := false
	for _, b := range m.sharedBorders(g.t, m.placedRects()) {
		line := sideOf(g.t.rect, b.side)
		dist := p.Y - line
		vertical := b.side == HandleLeft || b.side == HandleRight
		if vertical {
			dist = p.X - line
		}
		if dist < 0 {
			dist = -dist
		}
		rank := slices.Index(m.tiles, b.strip)
		if !vertical {
			rank += len(m.tiles)
		}
		if dist < bestDist || (dist == bestDist && rank < bestRank) {
			best, bestDist, bestRank, found = b.side, dist, rank, true
		}
	}
	return best, found
}

// StepHandle is the shared side a keyboard step moves: a vertical one for a width step, a
// horizontal one for a height step; a strip's own inner side first, else the first-declared owner.
func (g *tileTarget) StepHandle(dw, dh int) (Handle, bool) {
	m := g.owner()
	vertical := dw != 0
	var pick *tileBorder
	for _, b := range m.sharedBorders(g.t, m.plan(m.size, nil)) {
		if (b.side == HandleLeft || b.side == HandleRight) != vertical {
			continue
		}
		b := b
		switch {
		case b.strip == g.t:
			return b.side, true
		case pick == nil || slices.Index(m.tiles, b.strip) < slices.Index(m.tiles, pick.strip):
			pick = &b
		}
	}
	if pick == nil {
		return 0, false
	}
	return pick.side, true
}

// GestureBegan records the gesture: a resize needs a shared border on the handle's side, in the
// placed rects for the pointer and the requested ones for a keyboard step; a move is the pointer's
// only (a keyboard move has no drop to aim). A refusal changes nothing: a drag in progress keeps
// what it staged.
func (g *tileTarget) GestureBegan(op WindowOperation, h Handle, at tui.Point, pointer bool) bool {
	m := g.owner()
	var border *tileBorder
	switch op {
	case WindowResize:
		rects := m.placedRects()
		if !pointer {
			rects = m.plan(m.size, nil)
		}
		for _, b := range m.sharedBorders(g.t, rects) {
			if b.side == h {
				border = &b
				break
			}
		}
		if border == nil {
			return false
		}
	case WindowMove:
		if !pointer || g.t.gov != nil { // a member is never moved on its own: Layout › Float takes it out
			return false
		}
	default:
		return false
	}
	g.op, g.border, g.pointer, g.at, g.start = op, border, pointer, at, g.t.rect
	m.pending = nil
	return true
}

// GestureChanged reports whether the staged gesture changes the committed policy: a new request,
// or a drop, which always changes an edge or the order, even when it lands in the rect it began in.
func (g *tileTarget) GestureChanged() bool {
	op := g.owner().pending
	switch {
	case op == nil:
		return false
	case op.resize != nil:
		return op.size != op.resize.strip.place.Size
	}
	return op.drop != nil
}

// GestureLost keeps a resize's reached border, as WindowCore keeps reached geometry, and drops a
// move's staged drop: no re-dock the user never released on.
func (g *tileTarget) GestureLost(op WindowOperation) {
	m := g.owner()
	if op == WindowResize && g.GestureChanged() {
		g.commit(op)
		return
	}
	m.pending = nil
	m.sync()
}

// WindowChanged is the window's completed operation (WindowChangeHandler), a Tile child's or a
// member's alike: a gesture's staged effect is committed; a maximize, minimize or restore is reported.
func (g *tileTarget) WindowChanged(e WindowChangedEvent) {
	switch e.Operation {
	case WindowMove, WindowResize:
		g.commit(e.Operation)
	default:
		g.owner().publishChange(g.t, g.t, e.Operation)
	}
}

// commit applies the staged gesture and publishes it, once.
func (g *tileTarget) commit(op WindowOperation) {
	m := g.owner()
	p := m.pending
	m.pending = nil
	if p == nil || p.tile != g.t {
		m.sync()
		return
	}
	switch {
	case p.resize != nil:
		p.resize.strip.place.Size = p.size
		m.sync()
		m.publishChange(p.resize.strip, g.t, op)
	case p.drop != nil && p.drop.swap != nil:
		other := p.drop.swap
		m.swap(g.t, other)
		m.sync()
		m.publishChange(g.t, g.t, op)
		m.publishChange(other, g.t, op)
	case p.drop != nil:
		g.t.place.Edge = p.drop.edge
		m.sync()
		m.publishChange(g.t, g.t, op)
	default:
		m.sync()
	}
}

// ToggleMaximize gives the tile the whole rect, or gives every tile back its place.
func (g *tileTarget) ToggleMaximize() bool {
	m := g.owner()
	if m.zoomed == g.t {
		m.zoomed = nil
	} else {
		m.zoomed = g.t
		if ctx := m.ctx; ctx != nil {
			for _, tile := range m.tiles {
				if n, to := tile.node(), g.t.node(); tile != g.t && n != nil && to != nil && ctx.FocusWithin(n) {
					_ = focusFirst(ctx, to)
				}
			}
		}
	}
	m.sync()
	return true
}

func (g *tileTarget) Maximized() bool { return g.owner().zoomed == g.t }

func (g *tileTarget) Modal() bool { return false }

func (g *tileTarget) WindowVisible() bool { return !g.t.hidden }

// SetWindowVisible is the primitive the window's minimize and restore apply: layout state only,
// never a call back into the window. The centre never hides.
func (g *tileTarget) SetWindowVisible(v bool) bool {
	m := g.owner()
	if g.t.hidden == !v || (!v && g.t.place.Edge == tui.DockCenter) {
		return false
	}
	if !v && m.zoomed == g.t {
		m.zoomed = nil
	}
	g.t.hidden = !v
	m.sync()
	return true
}

// ---- the drop preview ----

// tilePreview outlines where a staged drop would land. It is the manager's last child, so it paints
// over the tiles, and it holds no point: the pointer goes to the tiles beneath.
type tilePreview struct{ Base }

func (p *tilePreview) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }

func (p *tilePreview) Render(s tui.Surface) {
	drawBorder(s, s.Size(), style.New().Foreground(style.TokenBorderFocused))
}

func (p *tilePreview) ContainsPointer(x, y int) bool { return false }
