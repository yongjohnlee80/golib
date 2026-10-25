package widget

import (
	"strconv"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Multi-Tab Bar & Content Switcher Architecture
//
// Tabs provides a unified tab navigation header and content pane switcher.
// Modeled after modern terminal multiplexers and editor tab bars, Tabs displays
// a top navigation row of labeled tabs above a dynamic content area displaying
// the active tab's view.
//
// # Subsystem Role & Responsibilities
//
//  1. Content Lifecycle & Mount Modes:
//     - Default Dynamic Unmount: By default, switching tabs unmounts deactivated
//     components to reclaim layout memory and detach inactive subscriptions.
//     - Persistent Mount ([WithKeepMounted]): Preserves all tab component trees
//     in mounted state across switches, retaining scroll positions, active selections,
//     and long-running bus subscriptions while hiding inactive views from layout.
//  2. Header Presentation Modes:
//     - Standard Header: Displays a 1-row styled tab bar with active highlights.
//     - Headless Switcher ([WithoutBar]): Hides the tab bar entirely, allocating
//     100% of vertical height to the active content pane while still providing
//     keyboard navigation and programmatic switching via [Tabs.Select].
//  3. Comprehensive Keyboard & Mouse Navigation:
//     - Subtree Chords: Ctrl+PageUp and Ctrl+PageDown cycle tabs from anywhere within
//     the active child component tree.
//     - Focused Bar Motions: When the tab bar holds focus, '[' and ']' or Left/Right
//     arrow keys switch tabs incrementally.
//     - Direct Mouse Clicks: Clicking any tab label on row 0 immediately activates it.
//     - The Bar's Menu ([Tabs.SetMenu]): ≡ on the bar, or a right press on a tab or on
//     the bar's blank, opens the host's rows for that tab under the bar.
//
// # Layout & Component Stack Hierarchy
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ [Tabs] Container Area                                       │
//	│                                                             │
//	│  Row 0:  [ Tab One ]  [ Tab Two (Active) ]  [ Tab Three ]    │ ◄── Tab Bar (1 row)
//	│  ─────────────────────────────────────────────────────────  │
//	│                                                             │
//	│  Row 1+: Active Tab Content (e.g. Table, BufferView, Form)  │
//	│          - Full remaining width & height (MaxH - 1)         │
//	│          - Only active component laid out & rendered        │
//	│                                                             │
//	└─────────────────────────────────────────────────────────────┘
//
// # Architectural Invariants
//
//  1. Single Active Child Rendering:
//     Regardless of the number of registered tabs, exactly one tab content component
//     is laid out and rendered per frame.
//  2. Automatic Focus Initialization ([WithAutoFocus]):
//     When configured with [WithAutoFocus](true), Tabs claims focus on Init, allowing
//     top-level application screens to accept tab navigation keys immediately without
//     requiring an initial Tab press.
//  3. Event Notification Contract:
//     Every tab change publishes [TabChangedEvent] carrying the Tabs node ID, new index,
//     and label string.
//
// # Concurrency & Goroutine Ownership
//
// Tabs is loop-goroutine-owned. All mutations ([Tabs.Select]) and lifecycle hooks
// must run strictly on the main application loop goroutine.
//
// # Usage Examples
//
// 1. Standard tabbed workspace with three views:
//
//	tabs := widget.NewTabs(
//		widget.WithTab("Editor", editorView),
//		widget.WithTab("Logs", logBufferView),
//		widget.WithTab("Status", statusTable),
//		widget.WithKeepMounted(true), // retain log scrollback while editing
//	)
//
// 2. Programmatic tab switching:
//
//	// Switch to "Logs" view (tab index 1):
//	tabs.Select(1)
type Tabs struct {
	Base
	tabs      []tabEntry
	active    int
	keep      bool
	autoFocus bool
	noBar     bool
	onChange  func(i int) // WithTabsOnChange

	// strip is the bar: the shared tab-row engine TabStrip and TerminalTabs draw with (tab_strip.go),
	// its tabs this container's, by index. Its styles are the bar's, the tabs' and the active tab's.
	strip tabStrip
}

type tabEntry struct {
	label   string
	comp    tui.Component
	mounted bool
}

var _ tui.Focusable = (*Tabs)(nil)

// TabsOption customizes a Tabs under construction.
type TabsOption func(*Tabs)

// WithTab appends one tab (declaration order == bar order).
func WithTab(label string, content tui.Component) TabsOption {
	if content == nil {
		panic("widget: WithTab: nil content")
	}
	return func(t *Tabs) { t.tabs = append(t.tabs, tabEntry{label: label, comp: content}) }
}

// WithTabsOnChange sets what runs, at once, when tab i becomes active: the bus's TabChangedEvent
// reaches other subscribers a loop turn later. An owner composing a Tabs uses it rather than
// subscribing app-wide and filtering by Owner.
func WithTabsOnChange(fn func(i int)) TabsOption { return func(t *Tabs) { t.onChange = fn } }

// WithKeepMounted keeps deactivated tab content mounted (state and
// subscriptions live) instead of unmounting it on switch.
func WithKeepMounted(v bool) TabsOption { return func(t *Tabs) { t.keep = v } }

// WithAutoFocus makes the bar request focus for itself on Init, so a Tabs
// used as a top-level menu is keyboard-navigable (←/→, [ / ]) immediately —
// without the user first pressing Tab to move focus onto it. Off by default:
// a Tabs nested among other focusables should not steal the initial focus.
func WithAutoFocus(v bool) TabsOption { return func(t *Tabs) { t.autoFocus = v } }

// WithoutBar hides the tab bar: Layout gives the active content the full height
// and Render paints no bar. Use when a separate widget (e.g. a dedicated menu
// panel) drives Select and the Tabs acts purely as a content switcher.
func WithoutBar() TabsOption { return func(t *Tabs) { t.noBar = true } }

// WithTabsStyles overrides the bar, tab, and active-tab styles.
func WithTabsStyles(bar, tab, active style.Style) TabsOption {
	return func(t *Tabs) {
		t.strip.bar = bar.Inherit(t.strip.bar)
		t.strip.tab = tab.Inherit(t.strip.tab)
		t.strip.selected = active.Inherit(t.strip.selected)
	}
}

// defaultTabsStyles are the bar, tab and active-tab styles a Tabs has unless it is given others.
func defaultTabsStyles() (bar, tab, active style.Style) {
	return style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		style.New().Background(style.TokenSurface).Foreground(style.TokenForeground).Bold(true)
}

// tabsStyles are a tab bar's looks from its defaults: each one given laid over its default, so a
// look left unset is the default's (Tabs, TabStrip and TerminalTabs SetStyles).
func tabsStyles(bar, tab, active style.Style) (style.Style, style.Style, style.Style) {
	b, t, a := defaultTabsStyles()
	return bar.Inherit(b), tab.Inherit(t), active.Inherit(a)
}

// NewTabs builds a tab switcher. At least one tab is required.
func NewTabs(opts ...TabsOption) *Tabs {
	t := &Tabs{}
	t.strip = tabStrip{base: &t.Base, anchor: t, layer: "tabs", markFocus: true, owner: t}
	t.strip.bar, t.strip.tab, t.strip.selected = defaultTabsStyles()
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	if len(t.tabs) == 0 {
		panic("widget: NewTabs requires at least one WithTab")
	}
	return t
}

// stripTabs, stripActive and stripChoose are the bar's owner side (stripOwner): the tabs by
// index, the active one, and Select.
func (t *Tabs) stripTabs() []stripTab {
	out := make([]stripTab, len(t.tabs))
	for i, tab := range t.tabs {
		out[i] = stripTab{id: strconv.Itoa(i), label: cellLabel(tab.label)}
	}
	return out
}

func (t *Tabs) stripActive() string { return strconv.Itoa(t.active) }

func (t *Tabs) stripChoose(id string) {
	if i, err := strconv.Atoi(id); err == nil {
		t.Select(i)
	}
}

// SetStyles gives the bar, the tabs and the active tab these styles over the defaults, as
// [WithTabsStyles] does at construction: what a style leaves unset is the default's, so zero
// styles restore the defaults.
func (t *Tabs) SetStyles(bar, tab, active style.Style) {
	t.strip.bar, t.strip.tab, t.strip.selected = tabsStyles(bar, tab, active)
	t.MarkDirty()
}

// Add appends a tab at runtime; its content mounts lazily on first select.
func (t *Tabs) Add(label string, content tui.Component) {
	if content == nil {
		panic(errs.Fatal{Op: "widget: Tabs.Add", Rule: "nil content"})
	}
	t.tabs = append(t.tabs, tabEntry{label: label, comp: content})
	t.MarkDirty()
}

// Active returns the active tab index.
func (t *Tabs) Active() int { return t.active }

// TabsMenuAction is a row of the bar's menu: when the row is chosen, Run runs with the index of the
// tab the menu was opened for (SetMenu).
type TabsMenuAction struct {
	ID  tui.ActionID
	Run func(i int)
}

// ActionID implements tui.Action.
func (a TabsMenuAction) ActionID() tui.ActionID { return a.ID }

func (a TabsMenuAction) stripRun() func(string) {
	if a.Run == nil {
		return nil
	}
	return func(id string) {
		if i, err := strconv.Atoi(id); err == nil {
			a.Run(i)
		}
	}
}

// SetMenu gives the bar a menu, opened over it (it needs an OverlayHost above it) by ≡ and by a
// right press on a tab or on the bar's blank: build returns its rows for the tab it opens for (the
// active one off a tab), each time it opens. A row's action is a
// TabsMenuAction; a row with children is a submenu. nil unsets it.
func (t *Tabs) SetMenu(build func(i int) []MenuItemModel) {
	t.strip.menuRows = nil
	if build != nil {
		t.strip.menuRows = func(id string) []MenuItemModel {
			i, _ := strconv.Atoi(id) // the strip's ids are Tabs' indices (stripTabs)
			return build(i)
		}
	}
	t.RequestLayout()
	t.MarkDirty()
}

// listChildren feeds the package's focus walk (active content only).
func (t *Tabs) listChildren() []tui.Component {
	return []tui.Component{t.tabs[t.active].comp}
}

// AcceptsFocus implements tui.Focusable: the bar itself is a tab stop.
func (t *Tabs) AcceptsFocus() bool { return true }

// Init mounts the active tab's content. Re-entrant across remounts.
func (t *Tabs) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	for i := range t.tabs {
		t.tabs[i].mounted = false
	}
	t.active = max(0, min(t.active, len(t.tabs)-1))
	ctx.Mount(t.tabs[t.active].comp)
	t.tabs[t.active].mounted = true
	if t.autoFocus {
		ctx.RequestFocus()
	}
}

