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

// An Editor's titleTabs puts its tabs on its Frame's title row, at the left, however deep the
// Editor sits: the title after them, the switch at the right. Without it, the frame's title row is
// as before, tabs or none.
func TestAnEditorsTitleTabsLeadItsFramesTitleRow(t *testing.T) {
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
	row0 := func(s *decltest.Screen) string { return strings.Split(s.String(), "\n")[0] }
	t.Run("on", func(t *testing.T) {
		s := decltest.RunWith(t, 60, 6, withTabs, switchDoc(strings.Replace(page, "%s", "titleTabs: true", 1)))
		s.WaitForText(t, "beta")
		top := row0(s)
		a, b, title, at := strings.Index(top, "alpha"), strings.Index(top, "beta"), strings.Index(top, "notes.csv"), strings.Index(top, sw)
		if !strings.HasPrefix(top, "┌─") || !(a > 0 && a < b && b < title && title < at) {
			t.Fatalf("row 0 %q; want ┌─, the tabs, the title, then the switch:\n%s", top, s)
		}
	})
	t.Run("off", func(t *testing.T) {
		s := decltest.RunWith(t, 60, 6, withTabs, switchDoc(strings.Replace(page, "%s", "titleTabs: false", 1)))
		s.WaitForText(t, sw)
		if top := row0(s); !strings.HasPrefix(top, "┌ notes.csv ") || strings.Contains(s.String(), "alpha") {
			t.Fatalf("titleTabs false: row 0 %q; want the title after the corner, no tabs:\n%s", top, s)
		}
	})
}

// The title row's tabs wear the palette as TerminalTabs's do: the active one the highlight pair.
func TestTheTitleRowTabsWearThePalette(t *testing.T) {
	s := decltest.RunWith(t, 60, 6, withTabs, switchDoc(`palette.highlight: "red"; palette.highlightedText: "white"
Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: "title"; titleTabs: true } }`))
	s.WaitForText(t, "alpha")
	s.WaitFor(t, "the active tab in the highlight", func(string) bool {
		c := cellOf(t, s, "alpha")
		return c.Attrs.BG == ansi(red) && c.Attrs.FG == ansi(white)
	})
}
