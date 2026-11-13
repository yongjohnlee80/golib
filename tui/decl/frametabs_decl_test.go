package decl_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
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

// An Editor's frameTabs puts its tabs on the first row inside its Frame, under the title row,
// however deep the Editor sits, as a panel of terminals carries its tabs: the title row keeps the
// title and the switch, and the bottom border is a plain border. Without it, no tabs are drawn.
func TestAnEditorsFrameTabsSitOnTheRowUnderItsFramesTitle(t *testing.T) {
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
		if a, b := strings.Index(r[1], "alpha"), strings.Index(r[1], "beta"); !strings.HasPrefix(r[1], "│") || !(a > 0 && a < b) {
			t.Fatalf("row 1 %q; want the tabs inside the frame, alpha then beta:\n%s", r[1], s)
		}
		if strings.Contains(bottom, "alpha") || strings.Contains(bottom, "beta") {
			t.Fatalf("the bottom border %q; want a plain border:\n%s", bottom, s)
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

// The frame's tabs wear the palette the Editor inherits, the window's: a highlight the Editor sets
// for its own text (its selection, say) does not reach them. Both are live: a change of the
// window's restyles them though the Editor's own still masks it on the Editor, and a change of the
// Editor's own leaves them in the window's.
func TestTheFrameTabsWearTheWindowsPaletteNotTheEditors(t *testing.T) {
	doc := `import tui 1.0
import demo 1.0
Window {
    palette.highlight: App.hl; palette.highlightedText: "white"
    Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: "title"; frameTabs: true; palette.highlight: App.ehl } }
}`
	s := decltest.RunWith(t, 60, 6, withTabs, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(map[string]any{"App.hl": "red", "App.ehl": "blue"}))
	s.WaitForText(t, "alpha")
	active := func(bg tui.CellColor) func(string) bool {
		return func(string) bool {
			c := cellOf(t, s, "alpha")
			return c.Attrs.BG == bg && c.Attrs.FG == ansi(white)
		}
	}
	s.WaitFor(t, "the active tab in the window's highlight, not the Editor's", active(ansi(red)))
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.hl", "green"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the active tab in the window's new highlight", active(ansi(green)))
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.ehl", "#a0a000"); err != nil {
			t.Error(err)
		}
	})
	time.Sleep(100 * time.Millisecond) // the Editor restyled: its page, not its tabs
	if c := cellOf(t, s, "alpha"); c.Attrs.BG != ansi(green) {
		t.Fatalf("the Editor's own highlight changed and the active tab followed it: %+v", c.Attrs)
	}
}
