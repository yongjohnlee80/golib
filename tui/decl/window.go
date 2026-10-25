package decl

import (
	"fmt"
	"slices"
	"unicode"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE WINDOW — the top-level surface a document mounts into.
//
// It is a real container, mounting golib's OverlayHost as its child, rather
// than the OverlayHost itself. That is the Go editor's own shape — its App
// mounts the host the same way — and it is what puts the Window on the path
// every key bubbles up, which three things need:
//
//   - Shortcut: a sequence must fire whichever widget holds focus;
//   - menu access keys: Alt+F opens File, F10 goes to the bar, as in Qt;
//   - focus: `focus: true` says where the keyboard starts, and it must come
//     back there when a menu action finishes or Escape leaves the menu.
//
// It mounts its child rather than embedding it. Embedding would make every
// OverlayHost method calling its own receiver bypass anything overridden here —
// Go has no virtual dispatch — and a mounted child keeps every capability it
// has without this type forwarding a single interface.

// windowNode is the component a `Window` declaration builds.
type windowNode struct {
	ctx  *tui.Context
	host *widget.OverlayHost
	dock *tui.Dock

	// focus is the document's `focus: true` target, or nil; focusPending is
	// that it waits for the first layout to be applied.
	focus        tui.Component
	focusPending bool
	minimum      tui.Size // Window.minimumWidth / minimumHeight, cells
	shortcuts    []*shortcutNode
	menus        []*menuBarNode
	overlaid     []Overlaid
	// slots are the reserving Drawers' strips (drawer_reserve.go), pinned after the Window's own
	// docked children
	slots map[*drawerNode]*reserveSlot

	// color is Qt's Window.color, painted under everything; unset leaves the
	// terminal's own background wherever no widget paints.
	color   style.Color
	colored bool

	// sink is where an OpensWithWindow child's error goes (the adapter's error sink)
	sink func(error)
}

func buildWindow(b Build) (tui.Component, []string, error) {
	w := &windowNode{sink: b.sink}
	if err := w.arrange(b.Children, b.ChildAttached, b.FocusNominee); err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	return w, nil, nil
}

// arrange sorts the Window's children into what each is: keys, menus,
// dialogs, and the widgets its dock lays out.
//
// It runs at construction and again whenever a reload changes the children
// (the adapter restructures a Window through it), and the second time it
// DIFFS the dock rather than rebuilding it: a child that is still there keeps
// its mount — its state, its subscriptions, the keyboard — and only what came
// or went is mounted or unmounted. That is what lets an edit to one menu leave
// the editor beside it untouched.
func (w *windowNode) arrange(children []tui.Component, attached []map[string]qml.SpecValue, nominee tui.Component) error {
	kids, err := classifyWindowChildren(children, attached)
	if err != nil {
		return err
	}
	// each reserving Drawer's strip, after the document's own children: the Dock consumes its pinned
	// children in order, so the strips are between the bars
	items := append(kids.items, w.reserveSlots(kids.overlaid)...)
	w.syncDock(items)
	w.shortcuts, w.menus, w.overlaid, w.focus = kids.shortcuts, kids.menus, kids.overlaid, nominee
	for _, m := range w.menus {
		m.leave = func() { w.restoreFocus(); m.endVisit() }
	}
	for _, o := range w.overlaid {
		o.SetOverlay(w.host, w.afterOverlay)
	}
	if w.ctx != nil { // a reload's: at construction the host is not mounted yet, and Init opens them
		w.openWithWindow()
	}
	return nil
}

// openWithWindow opens every overlaid child that is shown for as long as the Window is
// (OpensWithWindow). Each is idempotent, so a reload opens again only what it added.
func (w *windowNode) openWithWindow() {
	for _, o := range w.overlaid {
		ow, ok := o.(OpensWithWindow)
		if !ok {
			continue
		}
		if err := ow.OpenWithWindow(); err != nil && w.sink != nil {
			w.sink(err)
		}
	}
}

// docked is a child of the Window's dock and the edge it is pinned to.
type docked struct {
	comp tui.Component
	edge tui.DockEdge
}

// windowChildren are a Window's children by what each is: keys, menu bars, what opens over the
// Window, and what its dock lays out (a menu bar among them).
type windowChildren struct {
	shortcuts []*shortcutNode
	menus     []*menuBarNode
	overlaid  []Overlaid
	items     []docked
}

// classifyWindowChildren sorts a Window's children, reading each docked one's Dock.edge.
func classifyWindowChildren(children []tui.Component, attached []map[string]qml.SpecValue) (windowChildren, error) {
	var kids windowChildren
	for i, child := range children {
		switch c := child.(type) {
		case menuRow:
			return windowChildren{}, fmt.Errorf("a %s belongs inside a MenuBar, not directly in a Window", c.row().kind)
		case *shortcutNode:
			// Keys, not layout: a Shortcut takes no place on the screen.
			kids.shortcuts = append(kids.shortcuts, c)
			continue
		case Overlaid:
			// Not layout either: a Dialog, a Popup, opens over the Window.
			kids.overlaid = append(kids.overlaid, c)
			continue
		case *menuBarNode:
			kids.menus = append(kids.menus, c)
		}
		var own map[string]qml.SpecValue
		if i < len(attached) {
			own = attached[i]
		}
		edge, _, err := dockEdgeOf(own)
		if err != nil {
			return windowChildren{}, err
		}
		kids.items = append(kids.items, docked{child, edge})
	}
	return kids, nil
}

// dockEdgeProp is the attached property that pins a Window's child to an edge of its dock.
const dockEdgeProp = "Dock.edge"

// dockEdgeOf is the edge a child's attached Dock.edge pins it to, and whether it wrote one. A child
// that wrote none is the dock's centre.
func dockEdgeOf(attached map[string]qml.SpecValue) (tui.DockEdge, bool, error) {
	v, pinned := attached[dockEdgeProp]
	if !pinned {
		return tui.DockCenter, false, nil
	}
	e, err := dockEdges.read(v)
	if err != nil {
		return tui.DockCenter, true, fmt.Errorf("%s: %w", dockEdgeProp, err)
	}
	return e, true, nil
}

// syncDock makes the dock hold items, in order. The first time it builds the dock and the overlay
// host over it. After, it DIFFS: a child that is still there keeps its mount, and only what came or
// went is pinned or removed.
func (w *windowNode) syncDock(items []docked) {
	if w.dock == nil {
		w.dock = tui.NewDock()
		// layout-safe: the slot's rectangle is recorded for its Drawer's Float, placed after the Dock
		// in the same pass
		w.dock.OnPlaced(func(c tui.Component, r tui.Rect) {
			if s, ok := c.(*reserveSlot); ok {
				s.d.slot = r
			}
		})
		for _, it := range items {
			w.dock.Pin(it.edge, it.comp)
		}
		w.host = widget.NewOverlayHost(w.dock)
		return
	}
	keep := map[tui.Component]bool{}
	for _, it := range items {
		keep[it.comp] = true
	}
	// a snapshot: removing shifts the dock's children under a live range, which would skip the
	// one after each removal
	for _, c := range slices.Collect(w.dock.Children()) {
		if !keep[c] {
			w.dock.Remove(c)
		}
	}
	present := map[tui.Component]bool{}
	for _, c := range w.dock.All() {
		present[c] = true
	}
	for _, it := range items {
		if !present[it.comp] {
			w.dock.Pin(it.edge, it.comp)
		}
	}
	for i, it := range items {
		w.dock.Move(it.comp, i)
	}
}

func (w *windowNode) Init(ctx *tui.Context) {
	w.ctx = ctx
	if w.minimum != (tui.Size{}) {
		w.applyMinimum()
	}
	ctx.Mount(w.host)
	// what is shown for as long as the Window is opens now, its host mounted, so a `focus: true`
	// inside it is there for the first layout's restoreFocus
	w.openWithWindow()
	// A menu row that ran closes the menu. Focus goes back where the document
	// said it lives; left on the closed menu, the next keystroke would go
	// nowhere the user can see.
	tui.SubscribeScoped(ctx, func(ev widget.MenuActivatedEvent) {
		if ev.Handled {
			w.restoreFocus()
		}
	})
	// The document's `focus: true` is applied once the screen is laid out:
	// focus moves INTO the nominee, and that needs its parts placed.
	w.focusPending = true
}

// afterOverlay runs when a dialog or popup the Window hosts has closed. The
// runtime has already given focus back to where it was when the dialog opened
// — Qt's rule, and the one the user relies on: About opened from the explorer
// closes back into the explorer. Only a close that left nothing focused (the
// control that had it is gone) falls back to the document's `focus: true`.
func (w *windowNode) afterOverlay() {
	if w.ctx != nil && w.ctx.FocusWithin(w) {
		return
	}
	w.restoreFocus()
}

func (w *windowNode) restoreFocus() {
	// NOT WHILE A DIALOG IS UP. A menu row that opened one finishes after it
	// has, and taking the keyboard back to the editor then would leave the
	// dialog on screen with nothing able to answer it.
	if w.host.TopModal() != nil {
		return
	}
	if w.focus != nil && w.ctx != nil {
		// INTO the nominee, not onto it: `focus: true` on a ListView names a
		// view whose focusable part is the list inside it — as a Qt Item with
		// focus is given it within its scope. FocusComponent takes only a
		// component that is focusable itself, and did nothing for one that is
		// not.
		w.ctx.FocusInto(w.focus)
	}
}

func (w *windowNode) Layout(c tui.Constraints) tui.Size {
	win := c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH}) // the Window a reserving Drawer's size is a percentage of
	for _, s := range w.slots {
		s.win = win
	}
	sz := w.ctx.LayoutChild(w.host, c)
	w.ctx.PlaceChild(w.host, tui.Rect{W: sz.W, H: sz.H})
	if w.focusPending {
		w.focusPending = false
		w.ctx.AfterLayout(windowFocusKey, w.restoreFocus)
	}
	return sz
}

