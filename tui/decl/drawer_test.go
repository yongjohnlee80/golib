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

func TestADrawerClosesOnQOnlyWhenRequested(t *testing.T) {
	for _, tc := range []struct {
		name     string
		closeOnQ string
		closes   bool
	}{
		{"default", "", false},
		{"enabled", "closeOnQ: true;", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := decltest.Run(t, 60, 10,
				tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow { Drawer { id: d; "+tc.closeOnQ+
					" Frame { title: \"explorer\"; ListView { model: App.rows; textRole: \"name\" } } } }")),
				tuidecl.Singleton("demo", "1.0", "App"),
				tuidecl.Sources(map[string]any{"App.rows": people()}))
			onScreenLoop(t, s, func() { _ = s.Program.Call("d", "open") })
			s.WaitForText(t, "explorer")
			s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'q', Mods: tui.ModCtrl})
			s.WaitForText(t, "explorer")
			s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'q'})
			if tc.closes {
				s.WaitFor(t, "q closed the drawer", func(sc string) bool { return !strings.Contains(sc, "explorer") })
			} else {
				s.WaitForText(t, "explorer")
			}
		})
	}
}

// TestADrawersLengthIsCentredAlongItsEdge: length: 50 on a 20-row Window, from the left, takes
// the middle 10 rows, the rows above and below showing the page; from the top, the middle 30 of
// 60 columns.
func TestADrawersLengthIsCentredAlongItsEdge(t *testing.T) {
	page := strings.Repeat("page ", 12)
	s := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Flex { direction: Tui.Vertical\n"+strings.Repeat(" Text { text: \""+page+"\" }\n", 20)+" }\n"+
			" Drawer { id: d; edge: App.edge; size: 30; length: 50\n  Frame { title: \"explorer\"; Text { text: \"x\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.edge": "left"}))
	s.WaitForText(t, "page")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "explorer")
	var top, bottom = -1, -1
	for y := range 20 {
		if strings.HasPrefix(row(s, y), "┌ explorer") {
			top = y
		}
		if strings.HasPrefix(row(s, y), "└") {
			bottom = y
		}
	}
	if top != 5 || bottom != 14 || !strings.HasPrefix(row(s, 4), "page") || !strings.HasPrefix(row(s, 15), "page") {
		t.Fatalf("the drawer spans rows %d to %d, want 5 to 14, the page above and below:\n%s", top, bottom, s)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.edge", "top"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the drawer on the top", func(string) bool { return col(row(s, 0), "┌ explorer") == 15 })
	if r := []rune(row(s, 0)); r[44] != '┐' || r[45] == '─' {
		t.Fatalf("from the top, the drawer does not end at column 44:\n%s", s)
	}
}

// TestADrawersEdgeIsOneOfTheFour: Tui.Left, Right, Top or Bottom, and nothing else.
func TestADrawersEdgeIsOneOfTheFour(t *testing.T) {
	if _, err := mountDoc(t, "import tui 1.0\nWindow { Drawer { edge: Tui.Vertical; Text { } } }"); err == nil {
		t.Fatal("Drawer { edge: Tui.Vertical } was not refused")
	}
}

// TestANonModalDrawerLetsTheKeyboardGo: open and not modal, focus can go to the page under it and
// the drawer stays open.
func TestANonModalDrawerLetsTheKeyboardGo(t *testing.T) {
	s := decltest.Run(t, 60, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Editor { id: page; focus: true }\n"+
			" Drawer { id: d; modal: false; edge: Tui.Right; Frame { title: \"explorer\"; ListView { id: list; model: App.rows; textRole: \"name\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": people()}))
	onScreenLoop(t, s, func() { _ = s.Program.Call("d", "open") })
	s.WaitForText(t, "explorer")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("page", "forceActiveFocus"); err != nil {
			t.Error(err)
		}
	})
	s.Keys(t, decltest.Rune('i'))
	s.Keys(t, decltest.Type("typed")...)
	s.WaitFor(t, "typing reached the page, the drawer open", func(sc string) bool {
		return strings.Contains(sc, "typed") && strings.Contains(sc, "explorer")
	})
}
