package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// TestTheEditorsTitleBarHoldsTheHostsTabs: the title bar's left holds the host's tabs (Tabs), in all
// its room up to Raw | Rendered at its right; a press on a tab tells the host.
func TestTheEditorsTitleBarHoldsTheHostsTabs(t *testing.T) {
	h := startEditor(t, 120, 6, WithRenderer(NewMarkdownRenderer()))
	row := func() string { return strings.Split(h.tb.String(), "\n")[0] }
	const sw = "[ Raw | Rendered ]"
	h.until("the switch at the bar's right", func() bool { return strings.HasSuffix(row(), sw) })
	if strings.TrimSpace(row()[:120-len(sw)]) != "" {
		t.Fatalf("no tabs set, yet the bar's left holds %q", row())
	}

	var chosen []string
	h.onLoop(func() {
		h.e.Tabs().SetTabs([]tuiwidget.StripTab{{ID: "1", Label: "a.md"}, {ID: "2", Label: "b.md"}})
		h.e.Tabs().SetActive("1")
		h.e.Tabs().SetOnSelect(func(id string) { chosen = append(chosen, id) })
	})
	h.until("the tabs at the bar's left", func() bool { return strings.HasPrefix(row(), "  a.md   b.md ") })
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1 + len(" a.md ") + 2, Y: 0}); err != nil {
		t.Fatal(err)
	}
	h.until("b.md chosen", func() bool {
		var active string
		h.onLoop(func() { active = h.e.Tabs().Active() })
		return active == "2" && len(chosen) == 1 && chosen[0] == "2"
	})

	// more tabs than fit: they take the bar's room up to the switch (column 102 of 120), a blank
	// cell before it, and the switch keeps its place (was: two thirds of the bar, to column 81)
	h.onLoop(func() {
		var tabs []tuiwidget.StripTab
		for i := range 12 {
			tabs = append(tabs, tuiwidget.StripTab{ID: fmt.Sprint(i), Label: fmt.Sprintf("a-long-file-name-%02d.md", i)})
		}
		h.e.Tabs().SetTabs(tabs)
	})
	h.until("the tabs filling their room", func() bool { return strings.Contains(row(), "a-long-file-name-02") })
	r := []rune(row()) // by cell: the cut tab ends in a one-cell …
	swAt := 120 - len(sw)
	if !strings.HasSuffix(row(), sw) || r[swAt-1] != ' ' || strings.TrimSpace(string(r[82:swAt-1])) == "" {
		t.Fatalf("the bar with more tabs than fit:\n%q\nwant the tabs past column 81, up to a blank cell before the switch", row())
	}
}