// windowFocusKey is the Window's one commit: the start-up focus.
const windowFocusKey tui.CommitKey = "window.focus"

// Render paints the Window's color, when it has one, over its whole surface:
// what no widget covers — the screen either side of a Frame narrower than it,
// say — then shows the theme's colour rather than the terminal's.
func (w *windowNode) Render(s tui.Surface) {
	if !w.colored {
		return
	}
	sz := s.Size()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", style.New().Background(w.color))
}

// setColor sets Qt's Window.color.
// setMinimumWidth and setMinimumHeight are Qt's Window.minimumWidth and minimumHeight, in cells.
// The App holds the minimum; before the Window is mounted it waits here for Init.
func (w *windowNode) setMinimumWidth(n int)  { w.minimum.W = max(n, 0); w.applyMinimum() }
func (w *windowNode) setMinimumHeight(n int) { w.minimum.H = max(n, 0); w.applyMinimum() }

func (w *windowNode) applyMinimum() {
	if w.ctx != nil {
		w.ctx.App().SetMinimumSize(w.minimum)
	}
}

func (w *windowNode) setColor(c style.Color) {
	if w.colored && w.color == c {
		return
	}
	w.color, w.colored = c, true
	if w.ctx != nil {
		w.ctx.MarkDirty()
	}
}

