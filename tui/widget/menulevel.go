package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// LEVELS AND GEOMETRY.
//
// The root level is the Menu itself. Every nested level is a menuPopup mounted
// on the overlay host and anchored to the row that opened it.
//
// Exactly two functions mount or unmount a level. No field is ever assigned nil
// to "close" one: that is how a mounted child gets orphaned, still in the tree,
// still painting, with nothing left holding a reference to close it.

// menuPopup is one open submenu level: a framed list of rows belonging to the
// Menu that opened it.
//
// It paints its owner's rows rather than owning a model of its own, so there is
// one model and one selection however deep the cascade goes.
type menuPopup struct {
	Base
	owner  *Menu
	parent ItemID
	st     *MenuStyle
	// rects is where this level's rows were placed by the last committed
	// layout, in THIS popup's local coordinates.
	rects map[ItemID]tui.Rect
}

// Init installs the owner's resolver against THIS level's rects.
//
// A level is a separate node, so the runtime delivers its pointer events here
// rather than to the Menu — and without a resolver of its own a submenu's rows
// would simply not be clickable, which is exactly what happened before this
// existed. The resolver and the handler both delegate to the owner, so there is
// one state machine for the whole cascade rather than one per level.
func (p *menuPopup) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(func(ev tui.Event) (tui.Action, bool) {
		return p.owner.resolveIn(ev, p.rects)
	}))
	// THE LEVEL IS GONE WHEN THIS NODE IS GONE, and the owner learns it HERE —
	// synchronously, on the unmount itself, whoever caused it.
	//
	// The Menu used to reconcile through OverlayDismissedEvent instead, which is
	// published on the program lane: CloseAnchored unmounts the popup and
	// returns, and until that event drains the Menu still counts a level that no
	// longer exists. In the same turn, reopening the row then hit the
	// already-deepest early return and mounted nothing while reporting success.
	//
	// An unmount hook covers every path rather than the one the host mediates:
	// an explicit CloseAnchored, the anchor-loss commit, the host's own
	// teardown. The bus event stays, for observers rather than for this.
	ctx.OnUnmount(func() { p.owner.dropLevelFor(p) })
}

// HandleAction runs the owner's machine, telling it that THIS node is the one
// whose handler is executing — which is the node that may take or release the
// pointer capture.
func (p *menuPopup) HandleAction(inv tui.ActionInvocation) bool {
	return p.owner.handleIn(inv, p.Context())
}

// HandleEvent forwards to the owner, and the capture-loss case is why it must.
//
// A gesture started in a level is CAPTURED BY THAT LEVEL — only the node whose
// handler is running may take the pointer — so the runtime delivers the loss
// here, not to the Menu. Without this the Menu never learns, its pressed and
// armed state survives a capture it no longer holds, and the next release
// activates a row the user stopped pointing at.
func (p *menuPopup) HandleEvent(ev tui.Event) bool {
	return p.owner.HandleEvent(ev)
}

// rowsOf returns the rows this level shows, read from the owner's live model so
// a mutation is reflected without the popup holding a stale copy.
func (p *menuPopup) rowsOf() []MenuItemModel {
	if it := findItem(p.owner.items, p.parent); it != nil {
		return it.Children
	}
	return nil
}

// AcceptsFocus reports that a level is not a tab stop. The Menu owns the
// selection and the keys for the whole cascade; a focusable popup would insert a
// stop that steals the arrow keys from the widget driving them.
func (p *menuPopup) AcceptsFocus() bool { return false }

// Layout sizes the level to its rows and declares a region per row, so a deeper
// level can anchor to one of them.
func (p *menuPopup) Layout(cs tui.Constraints) tui.Size {
	rows := p.rowsOf()
	w, h := p.owner.measureRows(rows)
	// THE TITLE IS PART OF THE WIDTH. Sized to the rows alone, a level whose
	// opening row has a longer name than anything inside it would drop its own
	// title for want of two cells.
	if t := p.titleText(); t != "" {
		w = max(w, p.owner.measure(t)+2) // a space either side of the name
	}
	size := cs.Constrain(tui.Size{W: w + 2, H: h + 2}) // +2 for the frame
	if p.rects == nil {
		p.rects = make(map[ItemID]tui.Rect)
	}
	clear(p.rects) // per-pass, like the runtime's own declared regions
	if ctx := p.Context(); ctx != nil {
		p.owner.declareRows(ctx, rows, p.interior(size), p.rects)
	}
	return size
}

