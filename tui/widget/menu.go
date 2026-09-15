package widget

import (
	"fmt"
	"slices"

	"github.com/yongjohnlee80/golib/tui"
)

// MENU.
//
// A Menu owns a MODEL and paints it. Its rows are values, not mounted children,
// so the runtime cannot hit-test them, cannot arm them, and cannot tell one from
// another: every row shares the Menu's single NodeID. Menu therefore does three
// things a leaf widget does not have to.
//
//  1. It DECLARES A REGION per visible row during Layout, which is what lets a
//     submenu anchor to the row that opened it and what its own pointer
//     resolver maps coordinates through.
//  2. It runs its OWN press-arm / release-activate machine, driven by the named
//     actions in menuactions.go rather than by raw event handling, so the mouse,
//     the keyboard, a UserEvent and DoAction all end at one activation path.
//  3. It owns the open submenu levels as anchored overlay layers, opened and
//     closed through exactly one pair of functions, so no field is ever assigned
//     nil to "close" and leave a mounted child orphaned.
//
// A Menu is the ROOT LEVEL, rendered vertically. Nested levels are popups it
// opens on the overlay host. MenuBar replaces only the root level's layout.

// Menu is a model-driven menu: a vertical list of rows, with cascading submenu
// levels opened as anchored overlays.
type Menu struct {
	Base

	items    []MenuItemModel
	style    *MenuStyle
	rows     RowRenderer
	exec     func(tui.ActionInvocation) bool
	onSelect func(ItemID)
	policy   AnchorPolicy

	// selected is the highlighted row, empty when nothing is.
	selected ItemID
	// pressed and armed are the pointer gesture. pressed is the row the press
	// landed on and does not change for the life of the gesture; armed says
	// whether the pointer is currently back over it, and is what a release
	// checks. Two fields rather than one, because moving off a row and back on
	// must re-arm, which a single "armed row" cannot express.
	pressed ItemID
	armed   bool

	// levels is the open submenu chain, outermost first. Each entry is a
	// mounted popup; openLevel and closeLevel are the only things that change
	// this slice or the mounts it tracks.
	levels []menuOpenLevel

	// rowRects is where each ROOT row was placed by the last committed layout,
	// in the Menu's own local coordinates. Each open level keeps its own map,
	// because a popup's rows are in the POPUP's frame — one shared map would
	// mix two coordinate systems and hit-test the wrong row.
	rowRects map[ItemID]tui.Rect

	// captureCtx is the node that took the pointer for the gesture in progress.
	// Remembered because a press can land on the root or on any open level, and
	// only the node that took a capture may release it.
	captureCtx *tui.Context

	// horizontal is set by MenuBar, which lays the root level along an edge
	// instead of down the side. It changes layout and the arrow keys, nothing
	// else — there is one lifecycle, not two.
	horizontal bool
	// barMarkers is whether a submenu row in the ROOT level of a horizontal bar
	// draws its "▸". Off by default: in a bar every top-level entry opens a
	// dropdown, so a marker on each one repeats what the bar already is and adds
	// two cells to every entry. Inside a popup the marker distinguishes the rows
	// that cascade from the rows that act, so there it is always drawn.
	barMarkers bool
	// levelTitles is whether a dropdown names the row that opened it, in its
	// own top border. On by default: a level that says where it came from
	// stays readable beside a sibling and reads as a window of its own, which
	// is what a detached or floating menu needs.
	levelTitles bool
	// levelMinWidth is the smallest interior a dropdown may have, in columns.
	// Keeps a cascade of short categories from looking ragged, one narrow box
	// per level.
	levelMinWidth int
	// dropSide is where a bar's first level opens, set by MenuBar so a bottom
	// bar drops upward rather than back across itself. Zero means "the default
	// for this orientation", which is what a bare Menu wants.
	dropSide PlacementSide

	pointerPolicy tui.PointerPolicy
}

// menuOpenLevel is one open submenu: the row that opened it and the layer id it
// was registered under.
type menuOpenLevel struct {
	parent ItemID
	layer  LayerID
	popup  *menuPopup
}

// MenuOption configures a Menu at construction.
type MenuOption func(*Menu)

// NewMenu builds an empty menu. Supply a model with SetModel.
func NewMenu(opts ...MenuOption) *Menu {
	m := &Menu{levelTitles: true, levelMinWidth: defaultLevelMinWidth, pointerPolicy: tui.PointerInherit}
	for _, o := range opts {
		if o != nil {
			o(m)
		}
	}
	return m
}

