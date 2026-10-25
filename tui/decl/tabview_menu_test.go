package decl_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A host gives a TabView's bar a menu through the node Find returns: ≡ shows on the bar, a press
// on it opens the host's rows right under the bar (the panel's border on the next row), and a
// chosen row runs with the current tab's index.
func TestAHostGivesATabViewsBarAMenu(t *testing.T) {
	ran := make(chan int, 1)
	s := decltest.RunWith(t, 40, 6, func(p *tuidecl.Program) error {
		c, _ := p.Find("tabs")
		m, ok := c.(interface {
			SetMenu(func(i int) []widget.MenuItemModel)
		})
		if !ok {
			t.Fatalf("the TabView found (%T) takes no menu", c)
		}
		m.SetMenu(func(i int) []widget.MenuItemModel {
			return []widget.MenuItemModel{widget.NewCommand("layout", "Layout",
				widget.TabsMenuAction{ID: "layout", Run: func(i int) { ran <- i }})}
		})
		return nil
	}, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
 TabView { id: tabs; currentIndex: 1
  Tab { title: "Edit"; Text { text: "edit" } }
  Tab { title: "Advanced"; Text { text: "advanced" } } } }`)), tuidecl.Types(controls.Types()...))
	s.WaitForText(t, "≡")
	bar, x := -1, -1
	for i, line := range strings.Split(s.String(), "\n") {
		if j := strings.Index(line, "≡"); j >= 0 {
			bar, x = i, len([]rune(line[:j]))
			break
		}
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: bar})
	s.WaitFor(t, "the menu right under the bar", func(screen string) bool {
		lines := strings.Split(screen, "\n")
		return bar+2 < len(lines) && strings.Contains(lines[bar+1], "┌") && strings.Contains(lines[bar+2], "Layout")
	})
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	select {
	case i := <-ran:
		if i != 1 {
			t.Errorf("Layout ran with tab %d, want the current one, 1", i)
		}
	case <-time.After(decltest.WaitTimeout):
		t.Fatalf("Enter on Layout ran nothing:\n%s", s)
	}
}
