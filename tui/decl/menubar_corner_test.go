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

// A Window with a MenuBar whose one non-Menu child is its corner, a flat Button there: the corner
// sits at the bar's right end, after the menus, its button bare and clickable, and it stays at the
// right end when the window widens.
func TestAMenuBarCornerWithAFlatButton(t *testing.T) {
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
 Frame { title: "page"; Text { text: "body" } }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.dock": func([]qml.SpecValue) error { clicked.Add(1); return nil }}))
	s.WaitForText(t, "body")
	bar := strings.TrimRight(strings.Split(s.String(), "\n")[0], " ")
	if !strings.HasSuffix(bar, "D") || strings.Contains(bar, "[") || strings.Index(bar, "Help") > strings.Index(bar, "D") {
		t.Fatalf("the bar: %q; want the menus, then the flat corner button D at its right end", bar)
	}
	// the window widened: the corner follows the bar's right end, 20 columns on
	at50 := len([]rune(bar)) - 1
	s.Backend.InjectResize(70, 12)
	s.WaitFor(t, "the corner 20 columns on", func(sc string) bool {
		row := strings.TrimRight(strings.Split(sc, "\n")[0], " ")
		return strings.HasSuffix(row, "D") && len([]rune(row))-1 == at50+20
	})
	bar = strings.TrimRight(strings.Split(s.String(), "\n")[0], " ")
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

// A Button's flat is a boolean: anything else is refused, naming it.
func TestAButtonsFlatIsABoolean(t *testing.T) {
	_, err := mountDoc(t, "import tui 1.0\nWindow {\n Button { text: \"x\"; flat: \"yes\" } }")
	if err == nil || !strings.Contains(err.Error(), "flat") {
		t.Fatalf("flat: \"yes\": %v", err)
	}
}