// WithMenuStyle associates a style. The Menu does not own it; several menus may
// share one.
func WithMenuStyle(s *MenuStyle) MenuOption {
	return func(m *Menu) { m.style = s }
}

// WithAnchorPolicy sets how submenu levels are placed when the preferred side
// does not fit. nil selects FlipClipPolicy.
func WithAnchorPolicy(p AnchorPolicy) MenuOption {
	return func(m *Menu) {
		if nilLike(p) {
			m.policy = nil // the host substitutes its default
			return
		}
		m.policy = p
	}
}

// defaultLevelMinWidth is the interior every dropdown gets at least.
//
// Chosen rather than derived, because there is nothing to derive it FROM: the
// widest row of a level says how much space that level needs, not how much it
// should have. Twelve columns is about four short words, which is where a
// dropdown stops reading as a box that happens to be wide enough for its
// longest verb and starts reading as a menu.
const defaultLevelMinWidth = 12

// WithLevelMinWidth sets the smallest interior a dropdown may have, in columns.
// Zero removes the floor, sizing every level to its own content.
//
// The alternative — one width shared by every level, taken from the widest —
// is NOT what this does: a single long row in one submenu would then stretch
// every other dropdown in the menu to match it.
func WithLevelMinWidth(cols int) MenuOption {
	return func(m *Menu) {
		if cols < 0 {
			panic(fatalOf("widget: WithLevelMinWidth",
				"a negative width is not a narrower box; zero removes the floor",
				itoa(cols)))
		}
		m.levelMinWidth = cols
	}
}

// WithLevelTitle decides whether a dropdown names the row that opened it in
// its top border. On by default.
func WithLevelTitle(v bool) MenuOption {
	return func(m *Menu) { m.levelTitles = v }
}

// WithBarSubmenuMarker decides whether a horizontal bar's own rows draw the
// submenu arrow. Off by default; popup rows always draw it.
//
// A bar entry that opens a dropdown is the ordinary case rather than the
// exceptional one, so marking every entry says nothing and costs two cells
// each. It is an option rather than a fixed rule because a bar mixing entries
// that open with entries that act directly does need the distinction.
func WithBarSubmenuMarker(v bool) MenuOption {
	return func(m *Menu) { m.barMarkers = v }
}

// WithRowRenderer supplies a custom row painter — the declared extension seam
// for appearances the closed ItemKind does not provide.
//
// A nil OR TYPED-NIL renderer is stored as absent, so the built-in painter runs.
// Normalising here rather than at the paint site means one stored value can be
// trusted everywhere it is read, instead of every reader repeating the check.
func WithRowRenderer(r RowRenderer) MenuOption {
	return func(m *Menu) {
		if nilLike(r) {
			m.rows = nil
			return
		}
		m.rows = r
	}
}

// WithOnSelectionChanged registers a callback for selection movement. It runs
// synchronously; the bus also carries MenuSelectionChangedEvent for observers
// that would rather not be coupled to construction.
func WithOnSelectionChanged(fn func(ItemID)) MenuOption {
	return func(m *Menu) { m.onSelect = fn }
}

// WithActionExecutor supplies the function a row's Action is handed to.
//
// THE DISPATCH SEAM, and it has to exist: a row's Action has nowhere else to go.
// Context.DoAction is addressed to the calling node and does not bubble, and a
// Menu cannot implement an arbitrary application's ActionHandler after
// construction.
//
// Menu stores only the function and NEVER supplies it a Context. It is called
// synchronously and never after the Menu unmounts. A closure may still capture
// arbitrary application values and the consumer owns their lifetime — prefer a
// long-lived controller or store to capturing another component's Context.
func WithActionExecutor(fn func(tui.ActionInvocation) bool) MenuOption {
	return func(m *Menu) { m.exec = fn }
}