// Select activates tab i (out-of-range is ignored), mounting its content
// lazily and unmounting the previous one unless WithKeepMounted. Emits
// TabChangedEvent. On a Tabs not mounted — before its first mount, or after it
// was unmounted with its parent — only the active index moves: Init mounts
// that tab's content when the Tabs is mounted again.
func (t *Tabs) Select(i int) {
	if i < 0 || i >= len(t.tabs) || i == t.active {
		return
	}
	prev := t.active
	t.active = i
	if t.ctx != nil && t.ctx.Mounted() {
		if !t.keep && t.tabs[prev].mounted {
			t.ctx.Unmount(t.tabs[prev].comp)
			t.tabs[prev].mounted = false
		}
		if !t.tabs[i].mounted {
			t.ctx.Mount(t.tabs[i].comp)
			t.tabs[i].mounted = true
		}
	}
	t.RequestLayout()
	t.MarkDirty()
	t.publish(TabChangedEvent{Owner: t.NodeID(), Index: i, Label: t.tabs[i].label})
	if t.onChange != nil {
		t.onChange(i) // last: the owner may change the tabs
	}
}

// cycle advances the active tab by delta with wraparound.
func (t *Tabs) cycle(delta int) {
	n := len(t.tabs)
	t.Select(((t.active+delta)%n + n) % n)
}

