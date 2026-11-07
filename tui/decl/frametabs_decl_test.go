package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// withTabs gives the Editor declared with id e a table document view, as withTable does, and two
// tabs, alpha active, as a host gives it its buffers.
func withTabs(p *tuidecl.Program) error {
	if err := withTable(p); err != nil {
		return err
	}
	c, _ := p.Find("e")
	if ed, ok := tuidecl.EditorOf(c); ok {
		ed.Tabs().SetTabs([]widget.StripTab{{ID: "1", Label: "alpha"}, {ID: "2", Label: "beta"}})
		ed.Tabs().SetActive("1")
	}
	return nil
}

// An Editor's frameTabs puts its tabs on its Frame's bottom border, however deep the Editor sits;
// the title row keeps the title and the switch as without them. Without it, no tabs are drawn.
func TestAnEditorsFrameTabsSitOnItsFramesBottomBorder(t *testing.T) {
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
		if !strings.HasPrefix(r[0], "┌ notes.csv ") || !strings.Contains(r[0], sw) || strings.Contains(r[0], "alpha") {
			t.Fatalf("row 0 %q; want the title and the switch, no tabs:\n%s", r[0], s)
		}
		if a, b := strings.Index(bottom, "alpha"), strings.Index(bottom, "beta"); !strings.HasPrefix(bottom, "└─") || !(a > 0 && a < b) {
			t.Fatalf("the bottom border %q; want └─, then the tabs:\n%s", bottom, s)
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