// SetModel replaces the whole model, atomically.
//
// ONE TRANSITION, in this exact order, with no intermediate state any render can
// observe:
//
//  1. Validate and DEEP-COPY the complete new model before touching anything.
//  2. Keep an open level only when its row still names a visible, enabled
//     submenu; close every other level and all of its descendants.
//  3. Keep the selection only for a still-selectable row; otherwise fall back
//     to the first selectable one.
//  4. Request layout. Rows absent from the new model are simply not redeclared,
//     and any popup anchored to one is dismissed by the host's anchor-loss
//     commit.
//
// No generation bump is involved: a stable ItemID IS stable region identity, so
// surviving levels stay associated without one. Invalidating anchors here would
// close the very levels step 2 exists to preserve.
//
// On error NOTHING changes — not the model, not the open levels, not the
// selection. Failure is returned rather than panicked because model data is
// frequently externally sourced, and a malformed feed is an ordinary outcome.
func (m *Menu) SetModel(items []MenuItemModel) error {
	if err := validateItems(items, map[ItemID]bool{}, "items"); err != nil {
		return err
	}
	next := copyItems(items)

	// Close the levels the new model cannot justify, before the model changes,
	// so closeLevel still sees the tree it was opened against.
	for i := len(m.levels) - 1; i >= 0; i-- {
		row := findItem(next, m.levels[i].parent)
		if row == nil || row.Kind != ItemKindSubmenu || !row.Visible || !row.Enabled {
			m.closeLevelsFrom(i)
			break
		}
	}

	m.items = next
	m.repairSelection()
	if ctx := m.Context(); ctx != nil {
		ctx.RequestLayout()
	}
	return nil
}

// Model returns a deep copy, so a caller cannot reach inside a mounted Menu by
// writing through the slice it was handed.
func (m *Menu) Model() []MenuItemModel { return copyItems(m.items) }

// SetEnabled sets a row's availability and reports whether the row exists.
// Repairs the selection if it was resting on a row that just became unusable.
func (m *Menu) SetEnabled(id ItemID, v bool) bool {
	return m.mutate(id, func(it *MenuItemModel) { it.Enabled = v })
}

// SetVisible sets a row's presence and reports whether the row exists.
//
// Visibility is presentation: a hidden row KEEPS its checked state, because
// hiding a checked option does not uncheck it, and a caller that meant to
// uncheck it has SetChecked.
func (m *Menu) SetVisible(id ItemID, v bool) bool {
	return m.mutate(id, func(it *MenuItemModel) { it.Visible = v })
}

// SetChecked sets a check or radio row and reports whether the row exists.
// Setting a radio clears the rest of its group, at every depth.
func (m *Menu) SetChecked(id ItemID, v bool) bool {
	return m.mutate(id, func(it *MenuItemModel) {
		it.Checked = v
		if v && it.Kind == ItemKindRadio {
			clearGroupExcept(m.items, it.Group, it.ID)
		}
	})
}

// mutate applies fn to one row and repairs everything the change can invalidate.
// One place, so no mutator forgets the repair — which is how a selection ends up
// resting on a row that is no longer there.
func (m *Menu) mutate(id ItemID, fn func(*MenuItemModel)) bool {
	it := findItem(m.items, id)
	if it == nil {
		return false
	}
	fn(it)
	m.closeUnjustifiedLevels()
	m.repairSelection()
	if ctx := m.Context(); ctx != nil {
		ctx.RequestLayout()
	}
	return true
}

// closeUnjustifiedLevels closes the deepest run of levels whose opening row no
// longer qualifies, from the shallowest such level down.
func (m *Menu) closeUnjustifiedLevels() {
	for i := 0; i < len(m.levels); i++ {
		row := findItem(m.items, m.levels[i].parent)
		if row == nil || row.Kind != ItemKindSubmenu || !row.Visible || !row.Enabled {
			m.closeLevelsFrom(i)
			return
		}
	}
}

// Selected reports the highlighted row, and whether anything is selected.
func (m *Menu) Selected() (ItemID, bool) {
	return m.selected, m.selected != ""
}

// Select moves the highlight to a row, and reports whether it could. A row that
// is hidden, disabled or a separator cannot hold the selection.
func (m *Menu) Select(id ItemID) bool {
	it := findItem(m.items, id)
	if it == nil || !it.selectable() {
		return false
	}
	m.setSelected(id)
	return true
}

// setSelected moves the selection and announces it once, to the callback and the
// bus. One place, so the two cannot come to disagree about when a change
// happened, and a no-op move announces nothing.
func (m *Menu) setSelected(id ItemID) {
	if m.selected == id {
		return
	}
	m.selected = id
	if m.onSelect != nil {
		m.onSelect(id)
	}
	if ctx := m.Context(); ctx != nil {
		ctx.Bus().Publish(MenuSelectionChangedEvent{Owner: m.NodeID(), ItemID: id})
		ctx.MarkDirty()
	}
}

