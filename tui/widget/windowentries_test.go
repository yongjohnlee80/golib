package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// listEntries is a consumer's own collector read side: a fixed list, never changing.
type listEntries []widget.MinimizedWindow

func (l listEntries) Entries() []widget.MinimizedWindow {
	return append([]widget.MinimizedWindow(nil), l...)
}
func (l listEntries) Observe(func()) func() { return func() {} }

// A WindowTaskbar presents any collector's read side (WindowEntries), not only *MinimizedWindows:
// a consumer's own list shows its windows on the bar.
func TestAWindowTaskbarPresentsAnyWindowEntries(t *testing.T) {
	bar := widget.NewWindowTaskbar(listEntries{{Owner: 1, Title: "Notes"}, {Owner: 2, Title: "Log"}})
	h := startApp(t, bar, 40, 1)
	h.waitFor("both entries on the bar", func() bool {
		g := h.grid()
		return strings.Contains(g, "Notes") && strings.Contains(g, "Log")
	})
}
