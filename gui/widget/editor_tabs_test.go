package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// TestTheEditorsTitleBarHoldsTheHostsTabs: the title bar's left holds the host's tabs (Tabs), up to
// two thirds of the bar, and Raw | Rendered sits at its right; a press on a tab tells the host.
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

	// more tabs than fit: they stop at two thirds of the bar (column 81 of 120), short of the
	// switch, which keeps its place
	h.onLoop(func() {
		var tabs []tuiwidget.StripTab
		for i := range 12 {
			tabs = append(tabs, tuiwidget.StripTab{ID: fmt.Sprint(i), Label: fmt.Sprintf("a-long-file-name-%02d.md", i)})
		}
		h.e.Tabs().SetTabs(tabs)
	})
	h.until("the tabs filling their share", func() bool { return strings.Contains(row(), "a-long-file-name-02") })
	r := []rune(row()) // by cell: the cut tab ends in a one-cell …
	if !strings.HasSuffix(row(), sw) || strings.TrimSpace(string(r[81:120-len(sw)])) != "" || strings.TrimSpace(string(r[1:81])) == "" {
		t.Fatalf("the bar with more tabs than fit:\n%q\nwant the tabs within columns 1-80, blank to the switch", row())
	}
}
