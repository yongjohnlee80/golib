package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// drawer_resize_test.go: a resizable Drawer's corner grip drags it larger and smaller, within its
// bounds, and says where it was left once.

func runResizableDrawer(t *testing.T, edge string, resizable bool) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Text { text: \"the page underneath\" }\n"+
			" Drawer { id: d; resizable: "+map[bool]string{true: "true", false: "false"}[resizable]+"; edge: App.edge; size: 30; length: 100\n"+
			"  minimumSize: 20; onResized: App.log(size, length)\n"+
			"  Frame { title: \"panel\"; Text { text: \"inside\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.edge": edge}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "the page underneath")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	return s, rec
}

// find is the cell a glyph is drawn in, ok false when it is nowhere.
func find(s *decltest.Screen, glyph string) (x, y int, ok bool) {
	for y, row := range s.Backend.Snapshot() {
		for x, c := range row {
			if c.Content == glyph {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// panelTop is the row the panel's frame starts on (its title).
func panelTop(s *decltest.Screen) int {
	for y, line := range strings.Split(s.String(), "\n") {
		if strings.Contains(line, "panel") {
			return y
		}
	}
	return -1
}

func drag(s *decltest.Screen, t *testing.T, x, y, dx, dy int) {
	t.Helper()
	s.Keys(t,
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseMotion, X: x + dx, Y: y + dy},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x + dx, Y: y + dy})
}

// A bottom drawer's grip is a square on its top-right corner; dragged up 4 rows it grows 4 rows
// taller, from 30% of 20 rows (6) to 50% (10), and resized says so once, on release.
func TestAResizableDrawerGrowsByItsCornerGrip(t *testing.T) {
	s, rec := runResizableDrawer(t, "bottom", true)
	x, y, ok := find(s, "◥")
	if !ok {
		t.Fatalf("no grip on the bottom drawer:\n%s", s)
	}
	if top := panelTop(s); y != top || x != 59 {
		t.Fatalf("the grip is at %d,%d, want the top right corner (59,%d):\n%s", x, y, top, s)
	}
	before := panelTop(s)
	drag(s, t, x, y, 0, -4)
	s.WaitFor(t, "the drawer 4 rows taller", func(string) bool { return panelTop(s) == before-4 })
	s.WaitFor(t, "resized once", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "50" || got[1].Raw != "100" {
		t.Fatalf("resized(%s, %s), want (50, 100)", got[0].Raw, got[1].Raw)
	}
}

// Dragged past its bounds, a drawer stops at them: down to minimumSize (20%: 4 rows), up to 90%.
func TestAResizableDrawerStopsAtItsBounds(t *testing.T) {
	s, rec := runResizableDrawer(t, "bottom", true)
	x, y, _ := find(s, "◥")
	drag(s, t, x, y, 0, 10) // far below its minimum
	s.WaitFor(t, "resized at the minimum", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "20" {
		t.Fatalf("dragged down past its minimum, size is %s, want 20", got[0].Raw)
	}
	if top := panelTop(s); top != 20-4 {
		t.Fatalf("at its minimum the drawer starts on row %d, want %d:\n%s", top, 20-4, s)
	}
	x, y, _ = find(s, "◥")
	drag(s, t, x, y, 0, -30) // far above the top
	s.WaitFor(t, "resized at the maximum", func(string) bool { return len(rec.all()) == 4 })
	if got := rec.all(); got[2].Raw != "90" {
		t.Fatalf("dragged up past 90%%, size is %s, want 90", got[2].Raw)
	}
}

// Each edge's grip is on its inner corner: a left drawer's on the bottom right, a right one's on
// the bottom left; a drawer that is not resizable has none.
func TestADrawersGripIsOnItsInnerCorner(t *testing.T) {
	s, _ := runResizableDrawer(t, "left", true)
	if x, y, ok := find(s, "◢"); !ok || x != 60*30/100-1 || y != 19 {
		t.Fatalf("a left drawer's grip is at %d,%d (%v), want its bottom right (17,19):\n%s", x, y, ok, s)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.edge", "right"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the grip on the right drawer's bottom left", func(string) bool {
		x, y, ok := find(s, "◣")
		return ok && x == 60-60*30/100 && y == 19
	})
	plain, _ := runResizableDrawer(t, "bottom", false)
	for _, g := range []string{"◢", "◣", "◥"} {
		if _, _, ok := find(plain, g); ok {
			t.Fatalf("a drawer that is not resizable has a grip %s:\n%s", g, plain)
		}
	}
}
