package decl_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// A Window with a MenuBar whose one non-Menu child is its corner, a flat Button there, and a page
// in a Raisable capped to 20 columns: the corner sits at the bar's right end, after the menus, its
// button bare and clickable, and the page is centred in its place.
func TestARaisablePageAndAMenuBarCornerWithAFlatButton(t *testing.T) {
	var clicked atomic.Int32
	s := decltest.Run(t, 50, 12,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
 MenuBar { Dock.edge: Tui.Top
  Menu { title: "&File"; MenuItem { text: "&New" } }
  Menu { title: "&Help"; MenuItem { text: "&Keys" } }
  Flex { direction: Tui.Horizontal; Button { text: "D"; flat: true; onClicked: App.dock() } }
 }
 Raisable { maximumWidth: 20; Frame { title: "page"; Text { text: "body" } } }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.dock": func([]qml.SpecValue) error { clicked.Add(1); return nil }}))
	s.WaitForText(t, "body")
	rows := strings.Split(s.String(), "\n")
	bar := strings.TrimRight(rows[0], " ")
	if !strings.HasSuffix(bar, "D") || strings.Contains(bar, "[") || strings.Index(bar, "Help") > strings.Index(bar, "D") {
		t.Fatalf("the bar: %q; want the menus, then the flat corner button D at its right end", bar)
	}
	page := -1
	for _, r := range rows {
		if i := strings.Index(r, "┌ page"); i >= 0 {
			page = len([]rune(r[:i]))
		}
	}
	if page != 15 {
		t.Fatalf("the page's frame starts at column %d; want 15, centred in 50:\n%s", page, s.String())
	}
	// the window widened: the page's layer moves with its place, in the same frame
	s.Backend.InjectResize(70, 12)
	s.WaitFor(t, "the page centred in 70", func(sc string) bool {
		for _, r := range strings.Split(sc, "\n") {
			if i := strings.Index(r, "┌ page"); i >= 0 {
				return len([]rune(r[:i])) == 25
			}
		}
		return false
	})
	rows = strings.Split(s.String(), "\n")
	bar = strings.TrimRight(rows[0], " ")
	x := len([]rune(bar)) - 1
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: 0})
	// the click and its handler travel different lanes: wait for the handler
	s.WaitFor(t, "the corner's button clicked", func(string) bool { return clicked.Load() == 1 })
}

// A MenuBar holds at most one corner widget.
func TestAMenuBarRefusesASecondCorner(t *testing.T) {
	_, err := mountDoc(t, "import tui 1.0\nWindow {\n MenuBar {\n  Menu { title: \"&File\" }\n  Text { text: \"a\" }\n  Text { text: \"b\" } } }")
	if err == nil || !strings.Contains(err.Error(), "at most one other child") {
		t.Fatalf("two corners: %v", err)
	}
}
