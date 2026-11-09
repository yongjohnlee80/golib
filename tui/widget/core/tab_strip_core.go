package widget

import (
	"fmt"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// TAB STRIPS — the row of tabs TerminalTabs draws over its terminals, and TabStrip draws alone (a
// GUI editor's title bar, say): the labels, the active one, + and ≡, and the strip's own menus.
// tabStrip is that row; its owner keeps the tabs and says which is active, and is told what the
// user chose.

// StripOwner is a strip's owner (TabStrip, TerminalTabs, Tabs): it keeps the tabs, says which is
// active, and is told which one the user chose. golib's components draw through it; a widget outside golib rarely needs it.
type StripOwner interface {
	StripTabs() []kit.StripTab // the tabs, in order
	StripActive() string       // the active tab's id; "" for none
	StripChoose(id string)     // a tab chosen by a click or a key
}

// StripCore is the strip Tabs, TabStrip and TerminalTabs share: its state, and its drawing and
// input. golib's components draw through it; a widget outside golib rarely needs it. It is exported while TerminalTabs, a widget of the terminal's,
// draws through it; once that draws through the public TabStrip it is unexported again, so a
// widget outside golib should not build on it.
type StripCore struct {
	OwnerBase *Base        // the owner's: its context, measure and focus
	Anchor    ContextOwner // the owner: what the menus open over
	Layer     string       // the menus' layer id prefix

	Bar, Tab, Selected style.Style
	markFocus          bool // the selected tab is underlined while the owner has focus (Tabs)

	OnAdd    func()
	OnMenu   func(id string)
	onClose  func(id string)                 // a tab's ×: nil draws none (TabStrip.SetOnClose)
	MenuRows func(id string) []MenuItemModel // the strip's own menu (SetMenu)
	AddRows  func() []MenuItemModel          // what + offers (SetAddMenu)
	MenuOpen *PopupLayer
	// windowOps are the window controls at the row's right end, in their order (SetWindowButtons):
	// none by default; onWindow runs the one pressed
	windowOps []WindowOperation
	OnWindow  func(WindowOperation)

	Owner StripOwner // keeps the tabs, says which is active, and is told what the user chose

	maxLabel int // a tab's widest, in cells, its padding included; 0 for no cap (TabStrip.SetMaxTabWidth)
	// SetLabelFit's: the narrowest a tab is cut to, padding included, and the host's cut
	fitMin int
	fit    func(id string, w int) string
	// avail, first and revealed are stored after layout (commitWindow), and by a scroll: the row's
	// width as laid out (0 before the first layout: room for every tab), the first tab shown while the
	// tabs overflow the row (< and > scroll it), and the active tab the window last scrolled to show
	// and its width then (revealedW: a label that grows after, as a buffer's does when its file's name
	// arrives, is shown again whole)
	avail     int
	first     int
	revealed  string
	revealedW int
}

// StripCell is a stretch of the row as drawn: what it is, the tab's id, its text, where it starts
// and its width, in cells. StripCore's Row answers them; golib's components draw through it; a widget outside golib rarely needs it.
type StripCell struct {
	Kind  kit.StripKind
	id    string
	Label string
	X, w  int
	close bool            // a tab whose × is drawn whole: a press on it closes the tab
	op    WindowOperation // a window control's
}

// StripAction is a strip menu row's action: what runs, with the tab the menu opened for; nil for a
// row that runs nothing. golib's components draw through it; a widget outside golib rarely needs it.
type StripAction interface {
	tui.Action
	StripRun() func(id string)
}

// TabStripMenuAction is a row of a TabStrip's menus: when the row is chosen, Run runs with the tab
// the menu was opened for (SetMenu), or with "" (SetAddMenu's +).
type TabStripMenuAction struct {
	ID  tui.ActionID
	Run func(id string)
}

// ActionID implements tui.Action.
func (a TabStripMenuAction) ActionID() tui.ActionID { return a.ID }

func (a TabStripMenuAction) StripRun() func(string) { return a.Run }

func (s *StripCore) hasMenu() bool { return s.MenuRows != nil || s.OnMenu != nil }

// menuShown is whether ≡ is on the strip: the strip's own menu always, with no tab too; the host's
// callback only for a tab.
func (s *StripCore) menuShown() bool {
	return s.MenuRows != nil || s.OnMenu != nil && s.Owner.StripActive() != ""
}

func (s *StripCore) hasAdd() bool { return s.AddRows != nil || s.OnAdd != nil }

// add is + chosen at at, in the owner's frame: SetAddMenu's menu, else the host's callback.
func (s *StripCore) add(at tui.Point) {
	if s.AddRows != nil {
		s.popMenu("", s.AddRows(), at)
	} else if s.OnAdd != nil {
		s.OnAdd()
	}
}

// menuAt is the point a strip's menu (≡, +, a right press) opens at for column x: on the strip's
// own row, since the popup places its panel below the point it is given, so the menu's first row is
// the row right under the strip.
func menuAt(x int) tui.Point { return tui.Point{X: x} }

// OpenMenu opens the menu for tab id at at: SetMenu's, else the host's callback.
func (s *StripCore) OpenMenu(id string, at tui.Point) {
	if s.MenuRows == nil {
		if s.OnMenu != nil {
			s.OnMenu(id)
		}
		return
	}
	s.popMenu(id, s.MenuRows(id), at)
}

// popMenu shows items over the strip at at, a chosen row's action run with id once the menu has
// gone: the menu's layer traps the input scope while it is open, and an action that acts on a
// window around the strip (a Drawer's toggleMaximize) is refused inside it.
func (s *StripCore) popMenu(id string, items []MenuItemModel, at tui.Point) {
	ctx := s.OwnerBase.ctx
	host, ok := kit.HostFor[PopupHost](ctx)
	if !ok || len(items) == 0 {
		return
	}
	s.CloseMenu()
	var layer *PopupLayer
	var chosen func(string)
	menu := NewMenu(WithMenuVimNavigation(true), WithActionExecutor(func(inv tui.ActionInvocation) bool {
		act, ok := inv.Action.(StripAction)
		if !ok || act.StripRun() == nil || layer == nil {
			return false
		}
		chosen = act.StripRun()
		layer.RequestClose(DismissAccept)
		return true
	}))
	if err := menu.SetModel(items); err != nil {
		return
	}
	box := NewBox(menu, WithStyle(style.New().Background(style.TokenPanel)))
	l, err := host.OpenPopupAt(LayerID(fmt.Sprintf("%s.menu.%d", s.Layer, ctx.ID())), s.Anchor, box, at, func(reason DismissReason) {
		if s.MenuOpen == layer {
			s.MenuOpen = nil
		}
		if run := chosen; run != nil && reason == DismissAccept {
			chosen = nil
			ctx.Update(func() { run(id) }) // a later turn: the layer, and its trap, gone
		}
	})
	if err != nil {
		return
	}
	layer, s.MenuOpen = l, l
}

// CloseMenu closes the strip's menu, when one is open (OpenMenu), as the program closes it.
func (s *StripCore) CloseMenu() {
	if l := s.MenuOpen; l != nil {
		s.MenuOpen = nil
		l.Close(DismissProgrammatic)
	}
}

// label is tb's label as drawn now: cut to the tabs' cap (capNow), after a × when the tabs close.
func (s *StripCore) label(tb kit.StripTab) string { return s.labelAt(tb, s.capNow()) }

// capNow is a tab's widest now, its padding included: SetLabelFit's, as the tabs fit the row
// (fitTabs), or SetMaxTabWidth's; 0 for none.
func (s *StripCore) capNow() int {
	if s.fit != nil {
		return s.fitTabs(s.Owner.StripTabs())
	}
	return s.maxLabel
}

// labelAt is tb's label cut to limit cells: by the host's fit (SetLabelFit), else its end cut to an
// ellipsis; then after a × when the tabs close (onClose), outside the limit.
func (s *StripCore) labelAt(tb kit.StripTab, limit int) string {
	l := tb.Label
	if limit > 0 && s.OwnerBase.Measure(l) > limit {
		if s.fit != nil {
			l = kit.CellLabel(s.fit(tb.ID, limit-2)) // 2: the padding
		}
		if s.OwnerBase.Measure(l) > limit { // no fit, or one wider than asked
			l = kit.Truncate(strings.TrimRight(l, " "), limit-1, s.OwnerBase.Measure) + " "
		}
	}
	if s.onClose != nil {
		l = closeMark + l
	}
	return l
}

// fitTabs is the cap SetLabelFit's tabs take in avail cells: none while they all fit whole, else the
// widest at which they do, never under fitMin; fitMin when even that does not, and they scroll.
func (s *StripCore) fitTabs(tabs []kit.StripTab) int {
	if s.avail <= 0 || len(tabs) == 0 {
		return 0
	}
	mark := 0
	if s.onClose != nil {
		mark = s.OwnerBase.Measure(closeMark)
	}
	full, widest := make([]int, len(tabs)), 0
	for i, tb := range tabs {
		full[i] = s.OwnerBase.Measure(tb.Label)
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
func (s *StripCore) closeShown(label string) bool {
	return s.onClose != nil && strings.HasPrefix(label, closeMark) && s.OwnerBase.Measure(label) > s.OwnerBase.Measure(closeMark)
}

// the strip's own glyphs: a tab's close mark, before its padded label (" × name "), the cells a
// press on it takes; and the arrows either side of the window while the tabs overflow
const (
	closeMark = " ×"
	prevLabel = " ← "
	nextLabel = " → "
)

// tail is the cells + and ≡ take at the row's end, each followed by a cell.
func (s *StripCore) tail() int {
	w := 0
	if s.hasAdd() {
		w += 4
	}
	if s.menuShown() {
		w += 4
	}
	return w + s.controlsWidth()
}

// stripControlCells is a window control's width, and the pitch they sit at side by side: the native
// title bar's (gui/widget's controlCells), so a painter's dots are as far apart as its.
const stripControlCells = 3

// stripControls are the window controls a strip shows, in the title bar's order: minimize, maximize,
// close. Another control is one more entry.
var stripControls = []WindowOperation{WindowMinimize, WindowMaximize, WindowClose}

// controlsWidth is the window controls' cells: a cell before them, then each side by side; none
// without them.
func (s *StripCore) controlsWidth() int {
	if len(s.windowOps) == 0 {
		return 0
	}
	return 1 + stripControlCells*len(s.windowOps)
}

// SetWindowButtons shows _, + and x at the row's right end, or none.
func (s *StripCore) SetWindowButtons(on bool) {
	s.windowOps = nil
	if on {
		s.windowOps = slices.Clone(stripControls)
	}
	s.OwnerBase.MarkDirty()
}

// controlCells are the window controls from x at the earliest: right-aligned in the row as laid out
// (avail), else after the rest of it.
func (s *StripCore) controlCells(x int) []StripCell {
	start := max(x, s.avail-s.controlsWidth()+1)
	out := make([]StripCell, len(s.windowOps))
	for i, op := range s.windowOps {
		out[i] = StripCell{Kind: kit.CellControl, op: op, Label: " " + windowControlLabels[op] + " ", X: start + stripControlCells*i, w: stripControlCells}
	}
	return out
}

// ControlSpans are the window controls as a native painter reads them (TabWindowController).
func (s *StripCore) ControlSpans() []TabControl {
	var out []TabControl
	for _, c := range s.Row() {
		if c.Kind == kit.CellControl {
			out = append(out, TabControl{X: c.X, W: c.w, Op: c.op})
		}
	}
	return out
}

// width is the strip's natural width: every tab, + and ≡, each followed by a cell. A strip that
// fits its tabs to its room (SetLabelFit) asks for them whole.
func (s *StripCore) width() int {
	limit := s.maxLabel
	if s.fit != nil {
		limit = 0
	}
	w := s.tail()
	for _, tb := range s.Owner.StripTabs() {
		w += s.OwnerBase.Measure(s.labelAt(tb, limit)) + 1
	}
	return max(w-1, 0)
}

// Row is the strip as drawn in avail cells. When every tab fits, the tabs, then + and ≡. When they
// do not, < and > hold a window of the tabs between them, from first, and + and ≡ keep their place
// after: the window scrolls to show the active tab when it changes, and < and > scroll it freely.
// A window that shows the last tab is packed against >: it starts no later than the first tab from
// which every tab to the last fits (fitFrom), and the cells left over show the tab before it, cut
// from its start, as the window's far end cuts the tab there.
//
// It is a read: Render, a press and the spans all draw it. The window it shows is view's, and is
// stored only after layout (commitWindow) or by a scroll.
func (s *StripCore) Row() []StripCell {
	v := s.view()
	tabs, labels, widths := v.tabs, v.labels, v.widths
	var out []StripCell
	x := 0
	if !v.overflow {
		for i, tb := range tabs {
			out = append(out, StripCell{Kind: kit.CellTab, id: tb.ID, Label: labels[i], X: x, w: widths[i], close: s.closeShown(labels[i])})
			x += widths[i] + 1
		}
	} else {
		room, fit := v.room, fitFrom(widths, v.room)
		out = append(out, StripCell{Kind: kit.CellPrev, Label: prevLabel, w: s.OwnerBase.Measure(prevLabel)})
		x = 4
		end := x + room
		if v.first == fit && fit > 0 {
			used := -1
			for _, w := range widths[fit:] {
				used += w + 1
			}
			if w := room - used - 1; w >= 2 { // the tab before, cut from its start, then a cell
				label := ElideText(labels[fit-1], w, ElideLeft, s.OwnerBase.Measure)
				out = append(out, StripCell{Kind: kit.CellTab, id: tabs[fit-1].ID, Label: label, X: x, w: w, close: s.closeShown(label)})
				x += w + 1
			}
		}
		for i := v.first; i < len(tabs) && x < end; i++ {
			label, w := labels[i], widths[i]
			if x+w > end { // the last one shown, cut at the window's edge
				w = end - x
				label = kit.Truncate(label, w, s.OwnerBase.Measure)
			}
			out = append(out, StripCell{Kind: kit.CellTab, id: tabs[i].ID, Label: label, X: x, w: w, close: s.closeShown(label)})
			x += w + 1
		}
		x = end + 1 // > keeps its place, however full the window
		out = append(out, StripCell{Kind: kit.CellNext, Label: nextLabel, X: x, w: s.OwnerBase.Measure(nextLabel)})
		x += 4
	}
	if s.hasAdd() {
		out = append(out, StripCell{Kind: kit.CellAdd, Label: " + ", X: x, w: 3})
		x += 4
	}
	if s.menuShown() {
		out = append(out, StripCell{Kind: kit.CellMenu, Label: " ≡ ", X: x, w: 3})
		x += 4
	}
	return append(out, s.controlCells(x)...)
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
	tabs      []kit.StripTab
	labels    []string
	widths    []int
	overflow  bool
	room      int
	first     int
	revealed  string
	revealedW int
}

// view is the row as drawn now, from the stored window, storing nothing.
func (s *StripCore) view() stripView {
	tabs := s.Owner.StripTabs()
	limit := s.capNow()
	v := stripView{tabs: tabs, labels: make([]string, len(tabs)), widths: make([]int, len(tabs))}
	natural := s.tail()
	for i, tb := range tabs {
		v.labels[i] = s.labelAt(tb, limit)
		v.widths[i] = s.OwnerBase.Measure(v.labels[i])
		natural += v.widths[i] + 1
	}
	if s.avail <= 0 || natural-1 <= s.avail || len(tabs) == 0 {
		return v // not overflowing: no window, and the next overflow starts at the active tab
	}
	v.overflow = true
	v.room = max(s.avail-s.tail()-8, 1) // the window: the row less < and > and their cells
	v.first, v.revealed, v.revealedW = s.window(tabs, v.widths, v.room)
	return v
}

// window is the stored window, room cells wide, scrolled to show the active tab when it changed
// since the window last showed it, or its width did (its label grew or shrank: a cut at the window's
// edge would hide its end), and kept among the tabs: first is kept from layout to layout,
// so a row grown wider, or tabs closed at the end, would leave it later than it need be, and the
// cells it frees empty before >.
func (s *StripCore) window(tabs []kit.StripTab, widths []int, room int) (first int, revealed string, revealedW int) {
	first, revealed, revealedW = min(s.first, max(len(tabs)-1, 0)), s.revealed, s.revealedW
	active := s.Owner.StripActive()
	at := slices.IndexFunc(tabs, func(tb kit.StripTab) bool { return tb.ID == active })
	if active != revealed || (at >= 0 && widths[at] != revealedW) {
		revealed = active
		if at >= 0 {
			revealedW = widths[at]
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
	return min(first, fitFrom(widths, room)), revealed, revealedW
}

// CommitWindow stores the window the row shows in avail cells: the strip's owner calls it after
// layout (tui.Context.AfterLayout), with the width it was laid out in.
func (s *StripCore) CommitWindow(avail int) {
	s.avail = avail
	v := s.view()
	s.first, s.revealed, s.revealedW = v.first, v.revealed, v.revealedW
}

// scroll moves the window as shown by delta tabs, kept among them, and repaints.
func (s *StripCore) scroll(delta int) {
	v := s.view()
	s.first, s.revealed, s.revealedW = max(min(v.first+delta, len(v.tabs)-1), 0), v.revealed, v.revealedW
	s.OwnerBase.MarkDirty()
}

// lastShown is whether the window shows the last tab.
func (s *StripCore) lastShown(row []StripCell) bool {
	tabs := s.Owner.StripTabs()
	for i := len(row) - 1; i >= 0; i-- {
		if row[i].Kind == kit.CellTab {
			return len(tabs) > 0 && row[i].id == tabs[len(tabs)-1].ID && s.OwnerBase.Measure(s.label(tabs[len(tabs)-1])) == row[i].w
		}
	}
	return true
}

// AddX is where + starts.
func (s *StripCore) AddX() int {
	for _, c := range s.Row() {
		if c.Kind == kit.CellAdd {
			return c.X
		}
	}
	return 0
}

// TabX is where tab id's label starts; 0 when it is not shown.
func (s *StripCore) TabX(id string) int {
	for _, c := range s.Row() {
		if c.Kind == kit.CellTab && c.id == id {
			return c.X
		}
	}
	return 0
}

// tabIndex is the active tab's place among the tabs, or -1.
func (s *StripCore) tabIndex() int {
	active := s.Owner.StripActive()
	return slices.IndexFunc(s.Owner.StripTabs(), func(tb kit.StripTab) bool { return tb.ID == active })
}

// ActiveIndex is the active tab's place among the spans, or -1 when it is not shown.
func (s *StripCore) ActiveIndex() int {
	active := s.Owner.StripActive()
	return slices.IndexFunc(s.Row(), func(c StripCell) bool { return c.Kind == kit.CellTab && c.id == active })
}

// Spans are the row's stretches, in order: the tabs shown, < and > around them while they
// overflow, + and ≡.
func (s *StripCore) Spans() []TabSpan {
	var spans []TabSpan
	for _, c := range s.Row() {
		if c.Kind != kit.CellControl { // the window controls are their own (controlSpans), not tabs to box
			spans = append(spans, TabSpan{X: c.X, W: c.w})
		}
	}
	return spans
}

// Render draws the row on row 0 of sur, w cells wide.
func (s *StripCore) Render(sur tui.Surface, w int) {
	sur.Fill(tui.Rect{W: w, H: 1}, " ", s.Bar)
	active := s.Owner.StripActive()
	for _, c := range s.Row() {
		if c.X >= w {
			break
		}
		st := s.Tab
		if c.Kind == kit.CellControl {
			st = style.New().Foreground(windowControlTokens[c.op]).Inherit(s.Bar)
		}
		if c.Kind == kit.CellTab && c.id == active {
			st = s.Selected
			if s.markFocus && s.OwnerBase.Focused() {
				st = style.New().Underline(true).Inherit(st)
			}
		}
		kit.DrawText(sur, c.X, 0, kit.Truncate(c.Label, w-c.X, sur.StringWidth), st)
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
func (s *StripCore) keyOf(e tui.KeyEvent, focused, hasAdd, menuShown bool) stripKey {
	cmd, ok := s.OwnerBase.Context().KeyCommand(e, TabStripCommands...)
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

// Key acts on the strip's keys.
func (s *StripCore) Key(e tui.KeyEvent) bool {
	k := s.keyOf(e, s.OwnerBase.Focused(), s.hasAdd(), s.menuShown())
	switch k {
	case stripNone:
		return false
	case stripAdd:
		s.add(menuAt(s.AddX()))
		return true
	case stripMenu:
		s.OpenMenu(s.Owner.StripActive(), menuAt(s.TabX(s.Owner.StripActive())))
		return true
	}
	tabs := s.Owner.StripTabs()
	if len(tabs) == 0 {
		return false
	}
	delta := 1
	if k == stripPrev {
		delta = -1
	}
	if i := s.tabIndex(); i >= 0 {
		s.Owner.StripChoose(tabs[(i+delta+len(tabs))%len(tabs)].ID)
		return true
	}
	return false
}

// Press is a press on row 0 at x: a left press on a tab's × closes it (onClose), elsewhere on the
// tab chooses it, and a right one opens its menu; ← and → scroll the window; + and ≡ open theirs;
// a right press elsewhere on the row opens the strip's menu for the active tab, where it was.
func (s *StripCore) Press(e tui.MouseEvent) bool {
	row := s.Row()
	for _, c := range row {
		if e.X < c.X || e.X >= c.X+c.w {
			continue
		}
		switch {
		case c.Kind == kit.CellTab && e.Button == tui.MouseLeft && c.close && e.X < c.X+s.OwnerBase.Measure(closeMark):
			s.onClose(c.id) // the tab's ×: closed, not chosen
		case c.Kind == kit.CellTab && e.Button == tui.MouseLeft:
			s.Owner.StripChoose(c.id)
		case c.Kind == kit.CellTab && e.Button == tui.MouseRight && s.hasMenu():
			s.OpenMenu(c.id, menuAt(e.X))
		case c.Kind == kit.CellTab:
		case c.Kind == kit.CellPrev && e.Button == tui.MouseLeft:
			if s.first > 0 {
				s.scroll(-1)
			}
		case c.Kind == kit.CellNext && e.Button == tui.MouseLeft:
			if !s.lastShown(row) {
				s.scroll(1)
			}
		case c.Kind == kit.CellAdd && e.Button == tui.MouseLeft:
			s.add(menuAt(c.X))
		case c.Kind == kit.CellMenu && e.Button == tui.MouseLeft:
			s.OpenMenu(s.Owner.StripActive(), menuAt(c.X))
		case c.Kind == kit.CellControl && e.Button == tui.MouseLeft:
			if s.OnWindow != nil {
				s.OnWindow(c.op)
			}
		default:
			continue
		}
		return true
	}
	if e.Button == tui.MouseRight && s.MenuRows != nil {
		s.OpenMenu(s.Owner.StripActive(), menuAt(e.X))
		return true
	}
	return false
}
