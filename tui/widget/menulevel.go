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
	size := cs.Constrain(tui.Size{W: w + 2, H: h + 2}) // +2 for the frame
	if ctx := p.Context(); ctx != nil {
		p.owner.declareRows(ctx, rows, tui.Rect{X: 1, Y: 1, W: size.W - 2, H: size.H - 2})
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
	p.owner.paintRows(s, p.rowsOf(), tui.Rect{X: 1, Y: 1, W: sz.W - 2, H: sz.H - 2})
}

// measureRows is the natural size of a list of rows: the widest row, and one
// line each for the visible ones.
func (m *Menu) measureRows(rows []MenuItemModel) (w, h int) {
	for _, i := range visibleRows(rows) {
		w = max(w, m.rowWidth(rows[i]))
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
func (m *Menu) declareRows(ctx *tui.Context, rows []MenuItemModel, area tui.Rect) {
	if m.rowRects == nil {
		m.rowRects = make(map[ItemID]tui.Rect)
	}
	y := area.Y
	for _, i := range visibleRows(rows) {
		r := tui.Rect{X: area.X, Y: y, W: area.W, H: 1}
		ctx.DeclareRegion(tui.RegionID(rows[i].ID), r)
		m.rowRects[rows[i].ID] = r
		y++
	}
}

// paintRows draws each visible row into its line of area.
func (m *Menu) paintRows(s tui.Surface, rows []MenuItemModel, area tui.Rect) {
	y := area.Y
	for _, i := range visibleRows(rows) {
		it := rows[i]
		m.paintRow(s, it, tui.Rect{X: area.X, Y: y, W: area.W, H: 1}, m.rowStateOf(it))
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
		w, h := 0, 1
		x := 0
		for _, i := range visibleRows(m.items) {
			rw := m.rowWidth(m.items[i]) + 2
			r := tui.Rect{X: x, Y: 0, W: rw, H: 1}
			if ctx != nil {
				ctx.DeclareRegion(tui.RegionID(m.items[i].ID), r)
			}
			m.rowRects[m.items[i].ID] = r
			x += rw
		}
		w = x
		return cs.Constrain(tui.Size{W: w, H: h})
	}

	w, h := m.measureRows(m.items)
	size := cs.Constrain(tui.Size{W: w + 2, H: h})
	if ctx != nil {
		m.declareRows(ctx, m.items, tui.Rect{X: 0, Y: 0, W: size.W, H: size.H})
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
		for _, i := range visibleRows(m.items) {
			it := m.items[i]
			m.paintRow(s, it, m.rowRects[it.ID], m.rowStateOf(it))
		}
		return
	}
	m.paintRows(s, m.items, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
}

// openLevel mounts one submenu level, anchored to the row that opened it.
//
// Opening a level while a DEEPER one is open closes the deeper ones first: a
// cascade is a stack, and leaving an orphan hanging off a row whose sibling was
// just opened is a state the model does not have.
func (m *Menu) openLevel(parent ItemID, children []MenuItemModel) error {
	ctx := m.Context()
	if ctx == nil {
		return nil // not mounted: nothing to open onto, and nothing to report
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

	popup := &menuPopup{owner: m, parent: parent, st: m.style}
	// The layer id namespaces the row under the owning Menu's node, so two menus
	// with the same ItemID in their models cannot collide on the host.
	id := LayerID(fmt.Sprintf("menu:%d:%s", ctx.ID(), parent))
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
	if !spec.Ref.Valid() {
		return nil // the row has not been laid out yet; nothing to anchor to
	}
	ctx.Bus().Publish(anchoredOpenEvent{
		id: id, layer: popup, spec: spec, policy: m.policy,
	})
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
	ctx := m.Context()
	for j := len(m.levels) - 1; j >= i; j-- {
		if ctx != nil {
			ctx.Bus().Publish(anchoredCloseEvent{
				id: m.levels[j].layer, reason: DismissProgrammatic,
			})
		}
	}
	m.levels = m.levels[:i]
	if ctx != nil {
		ctx.MarkDirty()
	}
}
