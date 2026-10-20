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

// The switch's words are golib's catalog messages: in Korean the bar shows them, a click lands on
// the segment drawn, and a panel's title message follows the language too.
func TestTheSwitchAndThePanelTitleFollowTheLanguage(t *testing.T) {
	h := startEditor(t, 80, 6, WithRenderer(NewMarkdownRenderer()), WithPanel(WithTitleMessage(tui.Msg("tui.cmdline.title"))))
	row := func() string { return strings.Split(h.tb.String(), "\n")[0] }
	h.until("English", func() bool { return strings.HasSuffix(row(), "[ Raw | Rendered ]") })
	h.onLoop(func() { h.app.ApplyLanguage("ko_KR") })
	const sw = "[ 원문 | 렌더링 ]"
	h.until("Korean", func() bool { return strings.HasSuffix(strings.TrimRight(row(), " "), sw) })
	if r := row(); strings.Contains(r, "Raw") || strings.Contains(r, "Rendered") {
		t.Errorf("English is left on the Korean bar: %q", r)
	}
	var title string
	h.onLoop(func() { title = h.e.panel.title })
	if title != "VIM 명령" {
		t.Errorf("the panel's title in Korean: %q", title)
	}
	// "[ 원문 " is 7 cells, then the separator: a click past it is Rendered, before it Raw
	var swAt int
	h.onLoop(func() { // at the bar's right end
		ctx := h.e.sw.Context()
		swAt = 80 - (h.e.sw.rawW + ctx.StringWidth(switchSep) + ctx.StringWidth(h.e.sw.rendered))
	})
	click := func(x int) EditorMode {
		_ = h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: swAt + x, Y: 0})
		h.barrier()
		var m EditorMode
		h.onLoop(func() { m = h.e.mode })
		return m
	}
	if m := click(9); m != Rendered {
		t.Errorf("a click on 렌더링: mode %v", m)
	}
	if m := click(6); m != Raw { // the last cell of "[ 원문 ", past English's six
		t.Errorf("a click on 원문's last cell: mode %v", m)
	}
}
