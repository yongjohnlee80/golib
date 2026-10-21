package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// Tabs draws its bar with the shared tab strip: tabs that overflow the row scroll, so the active
// tab is always shown, and each span sits where its label is painted. Before, Tabs drew its own
// bar, cut at the right edge: a tab past it was never shown, its span pointing off the screen.
func TestTabsShowTheActiveTabWhenTheyOverflowAndSpansMatchThePaint(t *testing.T) {
	var opts []widget.TabsOption
	for _, l := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"} {
		opts = append(opts, widget.WithTab(l, widget.NewText(l+" body")))
	}
	tabs := widget.NewTabs(opts...)
	h := startApp(t, tabs, 24, 4)
	h.onLoop(func() { tabs.Select(5) })
	h.waitFor("the active foxtrot on the bar", func() bool { return strings.Contains(h.row(0), "foxtrot") })

	var spans []widget.TabSpan
	h.onLoop(func() { spans = tabs.TabSpans() })
	bar := []rune(h.row(0))
	for _, sp := range spans {
		if sp.X < 0 || sp.W <= 0 || sp.X+sp.W > len(bar) {
			t.Errorf("span %+v lies off the %d-cell bar %q", sp, len(bar), string(bar))
			continue
		}
		if strings.TrimSpace(string(bar[sp.X:sp.X+sp.W])) == "" {
			t.Errorf("span %+v covers nothing painted on the bar %q", sp, string(bar))
		}
	}
}