// repairSelection keeps the selection on a still-selectable row, or moves it to
// the first selectable row of the deepest open level.
func (m *Menu) repairSelection() {
	level := m.currentLevelItems()
	if it := findItem(level, m.selected); it != nil && it.selectable() {
		return
	}
	for i := range level {
		if level[i].selectable() {
			m.setSelected(level[i].ID)
			return
		}
	}
	m.setSelected("")
}

// currentLevelItems is the row slice the keyboard is currently acting on: the
// deepest open level, or the root.
func (m *Menu) currentLevelItems() []MenuItemModel {
	if n := len(m.levels); n > 0 {
		if row := findItem(m.items, m.levels[n-1].parent); row != nil {
			return row.Children
		}
	}
	return m.items
}

// OpenLevels reports how many submenu levels are open.
func (m *Menu) OpenLevels() int { return len(m.levels) }

// Open opens the submenu named by id.
//
// Typed errors rather than silence: a caller asking for a row that is not a
// submenu, or is disabled, hidden or absent, has a bug, and returning nil would
// leave it looking for a popup that was never going to appear.
//
// Two further refusals match [ErrAnchorUnusable] rather than
// [ErrInvalidMenuModel], because the model is fine and the SITUATION is not:
// there is no [OverlayHost] above this Menu to hold the level, or the row is
// not currently laid out — it has not been measured yet, or the rect its parent
// allowed clipped it away, so there is no region to anchor to. Both leave the
// Menu exactly as it was.
func (m *Menu) Open(id ItemID) error {
	it := findItem(m.items, id)
	switch {
	case it == nil:
		return fmt.Errorf("%w: no row %q", ErrInvalidMenuModel, id)
	case it.Kind != ItemKindSubmenu:
		return fmt.Errorf("%w: row %q is a %s, not a submenu", ErrInvalidMenuModel, id, it.Kind)
	case !it.Visible:
		return fmt.Errorf("%w: row %q is hidden", ErrInvalidMenuModel, id)
	case !it.Enabled:
		return fmt.Errorf("%w: row %q is disabled", ErrInvalidMenuModel, id)
	}
	return m.openLevel(id, it.Children)
}

// Close closes every open level and leaves the selection on the root.
func (m *Menu) Close() {
	m.closeLevelsFrom(0)
	m.repairSelection()
}

// WithStyle associates a style at runtime and returns the menu for chaining.
// nil reverts to the default look, so there is no separate clear API. Open
// levels are restyled too — a theme swap that reached only the root would leave
// a cascade half-dressed.
func (m *Menu) WithStyle(s *MenuStyle) *Menu {
	m.style = s
	for _, lv := range m.levels {
		lv.popup.st = s
		if ctx := lv.popup.Context(); ctx != nil {
			ctx.MarkDirty()
		}
	}
	if ctx := m.Context(); ctx != nil {
		ctx.MarkDirty()
	}
	return m
}

// WithPointerPolicy sets whether this menu and its subtree accept pointer input,
// and returns the menu for chaining.
//
// Remembered as well as applied, because NewMenu(...).WithPointerPolicy(...) is
// the natural way to write it and runs before there is any Context. Keyboard
// operation is unaffected: every action this widget resolves is reachable from
// the keyboard, so a pointer-disabled menu stays fully usable.
func (m *Menu) WithPointerPolicy(p tui.PointerPolicy) *Menu {
	if !p.Valid() {
		panic(tuiFatal("widget: Menu.WithPointerPolicy",
			"value outside PointerInherit, PointerEnabled, PointerDisabled", int(p)))
	}
	m.pointerPolicy = p
	if ctx := m.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
	return m
}

// AcceptsFocus reports that a menu is a tab stop whenever it has a row worth
// landing on. An empty menu is not: it would be a stop where nothing happens.
func (m *Menu) AcceptsFocus() bool {
	return slices.ContainsFunc(m.items, func(it MenuItemModel) bool { return it.selectable() })
}

// Init installs the menu's own key and pointer bindings as its DEFAULT resolver
// layer, so a consumer may add bindings without re-supplying these.
func (m *Menu) Init(ctx *tui.Context) {
	m.Base.Init(ctx)
	ctx.SetPointerPolicy(m.pointerPolicy)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(m.resolve))

	// A level lives on the host, not under this node, so the runtime's unmount
	// cascade does not reach it: without this hook, removing a Menu left its
	// popups mounted and its model still counting them.
	ctx.OnUnmount(m.closeAllOnUnmount)

	m.repairSelection()
}
