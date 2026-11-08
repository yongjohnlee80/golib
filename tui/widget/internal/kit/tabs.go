package kit

import "github.com/yongjohnlee80/golib/tui/style"

// DefaultTabsStyles are the bar, tab and active-tab styles a Tabs has unless it is given others.
func DefaultTabsStyles() (bar, tab, active style.Style) {
	return style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		style.New().Background(style.TokenSurface).Foreground(style.TokenForeground).Bold(true)
}

// TabsStyles are a tab bar's looks from its defaults: each one given laid over its default, so a
// look left unset is the default's (Tabs, TabStrip and TerminalTabs SetStyles).
func TabsStyles(bar, tab, active style.Style) (style.Style, style.Style, style.Style) {
	b, t, a := DefaultTabsStyles()
	return bar.Inherit(b), tab.Inherit(t), active.Inherit(a)
}

// CellLabel is the painted form of one tab label.
func CellLabel(label string) string { return " " + label + " " }
