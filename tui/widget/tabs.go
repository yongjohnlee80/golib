package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Tabs is the tab bar + content switcher (ADR-0007 §2.5): a one-row bar
// with the active content below. Only the active child is mounted, laid
// out, and rendered; switching unmounts the old child unless
// WithKeepMounted(true) (which preserves child state — and live
// subscriptions — across switches; ADR-0004 mount semantics).
//
// Keys: Ctrl+PgUp/PgDn cycle from anywhere inside the Tabs subtree; [ and ]
// cycle when the bar itself is focused. Click selects. Emits
// TabChangedEvent.
type Tabs struct {
	Base
	tabs   []tabEntry
	active int
	keep   bool

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
		panic("widget: Tabs.Add: nil content")
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

// HandleEvent implements the §2.5 key/mouse contract.
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
			case '[':
				t.cycle(-1)
				return true
			case ']':
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
			w := tui.StringWidth(cellLabel(tab.label))
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
	ch := max(h-1, 0)
	active := t.tabs[t.active]
	if active.mounted && ch > 0 {
		t.ctx.LayoutChild(active.comp, tui.Tight(tui.Size{W: w, H: ch}))
		t.ctx.PlaceChild(active.comp, tui.Rect{X: 0, Y: 1, W: w, H: ch})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the tab bar; only the active child renders below.
func (t *Tabs) Render(s tui.Surface) {
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
