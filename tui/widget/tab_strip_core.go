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

// stripOwner is a strip's owner (TabStrip, TerminalTabs, Tabs): it keeps the tabs, says which is
// active, and is told which one the user chose.
type stripOwner interface {
	stripTabs() []stripTab // the tabs, in order
	stripActive() string   // the active tab's id; "" for none
	stripChoose(id string) // a tab chosen by a click or a key
}

// stripTab is one tab as the strip draws it: its id, and its label as drawn (cellLabel's padding
// and any status mark included).
type stripTab struct {
	id    string
	label string
}

// tabStrip is the strip's state and its drawing and input, shared by its owners.
type tabStrip struct {
	base   *Base        // the owner's: its context, measure and focus
	anchor ContextOwner // the owner: what the menus open over
	layer  string       // the menus' layer id prefix

	bar, tab, selected style.Style
	markFocus          bool // the selected tab is underlined while the owner has focus (Tabs)

	onAdd    func()
	onMenu   func(id string)
	onClose  func(id string)                 // a tab's ×: nil draws none (TabStrip.SetOnClose)
	menuRows func(id string) []MenuItemModel // the strip's own menu (SetMenu)
	addRows  func() []MenuItemModel          // what + offers (SetAddMenu)
	menuOpen *popupLayer

	owner stripOwner // keeps the tabs, says which is active, and is told what the user chose

	maxLabel int // a tab's widest, in cells, its padding included; 0 for no cap (TabStrip.SetMaxTabWidth)
	// SetLabelFit's: the narrowest a tab is cut to, padding included, and the host's cut
	fitMin int
	fit    func(id string, w int) string
	// avail, first and revealed are stored after layout (commitWindow), and by a scroll: the row's
	// width as laid out (0 before the first layout: room for every tab), the first tab shown while the
	// tabs overflow the row (< and > scroll it), and the active tab the window last scrolled to show
	avail    int
	first    int
	revealed string
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
func (s *tabStrip) menuShown() bool {
	return s.menuRows != nil || s.onMenu != nil && s.owner.stripActive() != ""
}

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

// label is tb's label as drawn now: cut to the tabs' cap (capNow), after a × when the tabs close.
func (s *tabStrip) label(tb stripTab) string { return s.labelAt(tb, s.capNow()) }

// capNow is a tab's widest now, its padding included: SetLabelFit's, as the tabs fit the row
// (fitTabs), or SetMaxTabWidth's; 0 for none.
func (s *tabStrip) capNow() int {
	if s.fit != nil {
		return s.fitTabs(s.owner.stripTabs())
	}
	return s.maxLabel
}

// labelAt is tb's label cut to limit cells: by the host's fit (SetLabelFit), else its end cut to an
// ellipsis; then after a × when the tabs close (onClose), outside the limit.
func (s *tabStrip) labelAt(tb stripTab, limit int) string {
	l := tb.label
	if limit > 0 && s.base.measure(l) > limit {
		if s.fit != nil {
			l = cellLabel(s.fit(tb.id, limit-2)) // 2: the padding
		}
		if s.base.measure(l) > limit { // no fit, or one wider than asked
			l = truncate(strings.TrimRight(l, " "), limit-1, s.base.measure) + " "
		}
	}
	if s.onClose != nil {
		l = closeMark + l
	}
	return l
}

// fitTabs is the cap SetLabelFit's tabs take in avail cells: none while they all fit whole, else the
// widest at which they do, never under fitMin; fitMin when even that does not, and they scroll.
func (s *tabStrip) fitTabs(tabs []stripTab) int {
	if s.avail <= 0 || len(tabs) == 0 {
		return 0
	}
	mark := 0
	if s.onClose != nil {
		mark = s.base.measure(closeMark)
	}
	full, widest := make([]int, len(tabs)), 0
	for i, tb := range tabs {
		full[i] = s.base.measure(tb.label)
		widest = max(widest, full[i])
	}
	fits := func(limit int) bool {
		w := s.tail() - 1
		for _, f := range full {
			if limit > 0 {
				f = min(f, limit)
			}
			w += f + mark + 1
		}
		return w <= s.avail
	}
	if fits(0) {
		return 0
	}
	if s.fitMin >= widest || !fits(s.fitMin) {
		return s.fitMin
	}
	lo, hi := s.fitMin, widest-1 // fits(lo); the widest limit in [lo, hi] that fits
	for lo < hi {
		if mid := (lo + hi + 1) / 2; fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
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

// width is the strip's natural width: every tab, + and ≡, each followed by a cell. A strip that
// fits its tabs to its room (SetLabelFit) asks for them whole.
func (s *tabStrip) width() int {
	limit := s.maxLabel
	if s.fit != nil {
		limit = 0
	}
	w := s.tail()
	for _, tb := range s.owner.stripTabs() {
		w += s.base.measure(s.labelAt(tb, limit)) + 1
	}
	return max(w-1, 0)
}

// row is the strip as drawn in avail cells. When every tab fits, the tabs, then + and ≡. When they
// do not, < and > hold a window of the tabs between them, from first, and + and ≡ keep their place
// after: the window scrolls to show the active tab when it changes, and < and > scroll it freely.
// A window that shows the last tab is packed against >: it starts no later than the first tab from
// which every tab to the last fits (fitFrom), and the cells left over show the tab before it, cut
// from its start, as the window's far end cuts the tab there.
//
// It is a read: Render, a press and the spans all draw it. The window it shows is view's, and is
// stored only after layout (commitWindow) or by a scroll.
func (s *tabStrip) row() []stripCell {
	v := s.view()
	tabs, labels, widths := v.tabs, v.labels, v.widths
	var out []stripCell
	x := 0
	if !v.overflow {
		for i, tb := range tabs {
			out = append(out, stripCell{kind: cellTab, id: tb.id, label: labels[i], x: x, w: widths[i], close: s.closeShown(labels[i])})
			x += widths[i] + 1
		}
	} else {
		room, fit := v.room, fitFrom(widths, v.room)
		out = append(out, stripCell{kind: cellPrev, label: prevLabel, w: s.base.measure(prevLabel)})
		x = 4
		end := x + room
		if v.first == fit && fit > 0 {
			used := -1
			for _, w := range widths[fit:] {
				used += w + 1
			}
			if w := room - used - 1; w >= 2 { // the tab before, cut from its start, then a cell
				label := elide(labels[fit-1], w, ElideLeft, s.base.measure)
				out = append(out, stripCell{kind: cellTab, id: tabs[fit-1].id, label: label, x: x, w: w, close: s.closeShown(label)})
				x += w + 1
			}
		}
		for i := v.first; i < len(tabs) && x < end; i++ {
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

// fitFrom is the first tab from which every tab to the last fits room cells, each after the first
// following a cell.
func fitFrom(widths []int, room int) int {
	used := -1
	for i := len(widths) - 1; i >= 0; i-- {
		if used += widths[i] + 1; used > room {
			return i + 1
		}
	}
	return 0
}

// stripView is the row's tabs as they are drawn now: their labels and widths, whether they overflow
// the row and, while they do, the window over them, room cells wide from first, and the active tab
// it reveals.
type stripView struct {
	tabs     []stripTab
	labels   []string
	widths   []int
	overflow bool
	room     int
	first    int
	revealed string
}

// view is the row as drawn now, from the stored window, storing nothing.
func (s *tabStrip) view() stripView {
	tabs := s.owner.stripTabs()
	limit := s.capNow()
	v := stripView{tabs: tabs, labels: make([]string, len(tabs)), widths: make([]int, len(tabs))}
	natural := s.tail()
	for i, tb := range tabs {
		v.labels[i] = s.labelAt(tb, limit)
		v.widths[i] = s.base.measure(v.labels[i])
		natural += v.widths[i] + 1
	}
	if s.avail <= 0 || natural-1 <= s.avail || len(tabs) == 0 {
		return v // not overflowing: no window, and the next overflow starts at the active tab
	}
	v.overflow = true
	v.room = max(s.avail-s.tail()-8, 1) // the window: the row less < and > and their cells
	v.first, v.revealed = s.window(tabs, v.widths, v.room)
	return v
}

// window is the stored window, room cells wide, scrolled to show the active tab when it changed
// since the window last showed it, and kept among the tabs: first is kept from layout to layout,
// so a row grown wider, or tabs closed at the end, would leave it later than it need be, and the
// cells it frees empty before >.
func (s *tabStrip) window(tabs []stripTab, widths []int, room int) (first int, revealed string) {
	first, revealed = min(s.first, max(len(tabs)-1, 0)), s.revealed
	if active := s.owner.stripActive(); active != revealed {
		revealed = active
		if at := slices.IndexFunc(tabs, func(tb stripTab) bool { return tb.id == active }); at >= 0 {
			first = min(first, at)
			// forward until the active tab ends within the window
			for first < at {
				used := 0
				for i := first; i <= at; i++ {
					used += widths[i] + 1
				}
				if used-1 <= room {
					break
				}
				first++
			}
		}
	}
	return min(first, fitFrom(widths, room)), revealed
}

// tabWindowCommitKey is a tab row's window commit (tui.Context.AfterLayout), the same for every
// owner of a strip: TabStrip, Tabs and TerminalTabs.
const tabWindowCommitKey tui.CommitKey = "tab-window"

// commitWindow stores the window the row shows in avail cells: the strip's owner calls it after
// layout (tui.Context.AfterLayout), with the width it was laid out in.
func (s *tabStrip) commitWindow(avail int) {
	s.avail = avail
	v := s.view()
	s.first, s.revealed = v.first, v.revealed
}

// scroll moves the window as shown by delta tabs, kept among them, and repaints.
func (s *tabStrip) scroll(delta int) {
	v := s.view()
	s.first, s.revealed = max(min(v.first+delta, len(v.tabs)-1), 0), v.revealed
	s.base.MarkDirty()
}

// lastShown is whether the window shows the last tab.
func (s *tabStrip) lastShown(row []stripCell) bool {
	tabs := s.owner.stripTabs()
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
	active := s.owner.stripActive()
	return slices.IndexFunc(s.owner.stripTabs(), func(tb stripTab) bool { return tb.id == active })
}

// activeIndex is the active tab's place among the spans, or -1 when it is not shown.
func (s *tabStrip) activeIndex() int {
	active := s.owner.stripActive()
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
	active := s.owner.stripActive()
	for _, c := range s.row() {
		if c.x >= w {
			break
		}
		st := s.tab
		if c.kind == cellTab && c.id == active {
			st = s.selected
			if s.markFocus && s.base.focused() {
				st = style.New().Underline(true).Inherit(st)
			}
		}
		drawText(sur, c.x, 0, truncate(c.label, w-c.x, sur.StringWidth), st)
	}
}

// stripKey is what a key does to a strip.
type stripKey int

const (
	stripNone stripKey = iota
	stripAdd           // + : what the strip offers to add
	stripMenu          // Enter: the active tab's menu
	stripPrev          // the tab before the active one, round
	stripNext          // the tab after it
)

// keyOf is what e does to the strip (TabStripScope): + and Enter on the focused strip, when it has
// something to add and a menu; Ctrl+PgUp/PgDn, and ←/→ and [/] on the focused strip, a step.
func (s *tabStrip) keyOf(e tui.KeyEvent, focused, hasAdd, menuShown bool) stripKey {
	cmd, ok := s.base.Context().KeyCommand(e, tabStripCommands...)
	switch {
	case !ok:
		return stripNone
	case cmd.ID == TabStripPrev.ID:
		return stripPrev
	case cmd.ID == TabStripNext.ID:
		return stripNext
	case !focused:
		return stripNone
	case cmd.ID == TabStripLeft.ID:
		return stripPrev
	case cmd.ID == TabStripRight.ID:
		return stripNext
	case cmd.ID == TabStripAdd.ID && hasAdd:
		return stripAdd
	case cmd.ID == TabStripMenu.ID && menuShown:
		return stripMenu
	}
	return stripNone
}

// key acts on the strip's keys.
func (s *tabStrip) key(e tui.KeyEvent) bool {
	k := s.keyOf(e, s.base.focused(), s.hasAdd(), s.menuShown())
	switch k {
	case stripNone:
		return false
	case stripAdd:
		s.add(tui.Point{X: s.addX(), Y: 1})
		return true
	case stripMenu:
		s.openMenu(s.owner.stripActive(), tui.Point{X: s.tabX(s.owner.stripActive()), Y: 1})
		return true
	}
	tabs := s.owner.stripTabs()
	if len(tabs) == 0 {
		return false
	}
	delta := 1
	if k == stripPrev {
		delta = -1
	}
	if i := s.tabIndex(); i >= 0 {
		s.owner.stripChoose(tabs[(i+delta+len(tabs))%len(tabs)].id)
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
			s.owner.stripChoose(c.id)
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
			s.openMenu(s.owner.stripActive(), tui.Point{X: c.x, Y: 1})
		default:
			continue
		}
		return true
	}
	if e.Button == tui.MouseRight && s.menuRows != nil {
		s.openMenu(s.owner.stripActive(), tui.Point{X: e.X, Y: 1})
		return true
	}
	return false
}
