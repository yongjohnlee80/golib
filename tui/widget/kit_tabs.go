package widget

import "github.com/yongjohnlee80/golib/tui/style"

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

// cellLabel is the painted form of one tab label.
func cellLabel(label string) string { return " " + label + " " }