// HandleEvent sees every key the focused widget did not consume.
//
// The document's own Shortcuts are tried first, so a document can claim any
// sequence — including one the menu would otherwise take.
func (w *windowNode) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.FocusEvent); ok {
		// A FocusEvent bubbles up from whichever node lost or gained focus, so
		// this is where the Window learns the user clicked into the document
		// while a dropdown was open. Not consumed: other components are
		// entitled to the same news.
		for _, m := range w.menus {
			m.closeOnBlur()
		}
		return false
	}
	k, ok := ev.(tui.KeyEvent)
	if !ok || k.Kind != tui.KeyPress {
		return false
	}
	if fireShortcut(w.ctx, keymap.Global, w.shortcuts, k) {
		return true
	}
	for _, m := range w.menus {
		switch windowMenuKey(w.ctx, k, m.focused(), m.openHotkey) {
		case "toggle":
			if m.focused() {
				m.close()
				w.restoreFocus()
				m.endVisit()
			} else {
				m.activate()
			}
			return true
		case "hotkey":
			return true
		case "leave":
			// Escape that the menu did not consume leaves the menu entirely.
			m.close()
			w.restoreFocus()
			m.endVisit()
			return true
		}
	}
	return false
}

// windowMenuKey is what k does to one menu bar: "toggle" (widget.WindowMenu: F10 opens it, or leaves
// it when focused), "hotkey" (Alt and a category's mnemonic, which openHotkey has opened), "leave"
// (widget.MenuClose while it is focused), or "".
func windowMenuKey(ctx *tui.Context, k tui.KeyEvent, focused bool, openHotkey func(rune) bool) string {
	cmd, ok := ctx.KeyCommand(k, widget.WindowMenu, widget.MenuClose)
	switch {
	case ok && cmd.ID == widget.WindowMenu.ID:
		return "toggle"
	case k.Mods&shortcutMods == tui.ModAlt:
		if openHotkey(k.Code) {
			return "hotkey"
		}
	case ok && cmd.ID == widget.MenuClose.ID && focused:
		return "leave"
	}
	return ""
}

