package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// withTabs gives the Editor declared with id e a table document view, as withTable does, and two
// tabs, alpha active, as a host gives it its buffers.
func withTabs(p *tuidecl.Program) error {
	if err := withTable(p); err != nil {
		return err
	}
	c, _ := p.Find("e")
	if ed, ok := tuidecl.EditorOf(c); ok {
		ed.Tabs().SetTabs([]core.StripTab{{ID: "1", Label: "alpha"}, {ID: "2", Label: "beta"}})
		ed.Tabs().SetActive("1")
	}
	return nil
}

// An Editor's frameTabs puts its tabs at its Frame's title row's left, however deep the Editor sits,
// where a panel carries its tabs: the title follows them, the switch keeps the row's right, and the
// bottom border is a plain border. Without it, no tabs are drawn.
func TestAnEditorsFrameTabsSitAtItsFramesTitleRowsLeft(t *testing.T) {
	const page = `Frame {
    title: "notes.csv"
    Flex {
        direction: Tui.Vertical
        FocusScope {
            Layout.fillHeight: true
            Editor { id: e; text: "a,b"; modeSwitch: "title"; %s }
        }
    }
}`
	rows := func(s *decltest.Screen) []string { return strings.Split(s.String(), "\n") }
	t.Run("on", func(t *testing.T) {
		s := decltest.RunWith(t, 60, 6, withTabs, switchDoc(strings.Replace(page, "%s", "frameTabs: true", 1)))
		s.WaitForText(t, "beta")
		r := rows(s)
		bottom := ""
		for _, row := range r {
			if strings.HasPrefix(row, "└") {
				bottom = row
			}
		}
		a, b, title := strings.Index(r[0], "alpha"), strings.Index(r[0], "beta"), strings.Index(r[0], "notes.csv")
		if !strings.HasPrefix(r[0], "┌─") || !(a > 0 && a < b && b < title) || !strings.Contains(r[0], sw) {
			t.Fatalf("row 0 %q; want ┌─, the tabs, the title, then the switch:\n%s", r[0], s)
		}
		if strings.Contains(bottom, "alpha") || strings.Contains(bottom, "beta") {
			t.Fatalf("the bottom border %q; want a plain border, the tabs on the title row:\n%s", bottom, s)
		}
	})
	t.Run("off", func(t *testing.T) {
		s := decltest.RunWith(t, 60, 6, withTabs, switchDoc(strings.Replace(page, "%s", "frameTabs: false", 1)))
		s.WaitForText(t, sw)
		if strings.Contains(s.String(), "alpha") {
			t.Fatalf("frameTabs false: tabs drawn:\n%s", s)
		}
	})
}

// The frame's tabs wear the palette as TerminalTabs's do: the active one the highlight pair.
func TestTheFrameTabsWearThePalette(t *testing.T) {
	s := decltest.RunWith(t, 60, 6, withTabs, switchDoc(`palette.highlight: "red"; palette.highlightedText: "white"
Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: "title"; frameTabs: true } }`))
	s.WaitForText(t, "alpha")
	s.WaitFor(t, "the active tab in the highlight", func(string) bool {
		c := cellOf(t, s, "alpha")
		return c.Attrs.BG == ansi(red) && c.Attrs.FG == ansi(white)
	})
}
