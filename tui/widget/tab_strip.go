package widget

import (
	"fmt"
	"slices"
	"strings"

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
	onClose  func(id string)                 // a tab's ×: nil draws none (TabStrip.SetOnClose)
	menuRows func(id string) []MenuItemModel // the strip's own menu (SetMenu)
	addRows  func() []MenuItemModel          // what + offers (SetAddMenu)
	menuOpen *popupLayer

	tabs   func() []stripTab // the owner's tabs, in order
	active func() string     // the owner's active tab; "" for none
	choose func(id string)   // a tab chosen by a click or a key

	maxLabel int    // a tab's widest, in cells, its padding included; 0 for no cap (TabStrip.SetMaxTabWidth)
	avail    int    // the row's width as laid out; 0 before the first layout: room for every tab
	first    int    // the first tab shown while the tabs overflow the row (< and > scroll it)
	revealed string // the active tab the window last scrolled to show
}

// stripKind is what a stretch of the row is.
type stripKind uint8

const (
	cellTab  stripKind = iota // a tab
	cellPrev                  // ←, the tabs before the window
	cellNext                  // →, the tabs after it
	cellAdd                   // +
	cellMenu                  // ≡
)

// stripCell is a stretch of the row as drawn: what it is, the tab's id, its text, where it starts
// and its width, in cells.
type stripCell struct {
	kind  stripKind
	id    string
	label string
	x, w  int
	close bool // a tab whose × is drawn whole: a press on it closes the tab
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

// label is tb's label as drawn: capped at maxLabel cells, its end cut to an ellipsis, and after
// a × when the tabs close (onClose), outside the cap.
func (s *tabStrip) label(tb stripTab) string {
	l := tb.label
	if s.maxLabel > 0 && s.base.measure(l) > s.maxLabel {
		l = truncate(strings.TrimRight(l, " "), s.maxLabel-1, s.base.measure) + " "
	}
	if s.onClose != nil {
		l = closeMark + l
	}
	return l
}

// closeShown is whether label, as drawn (cut at the window's edge or not), shows its × whole: a
// tab cut to " …" has no × to press.
func (s *tabStrip) closeShown(label string) bool {
	return s.onClose != nil && strings.HasPrefix(label, closeMark) && s.base.measure(label) > s.base.measure(closeMark)
}

// the strip's own glyphs: a tab's close mark, before its padded label (" × name "), the cells a
// press on it takes; and the arrows either side of the window while the tabs overflow
const (
	closeMark = " ×"
	prevLabel = " ← "
	nextLabel = " → "
)

// tail is the cells + and ≡ take at the row's end, each followed by a cell.
func (s *tabStrip) tail() int {
	w := 0
	if s.hasAdd() {
		w += 4
	}
	if s.menuShown() {
		w += 4
	}
	return w
}

// width is the strip's natural width: every tab, + and ≡, each followed by a cell.
func (s *tabStrip) width() int {
	w := s.tail()
	for _, tb := range s.tabs() {
		w += s.base.measure(s.label(tb)) + 1
	}
	return max(w-1, 0)
}

// row is the strip as drawn in avail cells. When every tab fits, the tabs, then + and ≡. When they
// do not, < and > hold a window of the tabs between them, from first, and + and ≡ keep their place
// after: the window scrolls to show the active tab when it changes, and < and > scroll it freely.
func (s *tabStrip) row() []stripCell {
	tabs := s.tabs()
	labels, widths := make([]string, len(tabs)), make([]int, len(tabs))
	natural := s.tail()
	for i, tb := range tabs {
		labels[i] = s.label(tb)
		widths[i] = s.base.measure(labels[i])
		natural += widths[i] + 1
	}
	var out []stripCell
	x := 0
	if s.avail <= 0 || natural-1 <= s.avail || len(tabs) == 0 {
		s.first, s.revealed = 0, "" // overflowing again, the window starts at the active tab
		for i, tb := range tabs {
			out = append(out, stripCell{kind: cellTab, id: tb.id, label: labels[i], x: x, w: widths[i], close: s.closeShown(labels[i])})
			x += widths[i] + 1
		}
	} else {
		room := max(s.avail-s.tail()-8, 1) // the window: the row less < and > and their cells
		s.reveal(tabs, widths, room)
		out = append(out, stripCell{kind: cellPrev, label: prevLabel, w: s.base.measure(prevLabel)})
		x = 4
		end := x + room
		for i := s.first; i < len(tabs) && x < end; i++ {
			label, w := labels[i], widths[i]
			if x+w > end { // the last one shown, cut at the window's edge
				w = end - x
				label = truncate(label, w, s.base.measure)
			}
			out = append(out, stripCell{kind: cellTab, id: tabs[i].id, label: label, x: x, w: w, close: s.closeShown(label)})
			x += w + 1
		}
		x = end + 1 // > keeps its place, however full the window
		out = append(out, stripCell{kind: cellNext, label: nextLabel, x: x, w: s.base.measure(nextLabel)})
		x += 4
	}
	if s.hasAdd() {
		out = append(out, stripCell{kind: cellAdd, label: " + ", x: x, w: 3})
		x += 4
	}
	if s.menuShown() {
		out = append(out, stripCell{kind: cellMenu, label: " ≡ ", x: x, w: 3})
	}
	return out
}

// reveal scrolls the window, room cells wide, to show the active tab when it changed since the
// last time, and keeps first among the tabs.
func (s *tabStrip) reveal(tabs []stripTab, widths []int, room int) {
	s.first = min(s.first, max(len(tabs)-1, 0))
	active := s.active()
	if active == s.revealed {
		return
	}
	s.revealed = active
	at := slices.IndexFunc(tabs, func(tb stripTab) bool { return tb.id == active })
	if at < 0 {
		return
	}
	if at < s.first {
		s.first = at
	}
	// forward until the active tab ends within the window
	for s.first < at {
		used := 0
		for i := s.first; i <= at; i++ {
			used += widths[i] + 1
		}
		if used-1 <= room {
			break
		}
		s.first++
	}
}

// scroll moves the window by delta tabs, kept among them, and repaints.
func (s *tabStrip) scroll(delta int) {
	s.first = max(min(s.first+delta, len(s.tabs())-1), 0)
	s.base.MarkDirty()
}

// lastShown is whether the window shows the last tab.
func (s *tabStrip) lastShown(row []stripCell) bool {
	tabs := s.tabs()
	for i := len(row) - 1; i >= 0; i-- {
		if row[i].kind == cellTab {
			return len(tabs) > 0 && row[i].id == tabs[len(tabs)-1].id && s.base.measure(s.label(tabs[len(tabs)-1])) == row[i].w
		}
	}
	return true
}

// addX is where + starts.
func (s *tabStrip) addX() int {
	for _, c := range s.row() {
		if c.kind == cellAdd {
			return c.x
		}
	}
	return 0
}

// tabX is where tab id's label starts; 0 when it is not shown.
func (s *tabStrip) tabX(id string) int {
	for _, c := range s.row() {
		if c.kind == cellTab && c.id == id {
			return c.x
		}
	}
	return 0
}

// tabIndex is the active tab's place among the tabs, or -1.
func (s *tabStrip) tabIndex() int {
	active := s.active()
	return slices.IndexFunc(s.tabs(), func(tb stripTab) bool { return tb.id == active })
}

// activeIndex is the active tab's place among the spans, or -1 when it is not shown.
func (s *tabStrip) activeIndex() int {
	active := s.active()
	return slices.IndexFunc(s.row(), func(c stripCell) bool { return c.kind == cellTab && c.id == active })
}

// spans are the row's stretches, in order: the tabs shown, < and > around them while they
// overflow, + and ≡.
func (s *tabStrip) spans() []TabSpan {
	row := s.row()
	spans := make([]TabSpan, len(row))
	for i, c := range row {
		spans[i] = TabSpan{X: c.x, W: c.w}
	}
	return spans
}

// render draws the row on row 0 of sur, w cells wide.
func (s *tabStrip) render(sur tui.Surface, w int) {
	sur.Fill(tui.Rect{W: w, H: 1}, " ", s.bar)
	active := s.active()
	for _, c := range s.row() {
		if c.x >= w {
			break
		}
		st := s.tab
		if c.kind == cellTab && c.id == active {
			st = s.selected
		}
		drawText(sur, c.x, 0, truncate(c.label, w-c.x, sur.StringWidth), st)
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
	if i := s.tabIndex(); i >= 0 {
		s.choose(tabs[(i+delta+len(tabs))%len(tabs)].id)
		return true
	}
	return false
}

// press is a press on row 0 at x: a left press on a tab's × closes it (onClose), elsewhere on the
// tab chooses it, and a right one opens its menu; ← and → scroll the window; + and ≡ open theirs;
// a right press elsewhere on the row opens the strip's menu for the active tab, where it was.
func (s *tabStrip) press(e tui.MouseEvent) bool {
	row := s.row()
	for _, c := range row {
		if e.X < c.x || e.X >= c.x+c.w {
			continue
		}
		switch {
		case c.kind == cellTab && e.Button == tui.MouseLeft && c.close && e.X < c.x+s.base.measure(closeMark):
			s.onClose(c.id) // the tab's ×: closed, not chosen
		case c.kind == cellTab && e.Button == tui.MouseLeft:
			s.choose(c.id)
		case c.kind == cellTab && e.Button == tui.MouseRight && s.hasMenu():
			s.openMenu(c.id, tui.Point{X: e.X, Y: 1})
		case c.kind == cellTab:
		case c.kind == cellPrev && e.Button == tui.MouseLeft:
			if s.first > 0 {
				s.scroll(-1)
			}
		case c.kind == cellNext && e.Button == tui.MouseLeft:
			if !s.lastShown(row) {
				s.scroll(1)
			}
		case c.kind == cellAdd && e.Button == tui.MouseLeft:
			s.add(tui.Point{X: c.x, Y: 1})
		case c.kind == cellMenu && e.Button == tui.MouseLeft:
			s.openMenu(s.active(), tui.Point{X: c.x, Y: 1})
		default:
			continue
		}
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

// ActiveTabIndex is the active tab's place among TabSpans, or -1 when it is not shown (the
// tabs overflow the strip, and it is out of the window).
func (t *TabStrip) ActiveTabIndex() int { return t.core.activeIndex() }

// SetMaxTabWidth caps a tab at cells, its padding included, its label's end cut to an ellipsis;
// 0 for no cap. A strip narrower than its tabs scrolls them between < and >, whatever the cap.
func (t *TabStrip) SetMaxTabWidth(cells int) {
	t.core.maxLabel = max(cells, 0)
	t.changed()
}

// SetOnSelect hears the tab the user chose with a press, once it is active.
func (t *TabStrip) SetOnSelect(fn func(id string)) { t.onSelect = fn }

// SetActions sets + and the right-press menu to the host's callbacks, as TerminalTabs's do.
func (t *TabStrip) SetActions(add func(), menu func(id string)) {
	t.core.onAdd, t.core.onMenu = add, menu
	t.changed()
}

// SetOnClose puts a × at the start of every tab: a press on it calls fn with the tab's id (the host
// closes what the tab stands for, asking first if it must, and sets the tabs again); a press on the
// rest of the tab still chooses it. nil takes the × away.
func (t *TabStrip) SetOnClose(fn func(id string)) {
	t.core.onClose = fn
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

// Layout is one row, as wide as the tabs, + and ≡ need, within the constraints: narrower, the tabs
// scroll between < and >.
func (t *TabStrip) Layout(c tui.Constraints) tui.Size {
	w := t.core.width()
	if c.MaxW != tui.Unbounded {
		w = min(w, c.MaxW)
	}
	sz := c.Constrain(tui.Size{W: w, H: 1})
	t.core.avail = sz.W
	return sz
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
