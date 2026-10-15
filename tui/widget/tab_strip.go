package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TAB STRIPS — the row of tabs TerminalTabs draws over its terminals, and TabStrip draws alone (a
// GUI editor's title bar, say): the labels, the active one, + and ≡, and the strip's own menus.
// tabStrip is that row; its owner keeps the tabs and says which is active, and is told what the
// user chose.

// stripTab is one tab as the strip draws it: its id, and its label as drawn (cellLabel's padding
// and any status mark included).
type stripTab struct {
	id    string
	label string
}

// tabStrip is the strip's state and its drawing and input, shared by its owners.
type tabStrip struct {
	base   *Base         // the owner's: its context, measure and focus
	anchor tui.Component // the owner: what the menus open over
	layer  string        // the menus' layer id prefix

	bar, tab, selected style.Style

	onAdd    func()
	onMenu   func(id string)
	menuRows func(id string) []MenuItemModel // the strip's own menu (SetMenu)
	addRows  func() []MenuItemModel          // what + offers (SetAddMenu)
	menuOpen *popupLayer

	tabs   func() []stripTab // the owner's tabs, in order
	active func() string     // the owner's active tab; "" for none
	choose func(id string)   // a tab chosen by a click or a key
}

// stripAction is a strip menu row's action: what runs, with the tab the menu opened for; nil for a
// row that runs nothing.
type stripAction interface {
	tui.Action
	stripRun() func(id string)
}

// TabStripMenuAction is a row of a TabStrip's menus: when the row is chosen, Run runs with the tab
// the menu was opened for (SetMenu), or with "" (SetAddMenu's +).
type TabStripMenuAction struct {
	ID  tui.ActionID
	Run func(id string)
}

// ActionID implements tui.Action.
func (a TabStripMenuAction) ActionID() tui.ActionID { return a.ID }

func (a TabStripMenuAction) stripRun() func(string) { return a.Run }

func (s *tabStrip) hasMenu() bool { return s.menuRows != nil || s.onMenu != nil }

// menuShown is whether ≡ is on the strip: the strip's own menu always, with no tab too; the host's
// callback only for a tab.
func (s *tabStrip) menuShown() bool { return s.menuRows != nil || s.onMenu != nil && s.active() != "" }

func (s *tabStrip) hasAdd() bool { return s.addRows != nil || s.onAdd != nil }

// add is + chosen at at, in the owner's frame: SetAddMenu's menu, else the host's callback.
func (s *tabStrip) add(at tui.Point) {
	if s.addRows != nil {
		s.popMenu("", s.addRows(), at)
	} else if s.onAdd != nil {
		s.onAdd()
	}
}

// openMenu opens the menu for tab id at at: SetMenu's, else the host's callback.
func (s *tabStrip) openMenu(id string, at tui.Point) {
	if s.menuRows == nil {
		if s.onMenu != nil {
			s.onMenu(id)
		}
		return
	}
	s.popMenu(id, s.menuRows(id), at)
}

// popMenu shows items over the strip at at, a chosen row's action run with id once the menu has
// gone: the menu's layer traps the input scope while it is open, and an action that acts on a
// window around the strip (a Drawer's toggleMaximize) is refused inside it.
func (s *tabStrip) popMenu(id string, items []MenuItemModel, at tui.Point) {
	ctx := s.base.ctx
	host, ok := hostFor[popupHost](ctx)
	if !ok || len(items) == 0 {
		return
	}
	s.closeMenu()
	var layer *popupLayer
	var chosen func(string)
	menu := NewMenu(WithMenuVimNavigation(true), WithActionExecutor(func(inv tui.ActionInvocation) bool {
		act, ok := inv.Action.(stripAction)
		if !ok || act.stripRun() == nil || layer == nil {
			return false
		}
		chosen = act.stripRun()
		layer.requestClose(DismissAccept)
		return true
	}))
	if err := menu.SetModel(items); err != nil {
		return
	}
	box := NewBox(menu, WithStyle(style.New().Background(style.TokenPanel)))
	l, err := host.openPopupAt(LayerID(fmt.Sprintf("%s.menu.%d", s.layer, ctx.ID())), s.anchor, box, at, func(reason DismissReason) {
		if s.menuOpen == layer {
			s.menuOpen = nil
		}
		if run := chosen; run != nil && reason == DismissAccept {
			chosen = nil
			ctx.Update(func() { run(id) }) // a later turn: the layer, and its trap, gone
		}
	})
	if err != nil {
		return
	}
	layer, s.menuOpen = l, l
}

func (s *tabStrip) closeMenu() {
	if l := s.menuOpen; l != nil {
		s.menuOpen = nil
		l.close(DismissProgrammatic)
	}
}

// width is the strip's natural width: its tabs, + and ≡, each followed by a cell.
func (s *tabStrip) width() int {
	w := s.addX()
	if s.hasAdd() {
		w += 4
	}
	if s.menuShown() {
		w += 4
	}
	return max(w-1, 0)
}

// addX is where + starts: after the last tab.
func (s *tabStrip) addX() int {
	x := 0
	for _, tb := range s.tabs() {
		x += s.base.measure(tb.label) + 1
	}
	return x
}

// tabX is where tab id's label starts.
func (s *tabStrip) tabX(id string) int {
	x := 0
	for _, tb := range s.tabs() {
		if tb.id == id {
			return x
		}
		x += s.base.measure(tb.label) + 1
	}
	return 0
}

// activeIndex is the active tab's place, or -1.
func (s *tabStrip) activeIndex() int {
	active := s.active()
	for i, tb := range s.tabs() {
		if tb.id == active {
			return i
		}
	}
	return -1
}

// spans are the tabs', +'s and ≡'s cells on the row, in order.
func (s *tabStrip) spans() []TabSpan {
	tabs := s.tabs()
	spans := make([]TabSpan, 0, len(tabs)+2)
	x := 0
	for _, tb := range tabs {
		w := s.base.measure(tb.label)
		spans = append(spans, TabSpan{X: x, W: w})
		x += w + 1
	}
	if s.hasAdd() {
		spans = append(spans, TabSpan{X: x, W: 3})
		x += 4
	}
	if s.menuShown() {
		spans = append(spans, TabSpan{X: x, W: 3})
	}
	return spans
}

// render draws the row on row 0 of sur, w cells wide.
func (s *tabStrip) render(sur tui.Surface, w int) {
	sur.Fill(tui.Rect{W: w, H: 1}, " ", s.bar)
	x, active := 0, s.active()
	for _, tb := range s.tabs() {
		st := s.tab
		if tb.id == active {
			st = s.selected
		}
		if x >= w {
			break
		}
		drawText(sur, x, 0, truncate(tb.label, w-x, sur.StringWidth), st)
		x += sur.StringWidth(tb.label) + 1
	}
	if s.hasAdd() && x < w {
		drawText(sur, x, 0, truncate(" + ", w-x, sur.StringWidth), s.tab)
	}
	if s.hasAdd() {
		x += 4
	}
	if s.menuShown() && x < w {
		drawText(sur, x, 0, truncate(" ≡ ", w-x, sur.StringWidth), s.tab)
	}
}

// key is the strip's keys: + and Enter on the focused strip; Ctrl+PgUp/PgDn, and ←/→ and [/] on the
// focused strip, choose the tab before or after the active one, round.
func (s *tabStrip) key(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	focused := s.base.focused() && e.Mods.Chord() == 0
	if focused {
		if e.Text == "+" && s.hasAdd() {
			s.add(tui.Point{X: s.addX(), Y: 1})
			return true
		}
		if e.Code == tui.KeyEnter && s.menuShown() {
			s.openMenu(s.active(), tui.Point{X: s.tabX(s.active()), Y: 1})
			return true
		}
	}
	tabs := s.tabs()
	if len(tabs) == 0 {
		return false
	}
	delta := 0
	if e.Mods.Chord() == tui.ModCtrl {
		if e.Code == tui.KeyPageUp {
			delta = -1
		}
		if e.Code == tui.KeyPageDown {
			delta = 1
		}
	} else if focused {
		if e.Code == tui.KeyLeft || e.Code == '[' {
			delta = -1
		}
		if e.Code == tui.KeyRight || e.Code == ']' {
			delta = 1
		}
	}
	if delta == 0 {
		return false
	}
	if i := s.activeIndex(); i >= 0 {
		s.choose(tabs[(i+delta+len(tabs))%len(tabs)].id)
		return true
	}
	return false
}

// press is a press on row 0 at x: a left press on a tab chooses it, a right one opens its menu;
// + and ≡ open theirs; a right press elsewhere on the row opens the strip's menu for the active
// tab, where it was.
func (s *tabStrip) press(e tui.MouseEvent) bool {
	x := 0
	for _, tb := range s.tabs() {
		w := s.base.measure(tb.label)
		if e.X >= x && e.X < x+w {
			if e.Button == tui.MouseLeft {
				s.choose(tb.id)
			} else if e.Button == tui.MouseRight && s.hasMenu() {
				s.openMenu(tb.id, tui.Point{X: e.X, Y: 1})
			}
			return true
		}
		x += w + 1
	}
	if e.X >= x && e.X < x+3 && e.Button == tui.MouseLeft && s.hasAdd() {
		s.add(tui.Point{X: x, Y: 1})
		return true
	}
	if s.hasAdd() {
		x += 4
	}
	if e.X >= x && e.X < x+3 && e.Button == tui.MouseLeft && s.menuShown() {
		s.openMenu(s.active(), tui.Point{X: x, Y: 1})
		return true
	}
	if e.Button == tui.MouseRight && s.menuRows != nil {
		s.openMenu(s.active(), tui.Point{X: e.X, Y: 1})
		return true
	}
	return false
}

// StripTab is one of a TabStrip's tabs: the host's id for it, and its label.
type StripTab struct {
	ID    string
	Label string
}

// TabStrip is a row of tabs over nothing of its own: the host keeps what the tabs stand for (an
// editor's buffers, say), sets them and the active one, and hears which the user chooses. It draws
// and takes presses as TerminalTabs's strip does: a left press on a tab chooses it, a right one
// opens its menu, and + and ≡ open theirs (SetActions, SetMenu, SetAddMenu). A GUI backend paints
// it natively (TabSpans, ActiveTabIndex).
//
// It takes no focus and no keys: it sits in a title bar, and the host's own keys switch what it
// shows.
type TabStrip struct {
	Base
	core     tabStrip
	tabs     []StripTab
	active   string
	onSelect func(id string)
}

// NewTabStrip is an empty strip.
func NewTabStrip() *TabStrip {
	t := &TabStrip{}
	t.core = tabStrip{base: &t.Base, anchor: t, layer: "tabstrip",
		tabs: func() []stripTab {
			out := make([]stripTab, len(t.tabs))
			for i, tb := range t.tabs {
				out[i] = stripTab{id: tb.ID, label: cellLabel(tb.Label)}
			}
			return out
		},
		active: func() string { return t.active },
		choose: t.choose,
	}
	t.core.bar, t.core.tab, t.core.selected = defaultTabsStyles()
	return t
}

// SetTabs replaces the tabs, in order. The active tab stays active if it is still among them.
func (t *TabStrip) SetTabs(tabs []StripTab) {
	t.tabs = append(t.tabs[:0:0], tabs...)
	if t.index(t.active) < 0 {
		t.active = ""
	}
	t.changed()
}

// Tabs are the strip's tabs, in order.
func (t *TabStrip) Tabs() []StripTab { return append([]StripTab(nil), t.tabs...) }

// SetActive marks tab id active; "" or an id not among the tabs marks none. It tells no one: the
// host's own choice.
func (t *TabStrip) SetActive(id string) {
	if t.index(id) < 0 {
		id = ""
	}
	if id != t.active {
		t.active = id
		t.changed()
	}
}

// Active is the active tab's id; "" for none.
func (t *TabStrip) Active() string { return t.active }

// ActiveTabIndex is the active tab's place, or -1.
func (t *TabStrip) ActiveTabIndex() int { return t.index(t.active) }

// SetOnSelect hears the tab the user chose with a press, once it is active.
func (t *TabStrip) SetOnSelect(fn func(id string)) { t.onSelect = fn }

// SetActions sets + and the right-press menu to the host's callbacks, as TerminalTabs's do.
func (t *TabStrip) SetActions(add func(), menu func(id string)) {
	t.core.onAdd, t.core.onMenu = add, menu
	t.changed()
}

// SetMenu gives the strip a menu of its own, opened by ≡ and a right press on a tab or on the
// strip's blank (it needs an OverlayHost above it): build returns its rows for the tab it opens for
// (the active one off a tab; "" with none). A row's action is a TabStripMenuAction. nil unsets it.
func (t *TabStrip) SetMenu(build func(id string) []MenuItemModel) {
	t.core.menuRows = build
	t.changed()
}

// SetAddMenu makes + open a menu instead of SetActions' add callback; its rows' actions are
// TabStripMenuActions, run with "". nil unsets it.
func (t *TabStrip) SetAddMenu(build func() []MenuItemModel) {
	t.core.addRows = build
	t.changed()
}

// MenuOpen reports whether the strip's own menu is showing.
func (t *TabStrip) MenuOpen() bool { return t.core.menuOpen != nil }

// TabSpans are the tabs', +'s and ≡'s cells on the strip, in order.
func (t *TabStrip) TabSpans() []TabSpan { return t.core.spans() }

// SetStyles sets the strip's looks: its blank, a tab, and the active tab.
func (t *TabStrip) SetStyles(bar, tab, selected style.Style) {
	t.core.bar, t.core.tab, t.core.selected = defaultTabsStyles()
	t.core.bar, t.core.tab, t.core.selected = bar.Inherit(t.core.bar), tab.Inherit(t.core.tab), selected.Inherit(t.core.selected)
	t.MarkDirty()
}

func (*TabStrip) AccessibleRole() tui.AccessibleRole { return tui.RolePageTabList }

func (t *TabStrip) index(id string) int {
	for i, tb := range t.tabs {
		if id != "" && tb.ID == id {
			return i
		}
	}
	return -1
}

func (t *TabStrip) choose(id string) {
	if t.index(id) < 0 {
		return
	}
	t.active = id
	t.changed()
	if t.onSelect != nil {
		t.onSelect(id)
	}
}

func (t *TabStrip) changed() {
	t.RequestLayout()
	t.MarkDirty()
}

// Layout is one row, as wide as the tabs, + and ≡ need, within the constraints.
func (t *TabStrip) Layout(c tui.Constraints) tui.Size {
	w := t.core.width()
	if c.MaxW != tui.Unbounded {
		w = min(w, c.MaxW)
	}
	return c.Constrain(tui.Size{W: w, H: 1})
}

func (t *TabStrip) Render(s tui.Surface) {
	if w := s.Size().W; w > 0 && s.Size().H > 0 {
		t.core.render(s, w)
	}
}

func (t *TabStrip) HandleEvent(ev tui.Event) bool {
	e, ok := ev.(tui.MouseEvent)
	return ok && e.Kind == tui.MousePress && e.Y == 0 && t.core.press(e)
}