// Render paints the frame and the rows.
func (p *menuPopup) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", p.st.Surface())
	drawBorder(s, sz, p.st.Border())
	p.paintTitle(s, sz)
	p.owner.paintRows(s, p.rowsOf(), p.interior(sz))
}

// paintTitle writes the OPENING ROW'S LABEL into the popup's top border.
//
// A dropdown that names the category it came from stays readable once it has
// been torn off the bar or stacked beside a sibling: the frame says "File"
// rather than leaving four verbs floating over the document. It is also what
// makes a level legible as a window in its own right, which is the shape a
// floating or detached menu needs.
//
// Skipped when the frame is too narrow to hold the name AND its spaces, rather
// than clipping to something half-legible: a truncated title on a frame is
// harder to read than no title.
func (p *menuPopup) paintTitle(s tui.Surface, sz tui.Size) {
	title := p.titleText()
	if title == "" {
		return
	}
	// The last WRITABLE cell is sz.W-2: the frame owns column sz.W-1. An
	// exclusive limit of sz.W-2 is one short and clips the title's trailing
	// space against the corner, which reads as the text running into the frame.
	limit := sz.W - 1
	if s.StringWidth(title)+2 > limit {
		return
	}
	x := 1
	s.SetCell(x, 0, " ", p.st.Border())
	x++
	for cluster := range tui.Graphemes(title) {
		w := s.StringWidth(cluster)
		if x+w > limit {
			break
		}
		s.SetCell(x, 0, cluster, p.st.Selected())
		x += w
	}
	if x < limit {
		s.SetCell(x, 0, " ", p.st.Border())
	}
}

// titleText is the name this level shows on its frame, or "" when it shows
// none. One source for the width and the paint, so a level cannot reserve room
// for a title it then declines to draw.
func (p *menuPopup) titleText() string {
	if !p.owner.levelTitles {
		return ""
	}
	if it := findItem(p.owner.items, p.parent); it != nil {
		return it.Label
	}
	return ""
}

// interior is the rect inside the frame, never negative.
//
// A popup constrained below the frame's own two cells would otherwise hand a
// negative width or height down as an area, and a negative extent is not a
// smaller rectangle — it is one whose arithmetic silently inverts every bound
// computed from it.
func (p *menuPopup) interior(size tui.Size) tui.Rect {
	return tui.Rect{X: 1, Y: 1, W: max(size.W-2, 0), H: max(size.H-2, 0)}
}

