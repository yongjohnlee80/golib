package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
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
//  1. Standard tabbed workspace with three views:
//
//     tabs := widget.NewTabs(
//     widget.WithTab("Editor", editorView),
//     widget.WithTab("Logs", logBufferView),
//     widget.WithTab("Status", statusTable),
//     widget.WithKeepMounted(true), // retain log scrollback while editing
//     )
//
//  2. Programmatic tab switching:
//
//     // Switch to "Logs" view (tab index 1):
//     tabs.Select(1)
type Tabs struct {
	Base
	tabs      []tabEntry
	active    int
	keep      bool
	autoFocus bool
	noBar     bool

	barSt    style.Style
	tabSt    style.Style
	activeSt style.Style
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
		t.barSt = bar.Inherit(t.barSt)
		t.tabSt = tab.Inherit(t.tabSt)
		t.activeSt = active.Inherit(t.activeSt)
	}
}

// NewTabs builds a tab switcher. At least one tab is required.
func NewTabs(opts ...TabsOption) *Tabs {
	t := &Tabs{
		barSt:    style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		tabSt:    style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		activeSt: style.New().Background(style.TokenSurface).Foreground(style.TokenForeground).Bold(true),
	}
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
// TabChangedEvent.
func (t *Tabs) Select(i int) {
	if i < 0 || i >= len(t.tabs) || i == t.active {
		return
	}
	prev := t.active
	t.active = i
	if t.ctx != nil {
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
		if e.Kind == tui.KeyRelease {
			return false
		}
		if e.Mods&tui.ModCtrl != 0 {
			switch e.Code {
			case tui.KeyPageUp:
				t.cycle(-1)
				return true
			case tui.KeyPageDown:
				t.cycle(1)
				return true
			}
			return false
		}
		if t.focused() && e.Mods == 0 {
			switch e.Code {
			case '[', tui.KeyLeft:
				t.cycle(-1)
				return true
			case ']', tui.KeyRight:
				t.cycle(1)
				return true
			}
		}
		return false

	case tui.MouseEvent:
		if e.Kind != tui.MousePress || e.Button != tui.MouseLeft || e.Y != 0 {
			return false
		}
		x := 0
		for i, tab := range t.tabs {
			w := t.measure(cellLabel(tab.label))
			if e.X >= x && e.X < x+w {
				t.Select(i)
				return true
			}
			x += w + 1
		}
		return true // clicks on the empty bar are consumed, not bubbled
	}
	return false
}

// Layout gives the bar one row and the active content the rest.
func (t *Tabs) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, max(c.MinW, 1))
	h := boundedMax(c.MaxH, max(c.MinH, 1))
	barH := 1
	if t.noBar {
		barH = 0
	}
	ch := max(h-barH, 0)
	active := t.tabs[t.active]
	if active.mounted && ch > 0 {
		t.ctx.LayoutChild(active.comp, tui.Tight(tui.Size{W: w, H: ch}))
		t.ctx.PlaceChild(active.comp, tui.Rect{X: 0, Y: barH, W: w, H: ch})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the tab bar; only the active child renders below.
func (t *Tabs) Render(s tui.Surface) {
	if t.noBar {
		return // content-switcher mode: an external widget draws the menu
	}
	w := s.Size().W
	if w <= 0 {
		return
	}
	s.Fill(tui.Rect{X: 0, Y: 0, W: w, H: 1}, " ", t.barSt)
	x := 0
	for i, tab := range t.tabs {
		st := t.tabSt
		if i == t.active {
			st = t.activeSt
			if t.focused() {
				st = style.New().Underline(true).Inherit(st)
			}
		}
		label := cellLabel(tab.label)
		lw := s.StringWidth(label)
		if x >= w {
			break
		}
		drawText(s, x, 0, truncate(label, w-x, s.StringWidth), st)
		x += lw + 1
	}
}
