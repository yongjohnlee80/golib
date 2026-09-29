package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// drawer_test.go: a Drawer opens over the Window from its edge, moving nothing beneath it.

func runDrawerDoc(t *testing.T) *decltest.Screen {
	t.Helper()
	s := decltest.Run(t, 60, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Text { text: \"0123456789 the page underneath\" }\n"+
			" Drawer { id: d; edge: App.edge; size: 30\n  Frame { title: \"explorer\"; ListView { model: App.rows; textRole: \"name\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.edge": "left", "App.rows": people()}))
	s.WaitForText(t, "the page underneath")
	return s
}

func row(s *decltest.Screen, i int) string { return strings.Split(s.String(), "\n")[i] }

// col is the screen column sub starts at in line, -1 for none: characters, not bytes, since a
// frame's border is three bytes a cell.
func col(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return len([]rune(line[:i]))
}

// TestADrawerOpensOverThePageWithoutMovingIt: open, it covers the left third, and the page's text
// that shows is where it was; it moves to another edge while open; toggle and Escape close it.
func TestADrawerOpensOverThePageWithoutMovingIt(t *testing.T) {
	s := runDrawerDoc(t)
	before := col(row(s, 0), "underneath")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "explorer")
	if at := col(row(s, 0), "┌ explorer"); at != 0 {
		t.Fatalf("the drawer is at column %d of row 0, want 0 (the left edge):\n%s", at, s)
	}
	if at := col(row(s, 0), "underneath"); at != before {
		t.Fatalf("the page moved from column %d to %d under the drawer:\n%s", before, at, s)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.edge", "right"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the drawer on the right", func(string) bool {
		return col(row(s, 0), "┌ explorer") > 30 && strings.Contains(row(s, 0), "0123456789")
	})
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "toggle"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "toggled shut", func(sc string) bool { return !strings.Contains(sc, "explorer") })
	onScreenLoop(t, s, func() { _ = s.Program.Call("d", "toggle") })
	s.WaitForText(t, "explorer")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "Escape closed it", func(sc string) bool { return !strings.Contains(sc, "explorer") })
}

// TestADrawersEdgeIsOneOfTheFour: Tui.Left, Right, Top or Bottom, and nothing else.
func TestADrawersEdgeIsOneOfTheFour(t *testing.T) {
	if _, err := mountDoc(t, "import tui 1.0\nWindow { Drawer { edge: Tui.Vertical; Text { } } }"); err == nil {
		t.Fatal("Drawer { edge: Tui.Vertical } was not refused")
	}
}