// depthOfRow is which level a row is DISPLAYED IN: 0 for the menu's own rows,
// i+1 for a row shown inside the popup of levels[i].
//
// Not the same as "how deep in the model", because only the levels actually
// open are on screen — and it is the screen the cascade has to agree with.
func (m *Menu) depthOfRow(id ItemID) (int, bool) {
	for i := range m.items {
		if m.items[i].ID == id {
			return 0, true
		}
	}
	for i, lv := range m.levels {
		it := findItem(m.items, lv.parent)
		if it == nil {
			continue
		}
		for j := range it.Children {
			if it.Children[j].ID == id {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// measureRows is the natural size of a list of rows: the widest row, and one
// line each for the visible ones.
func (m *Menu) measureRows(rows []MenuItemModel) (w, h int) {
	for _, i := range visibleRows(rows) {
		w = max(w, m.rowWidth(rows[i], true))
		h++
	}
	return w, h
}

// declareRows records one anchor region per visible row, in the declaring node's
// local coordinates, and remembers the rects for hit-testing.
//
// ONE PASS produces both, which is the point: the region a submenu anchors to
// and the rect a click is mapped through are the same rectangle, so they cannot
// disagree about where a row is.
// BOUNDED BY THE AREA. A row below the last line the parent gave this level is
// not on screen: the surface clips the paint, but a region declared for it is
// still a live anchor and a rect still hit-tests. A Menu squeezed to one line
// would therefore open a submenu hanging off a row nobody can see, and a click
// in the space below would arm one. "Not painted" and "not addressable" have to
// be the same condition, so both loops stop at the same line.
func (m *Menu) declareRows(ctx *tui.Context, rows []MenuItemModel, area tui.Rect, into map[ItemID]tui.Rect) {
	y := area.Y
	bottom := area.Y + max(area.H, 0)
	for _, i := range visibleRows(rows) {
		if y >= bottom || area.W <= 0 {
			return
		}
		r := tui.Rect{X: area.X, Y: y, W: area.W, H: 1}
		ctx.DeclareRegion(tui.RegionID(rows[i].ID), r)
		into[rows[i].ID] = r
		y++
	}
}

// paintRows draws each visible row into its line of area, stopping at the same
// line declareRows does so that what is painted and what is addressable are one
// set of rows rather than two.
func (m *Menu) paintRows(s tui.Surface, rows []MenuItemModel, area tui.Rect) {
	y := area.Y
	bottom := area.Y + max(area.H, 0)
	for _, i := range visibleRows(rows) {
		if y >= bottom || area.W <= 0 {
			return
		}
		it := rows[i]
		m.paintRow(s, it, tui.Rect{X: area.X, Y: y, W: area.W, H: 1}, m.rowStateOf(it), true)
		y++
	}
}

// Layout lays the ROOT level out: vertically by default, along one line when a
// MenuBar has set it horizontal.
func (m *Menu) Layout(cs tui.Constraints) tui.Size {
	ctx := m.Context()
	if m.rowRects == nil {
		m.rowRects = make(map[ItemID]tui.Rect)
	}
	// Cleared each pass for the same reason the runtime clears declared
	// regions: a rect from a previous layout describes a row that may no longer
	// be there, and hit-testing through it would arm something invisible.
	clear(m.rowRects)

	if m.horizontal {
		// The ceiling is the constrained width, and a row beyond it is not on
		// screen — so it declares nothing and gets no rect, for the same reason
		// a clipped row of a vertical menu does not. A row that starts inside
		// and runs past the edge is TRUNCATED rather than dropped: it has
		// painted cells, so it is addressable through the cells it has.
		limit := cs.MaxW // tui.Unbounded means the parent imposed no ceiling
		place := func(id ItemID, r tui.Rect) {
			if ctx != nil {
				ctx.DeclareRegion(tui.RegionID(id), r)
			}
			m.rowRects[id] = r
		}

		// PEGGED ROWS ARE PLACED FIRST, from the right edge inward, so the
		// leading run knows how much room it actually has. Doing it the other
		// way round means discovering the overlap only after the left-hand rows
		// are already placed, and then either moving them or letting Help sit
		// on top of the row before it.
		//
		// Pegging needs a known edge, so with no ceiling there is nothing to peg
		// TO: those rows join the ordinary run rather than vanishing.
		right := limit
		if limit != tui.Unbounded {
			for _, i := range reverseVisible(m.items) {
				it := m.items[i]
				if !it.PegRight {
					continue
				}
				rw := m.rowWidth(it, m.barMarkers) + 2
				if rw > right {
					rw = right // it is the only thing that fits; clip it
				}
				if rw <= 0 {
					continue
				}
				right -= rw
				place(it.ID, tui.Rect{X: right, Y: 0, W: rw, H: 1})
			}
		}

		x := 0
		for _, i := range visibleRows(m.items) {
			it := m.items[i]
			if it.PegRight && limit != tui.Unbounded {
				continue // already placed against the right edge
			}
			rw := m.rowWidth(it, m.barMarkers) + 2
			if limit != tui.Unbounded {
				// Stop at the pegged block rather than at the screen edge, so a
				// leading row cannot be painted underneath Help.
				if x >= right {
					break
				}
				rw = min(rw, right-x)
			}
			place(it.ID, tui.Rect{X: x, Y: 0, W: rw, H: 1})
			x += rw
		}
		// The bar claims the FULL width when something is pegged to its far end:
		// returning only the used width would leave the right-hand rows outside
		// the rect the parent placed, where they are clipped away entirely.
		w := x
		if limit != tui.Unbounded && right < limit {
			w = limit
		}
		return cs.Constrain(tui.Size{W: w, H: 1})
	}

	w, h := m.measureRows(m.items)
	size := cs.Constrain(tui.Size{W: w + 2, H: h})
	if ctx != nil {
		m.declareRows(ctx, m.items, tui.Rect{X: 0, Y: 0, W: size.W, H: size.H}, m.rowRects)
	}
	return size
}

// Render paints the root level.
func (m *Menu) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", m.style.Surface())
	if m.horizontal {
		// ONLY the rows layout actually placed. A row past the bar's edge has no
		// entry, and indexing the map for it yields the ZERO rect — which paints
		// nothing, but still called a consumer's RowRenderer with an empty
		// rectangle. A row this package has declared unaddressable must not
		// execute consumer rendering code: layout, hit-testing, anchoring and
		// painting have to agree on which rows exist. Model order is kept, so
		// the paint order stays deterministic.
		for _, i := range visibleRows(m.items) {
			it := m.items[i]
			r, ok := m.rowRects[it.ID]
			if !ok || r.W <= 0 || r.H <= 0 {
				continue
			}
			m.paintRow(s, it, r, m.rowStateOf(it), m.barMarkers)
		}
		return
	}
	m.paintRows(s, m.items, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
}

// anchorHost is the overlay operation set a Menu needs from the container that
// will hold its levels.
//
// An INTERFACE rather than *OverlayHost, so the lookup states exactly what it
// requires and a consumer can supply a host of their own without this package
// naming their type.
type anchorHost interface {
	OpenAnchored(id LayerID, layer tui.Component, spec AnchorSpec, pol AnchorPolicy) error
	CloseAnchored(id LayerID, reason DismissReason)
}

// layerHost is the UNKEYED pair a plain popup needs — a Select's dropdown, which
// has no id and no anchor.
//
// Its methods are unexported, which is a statement rather than an oversight: the
// layers it mounts are this package's own private types, so only this package's
// OverlayHost can hold one. A consumer CAN implement anchorHost above and serve
// a Menu; there is nothing useful they could do with this one.
type layerHost interface {
	addLayer(layer tui.Component)
	removeLayer(layer tui.Component)
}

// hostFor resolves the nearest enclosing ancestor satisfying T.
//
// ONE rule about which host a widget belongs to — nearest enclosing, never a
// sibling, never a broadcast — shared by every widget that owns a layer, with
// the interface saying what that widget actually needs from it.
func hostFor[T any](ctx *tui.Context) (T, bool) {
	var zero T
	if ctx == nil {
		return zero, false
	}
	found := ctx.Ancestor(func(c tui.Component) bool {
		_, ok := c.(T)
		return ok
	})
	h, ok := found.(T)
	return h, ok
}

// host resolves the ONE container that will hold this Menu's levels: the
// nearest enclosing overlay host.
//
// This replaced an app-wide bus broadcast, and the difference is ownership. The
// broadcast had no addressee, so every mounted OverlayHost in the application
// received each request: the one containing the Menu satisfied it and all the
// others refused a request that was never theirs, each publishing a failure
// event. An application watching those events to detect a broken menu therefore
// saw one every time a menu opened correctly. Worse, the request was
// ASYNCHRONOUS — the Menu appended its logical level immediately and found out
// later, or never, whether anything had been mounted.
//
// Resolved on each use rather than cached at mount: the answer is live tree
// state, and a Menu can legitimately be re-parented between one open and the
// next.
func (m *Menu) host() anchorHost {
	h, _ := hostFor[anchorHost](m.Context())
	return h
}

// openLevel mounts one submenu level, anchored to the row that opened it.
//
// A TRANSACTION, in this order: resolve the host, check the anchor, mount, and
// only then record the level and move the selection. The old order recorded
// first and asked afterwards, so a request that mounted nothing still left a
// level in the model — and because opening an already-open row is idempotent,
// that stale entry made the NEXT open look like a duplicate and silently do
// nothing. The row became permanently unopenable.
//
// Opening a level while a DEEPER one is open closes the deeper ones first: a
// cascade is a stack, and leaving an orphan hanging off a row whose sibling was
// just opened is a state the model does not have.
func (m *Menu) openLevel(parent ItemID, children []MenuItemModel) error {
	ctx := m.Context()
	if ctx == nil {
		return fmt.Errorf("%w: the menu is not mounted", ErrAnchorUnusable)
	}
	// Already the deepest level? Opening it again is what a second click on the
	// same row means, and it should not rebuild anything.
	if n := len(m.levels); n > 0 && m.levels[n-1].parent == parent {
		return nil
	}
	for i, lv := range m.levels {
		if lv.parent == parent {
			m.closeLevelsFrom(i + 1)
			return nil
		}
	}
	// A NEW LEVEL REPLACES EVERYTHING BELOW ITS OWN ROW.
	//
	// Opening used to simply append, so a level stayed on screen when the user
	// moved to a row that had nothing to do with it: clicking File and then Help
	// left File's dropdown open beside Help's, and a third click added a third.
	// Which rows are on screen then disagrees with which rows the model says are
	// open, and the arrow keys drive one cascade while the user is looking at
	// another.
	//
	// The row's own depth decides what goes: a root row replaces the whole
	// cascade, a row inside the first dropdown keeps that dropdown and replaces
	// anything deeper. A row that is nowhere in the visible cascade closes
	// nothing, since there is no depth to truncate to.
	if d, ok := m.depthOfRow(parent); ok {
		m.closeLevelsFrom(d)
	}
	host := m.host()
	if host == nil {
		return fmt.Errorf("%w: a Menu must be mounted inside an OverlayHost to open a level",
			ErrAnchorUnusable)
	}

	// A nested level always cascades to the side; only a bar's FIRST level
	// opens away from the bar's own edge.
	side := PlacementRight
	if m.horizontal && len(m.levels) == 0 {
		side = m.dropSide
	}
	spec := AnchorSpec{
		Ref:  m.anchorFor(ctx, parent),
		Pref: Placement{Side: side},
	}
	// A row that has not been laid out — or has been clipped out of the rect its
	// parent allowed — declared no region, so there is nothing to anchor to. An
	// ERROR rather than a silent nil: the caller asked for a popup, and telling
	// them nothing happened is how they end up looking for one that was never
	// going to appear.
	if !spec.Ref.Valid() {
		return fmt.Errorf("%w: row %q is not currently laid out", ErrAnchorUnusable, parent)
	}

	popup := &menuPopup{owner: m, parent: parent, st: m.style}
	// The layer id namespaces the row under the owning Menu's node, so two menus
	// with the same ItemID in their models cannot collide on the host.
	id := LayerID(fmt.Sprintf("menu:%d:%s", ctx.ID(), parent))
	if err := host.OpenAnchored(id, popup, spec, m.policy); err != nil {
		return err // nothing recorded: the model still describes what is mounted
	}

	m.levels = append(m.levels, menuOpenLevel{parent: parent, layer: id, popup: popup})
	// The new level owns the selection: its first selectable row.
	for _, it := range children {
		if it.selectable() {
			m.setSelected(it.ID)
			break
		}
	}
	return nil
}

// dropLevelFor brings the model into line with a popup that has just been
// unmounted. The layer is already gone; this is the model catching up.
//
// Deeper levels go with it: a level hangs off a row of the one above, so a level
// whose parent has been dismissed is anchored to something that is no longer on
// screen.
//
// IDEMPOTENT BY CONSTRUCTION. closeLevelsFrom truncates the model before it
// closes anything, so a Menu-initiated close reaches this hook with the level
// already gone and the search below simply finds nothing — which is what lets
// one path serve both the Menu closing a level and the host closing it
// underneath.
//
// THIS popup is dropped from the model WITHOUT a host call, and that is not an
// optimisation. Its node is mid-teardown: it is still registered and still
// reports as mounted — the runtime clears that only after the hooks have run —
// so asking the host to close it again re-enters the unmount cascade on the very
// node whose hook list is being consumed, and the second pass reads a hook the
// first has already taken. Deeper levels DO go through the host: nothing is
// tearing those down yet.
func (m *Menu) dropLevelFor(p *menuPopup) {
	for i := range m.levels {
		if m.levels[i].popup != p {
			continue
		}
		m.closeLevelsFrom(i + 1)
		m.levels = m.levels[:i]
		if ctx := m.Context(); ctx != nil {
			ctx.MarkDirty()
		}
		return
	}
}

// anchorFor returns the anchor for a row: the region the owning level declared.
//
// The row may belong to the root (declared by the Menu) or to an open level
// (declared by that popup), and the anchor has to come from whichever node
// declared it — a region is scoped to its declaring node, so asking the wrong
// one yields nothing.
func (m *Menu) anchorFor(ctx *tui.Context, id ItemID) tui.AnchorRef {
	for _, lv := range m.levels {
		if rowsContain(lv.popup.rowsOf(), id) {
			if pctx := lv.popup.Context(); pctx != nil {
				return pctx.RegionAnchor(tui.RegionID(id))
			}
		}
	}
	return ctx.RegionAnchor(tui.RegionID(id))
}

// rowsContain reports whether a level's own rows include id, without descending:
// a row belongs to exactly one level.
func rowsContain(rows []MenuItemModel, id ItemID) bool {
	for i := range rows {
		if rows[i].ID == id {
			return true
		}
	}
	return false
}

// closeLevelsFrom closes level i and every level below it, deepest first.
//
// THE ONE UNMOUNT PATH for a level. Deepest first, so each close sees a cascade
// that is valid above it, and every entry is removed from the slice by this
// function rather than by a caller that might forget one.
func (m *Menu) closeLevelsFrom(i int) {
	if i < 0 || i >= len(m.levels) {
		return
	}
	// The model is truncated FIRST, so anything reached during the close sees a
	// Menu that already excludes these levels. The bus is not that path — a
	// dismissal is delivered on the program lane, so the Menu's own
	// reconciliation cannot run inside this call — but a popup's unmount hooks
	// do run synchronously, and the ordering costs nothing to get right.
	closing := append([]menuOpenLevel(nil), m.levels[i:]...)
	m.levels = m.levels[:i]

	ctx := m.Context()
	if host := m.host(); host != nil {
		for j := len(closing) - 1; j >= 0; j-- {
			// A popup the runtime has ALREADY unmounted is skipped. Two paths
			// reach here that way: the host's own teardown, where the cascade
			// takes the layers before it reaches the Menu, and this function
			// re-entered from a popup's unmount hook. Asking the host to close
			// it again would unmount a node that is gone — a panic — and during
			// a cascade it would edit the child list being walked.
			if ctx != nil && !ctx.MountedComponent(closing[j].popup) {
				continue
			}
			host.CloseAnchored(closing[j].layer, DismissProgrammatic)
		}
	}
	if ctx != nil {
		ctx.MarkDirty()
	}
}

// closeAllOnUnmount is the Menu's teardown: a level must not outlive the widget
// that opened it.
//
// The levels are the HOST's children, not the Menu's, so the runtime's own
// unmount cascade does not reach them — unmounting the Menu used to leave its
// popups mounted and its model still counting them. Registered as an unmount
// hook so the cleanup is synchronous and cannot be missed by a caller who
// removed the Menu without calling Close.
//
// A layer that is ALREADY unmounted is left alone. When the host itself is being
// torn down its layers have already gone, and asking it to close them again
// would mutate the child list the cascade is walking.
func (m *Menu) closeAllOnUnmount() {
	m.closeLevelsFrom(0)
	m.pressed, m.armed = "", false
	m.releaseCapture()
}