// cellLabel is the painted form of one tab label.
func cellLabel(label string) string { return " " + label + " " }

// HandleEvent implements the key/mouse contract.
func (t *Tabs) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		d := t.keyDelta(e, t.focused())
		if d == 0 {
			return false
		}
		t.cycle(d)
		return true

	case tui.MouseEvent:
		if t.noBar || e.Kind != tui.MousePress || e.Y != 0 {
			return false
		}
		if e.Button == tui.MouseRight && t.strip.hasMenu() {
			return t.strip.press(e) // the bar's menu (SetMenu), over the tab pressed
		}
		if e.Button != tui.MouseLeft {
			return false
		}
		t.strip.press(e)
		return true // clicks on the empty bar are consumed, not bubbled
	}
	return false
}

// keyDelta is the step a key takes from the active tab: -1, 1, or 0 for a key that is not the
// container's. The arrows and brackets are the content's unless the bar itself has focus.
func (t *Tabs) keyDelta(e tui.KeyEvent, focused bool) int {
	cmd, ok := t.Context().KeyCommand(e, tabsCommands...)
	switch {
	case !ok:
		return 0
	case cmd.ID == TabsPrev.ID:
		return -1
	case cmd.ID == TabsNext.ID:
		return 1
	case !focused:
		return 0
	case cmd.ID == TabsLeft.ID:
		return -1
	}
	return 1
}

// KeyScope is a Tabs container's key-map scope.
func (t *Tabs) KeyScope() keymap.Scope { return TabsScope }

// KeyBindings is the Tabs container's commands as m binds them (tui.KeyBinder).
func (t *Tabs) KeyBindings(m *keymap.Map) []keymap.Binding { return tui.BindingsOf(m, tabsCommands...) }

// Layout gives the bar one row and the active content the rest.
func (t *Tabs) Layout(c tui.Constraints) tui.Size {
	w := fillW(c)
	h := fillH(c)
	barH := 1
	if t.noBar {
		barH = 0
	}
	ch := max(h-barH, 0)
	if !t.noBar {
		t.ctx.AfterLayout(tabWindowCommitKey, func() { t.strip.commitWindow(w) })
	}
	active := t.tabs[t.active]
	if active.mounted && ch > 0 {
		t.ctx.LayoutChild(active.comp, tui.Tight(tui.Size{W: w, H: ch}))
		t.ctx.PlaceChild(active.comp, tui.Rect{X: 0, Y: barH, W: w, H: ch})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the tab bar, the shared strip's row: the window of tabs that fits, scrolled to
// show the active one, with ← and → when they overflow. Only the active child renders below.
func (t *Tabs) Render(s tui.Surface) {
	if t.noBar {
		return // content-switcher mode: an external widget draws the menu
	}
	if w := s.Size().W; w > 0 {
		t.strip.render(s, w)
	}
}
