package widget

import (
	"image/color"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TabStrip, the public strip: a row of tabs over nothing of its own, drawn by the engine in
// tab_strip_core.go.

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
	barColor atomic.Pointer[func() (color.NRGBA, bool)] // SetBarColor; read where a native painter paints
}

// SetBarColor gives a native painter the colour to fill the strip's bar with, asked at each paint
// (false: none yet, and the cells' colour stands): a strip on a panel's title bar takes the panel's
// colour, not its cells', so the bar reads as one. The terminal draws the cells and never asks. nil
// takes it away.
func (t *TabStrip) SetBarColor(fn func() (color.NRGBA, bool)) {
	if fn == nil {
		t.barColor.Store(nil)
		return
	}
	t.barColor.Store(&fn)
}

// BarColor is the colour SetBarColor gives, false when none is set or it has none yet.
func (t *TabStrip) BarColor() (color.NRGBA, bool) {
	if fn := t.barColor.Load(); fn != nil {
		return (*fn)()
	}
	return color.NRGBA{}, false
}

// NewTabStrip is an empty strip.
func NewTabStrip() *TabStrip {
	t := &TabStrip{}
	t.core = tabStrip{base: &t.Base, anchor: t, layer: "tabstrip", owner: t}
	t.core.bar, t.core.tab, t.core.selected = defaultTabsStyles()
	return t
}

func (t *TabStrip) stripTabs() []stripTab {
	out := make([]stripTab, len(t.tabs))
	for i, tb := range t.tabs {
		out[i] = stripTab{id: tb.ID, label: cellLabel(tb.Label)}
	}
	return out
}

func (t *TabStrip) stripActive() string   { return t.active }
func (t *TabStrip) stripChoose(id string) { t.choose(id) }

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

// SetLabelFit lets the tabs take the strip's room. Each shows its whole label while they all fit;
// when they do not, every label too wide is cut, by fit, to the widest width at which they all fit,
// never under min cells (its padding included, as SetMaxTabWidth's); under that the strip scrolls
// them between ← and →. fit is the host's cut, its middle, say, to keep an extension: tab id's label
// within w cells. With a fit, SetMaxTabWidth's cap is not used; nil leaves it.
func (t *TabStrip) SetLabelFit(min int, fit func(id string, w int) string) {
	t.core.fitMin, t.core.fit = max(min, 3), fit
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
	t.core.bar, t.core.tab, t.core.selected = tabsStyles(bar, tab, selected)
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
	if ctx := t.Context(); ctx != nil { // a measure before mount commits nothing
		ctx.AfterLayout(tabWindowCommitKey, func() { t.core.commitWindow(sz.W) })
	}
	return sz
}

func (t *TabStrip) Render(s tui.Surface) {
	if w := s.Size().W; w > 0 && s.Size().H > 0 {
		t.core.render(s, w)
	}
}

// KeyScope is a TabStrip's key-map scope.
func (t *TabStrip) KeyScope() keymap.Scope { return TabStripScope }

// KeyBindings is the strip's commands as m binds them (tui.KeyBinder).
func (t *TabStrip) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, tabStripCommands...)
}

func (t *TabStrip) HandleEvent(ev tui.Event) bool {
	e, ok := ev.(tui.MouseEvent)
	return ok && e.Kind == tui.MousePress && e.Y == 0 && t.core.press(e)
}