// menuBarNode is the component a `MenuBar` declaration builds: golib's MenuBar,
// mounted, and the access keys of its categories.
type menuBarNode struct {
	ctx  *tui.Context
	bar  *widget.MenuBar
	menu *widget.Menu
	// rows are the bar's Menus, and triggers each row's onTriggered by its
	// action — both as the last projection left them (see project).
	rows     []*menuNode
	triggers map[tui.ActionID]func()
	byID     map[widget.ItemID]*menuNode
	// leave gives the keyboard back before a row's onTriggered runs; the
	// Window that adopts the bar sets it.
	leave func()
	// autoHide is golib's MenuBar.autoHide: the bar takes no row until F10 or
	// an Alt+letter brings it up, and it goes again once the keyboard has left
	// it. shown is whether it is up now.
	autoHide, shown bool
	// pendingOpen is the category an Alt+letter asked for while the bar was
	// hidden: it opens once the bar is laid out again, and has a place.
	pendingOpen widget.ItemID
}

// menuCategory is one top-level Menu's access key.
type menuCategory struct {
	hotkey rune
	id     widget.ItemID
}

// accessKeys is each top-level Menu's access key as the bar shows it now. A title from a
// catalog has its key only once the menu has looked it up in the App's language, so the keys
// are read from the menu, not kept from when the rows were built.
func (m *menuBarNode) accessKeys() []menuCategory {
	var out []menuCategory
	for _, row := range m.menu.Model() {
		if row.Hotkey != 0 {
			out = append(out, menuCategory{hotkey: row.Hotkey, id: row.ID})
		}
	}
	return out
}

func (m *menuBarNode) Init(ctx *tui.Context) { m.ctx = ctx; ctx.Mount(m.bar) }

func (m *menuBarNode) Layout(c tui.Constraints) tui.Size {
	if m.autoHide && !m.shown {
		m.ctx.LayoutChild(m.bar, tui.Tight(tui.Size{}))
		m.ctx.PlaceChild(m.bar, tui.Rect{})
		return c.Constrain(tui.Size{})
	}
	sz := m.ctx.LayoutChild(m.bar, c)
	m.ctx.PlaceChild(m.bar, tui.Rect{W: sz.W, H: sz.H})
	if id := m.pendingOpen; id != "" {
		m.pendingOpen = ""
		m.ctx.AfterLayout(menuOpenKey, func() { _ = m.menu.Open(id) })
	}
	return sz
}

const menuOpenKey tui.CommitKey = "menubar.open"

// setAutoHide is MenuBar.autoHide's setter: a preference can hide the bar,
// or bring it back, while the program runs.
func (m *menuBarNode) setAutoHide(v bool) {
	m.autoHide = v
	if !v {
		m.shown = false
	}
	if m.ctx != nil {
		m.ctx.RequestLayout()
	}
}

// show brings a hidden bar up for a visit; hide puts it away once the visit
// is over. Neither does anything to a bar that is not auto-hidden.
func (m *menuBarNode) show() {
	if m.autoHide && !m.shown {
		m.shown = true
		m.ctx.RequestLayout()
	}
}

func (m *menuBarNode) hide() {
	if !m.focused() && m.menu.OpenLevels() == 0 {
		m.endVisit()
	}
}

// endVisit puts an auto-hidden bar away: the user said they were done with it
// (Escape, F10 again, a row that ran), wherever the keyboard went.
func (m *menuBarNode) endVisit() {
	if m.autoHide && m.shown {
		m.shown = false
		m.ctx.RequestLayout()
	}
}

func (m *menuBarNode) Render(tui.Surface)         {}
func (m *menuBarNode) HandleEvent(tui.Event) bool { return false }

func (m *menuBarNode) focused() bool {
	ctx := m.menu.Context()
	return ctx != nil && ctx.Focused()
}

// activate gives the menu focus, ALWAYS from the first category — resuming
// wherever the last visit ended is not where anybody expects to start.
func (m *menuBarNode) activate() {
	ctx := m.menu.Context()
	if ctx == nil {
		return
	}
	m.show()
	if keys := m.accessKeys(); len(keys) > 0 {
		m.menu.Select(keys[0].id)
	}
	ctx.RequestFocus()
}

// openHotkey opens the category whose mnemonic is r, in either case, and
// reports whether one had it. Mnemonics are stored lowercased, while a legacy
// terminal sends Alt+Shift+F as ESC F — 'F' with Alt alone.
func (m *menuBarNode) openHotkey(r rune) bool {
	r = unicode.ToLower(r)
	for _, c := range m.accessKeys() {
		if c.hotkey == r {
			wasHidden := m.autoHide && !m.shown
			m.activate()
			if wasHidden {
				m.pendingOpen = c.id // it has no place until it is laid out
				return true
			}
			// Opening the level hands the selection to its first row, which is
			// what puts the highlight on "New" when File drops down.
			_ = m.menu.Open(c.id)
			return true
		}
	}
	return false
}

// closeOnBlur closes the cascade once focus has left the menu.
//
// APPLICATION POLICY, NOT THE WIDGET'S. golib deliberately keeps a level open
// across a focus loss — a renderer drawing a cascade must still see it, and an
// involuntary loss is not a decision the user made about the menu. A document
// window wants the other rule: clicking into the content means "I am done with
// the menu", and a dropdown left hanging covers the line the user just aimed
// at. Losing focus to something that is not the menu is that click.
func (m *menuBarNode) closeOnBlur() {
	if m.focused() {
		return
	}
	if m.menu.OpenLevels() > 0 {
		m.close()
	}
	m.hide()
}

func (m *menuBarNode) close() {
	m.menu.Close()
	if ctx := m.menu.Context(); ctx != nil {
		ctx.MarkDirty()
	}
}

// KeyScope is a Window's: keymap.Global (tui.KeyScoped).
func (w *windowNode) KeyScope() keymap.Scope { return keymap.Global }

func (w *windowNode) heldShortcuts() (keymap.Scope, []*shortcutNode) {
	return keymap.Global, w.shortcuts
}

// KeyBindings is the Window's Shortcuts, then the menu bar's key (tui.KeyBinder). A category's
// Alt+letter is its label's mnemonic, not a command, and is not listed.
func (w *windowNode) KeyBindings(m *keymap.Map) []keymap.Binding {
	out := shortcutBindings(m, keymap.Global, w.shortcuts)
	if len(w.menus) > 0 {
		out = append(out, tui.BindingsOf(m, widget.WindowMenu)...)
	}
	return out
}

// windowType is the Window type.
var windowType = Type{Name: "Window", Children: AtLeast(1), Build: buildWindow, Setters: map[string]Setter{
	// Qt's Window.color: the background under everything, where no widget paints
	"color": setter("a Window", colorOf, (*windowNode).setColor),
	// Qt's Window.minimumWidth / minimumHeight, in cells: on a smaller screen the App
	// shows its too-small screen instead of the layout (tui.App.SetMinimumSize)
	"minimumWidth":  setter("a Window", truncOf, (*windowNode).setMinimumWidth),
	"minimumHeight": setter("a Window", truncOf, (*windowNode).setMinimumHeight),
}}
