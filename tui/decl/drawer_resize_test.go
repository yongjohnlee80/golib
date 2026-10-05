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

// A drag cancelled with Escape leaves the panel as it was: its size is kept only when a drag ends,
// and nothing is raised.
func TestACancelledDragLeavesTheDrawerAsItWas(t *testing.T) {
	// as a panel is: not modal, a list inside with the keyboard
	rec := &recorder{}
	s := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Text { text: \"the page underneath\" }\n"+
			" Drawer { id: d; modal: false; resizable: true; edge: Tui.Bottom; size: 30; onResized: App.log(size, length)\n"+
			"  Frame { title: \"panel\"; ListView { id: list; model: App.rows; textRole: \"name\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": people()}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "the page underneath")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
		if err := s.Program.Call("list", "forceActiveFocus"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	x, y, _ := find(s, "◥")
	before := panelTop(s)
	s.Keys(t,
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseMotion, X: x, Y: y - 4})
	s.WaitFor(t, "the drag shown", func(string) bool { return panelTop(s) == before-4 })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "the drawer back as it was", func(string) bool { return panelTop(s) == before })
	s.Keys(t, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y - 4})
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("a cancelled drag raised resized%v", got)
	}
	if panelTop(s) != before {
		t.Fatalf("after the release the drawer starts on row %d, want %d:\n%s", panelTop(s), before, s)
	}
}

// size and length a host sets are held to the Drawer's minimums too, not only a drag's.
func TestAHostsSizeIsHeldToTheMinimum(t *testing.T) {
	s := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Text { text: \"the page underneath\" }\n"+
			" Drawer { id: d; resizable: true; edge: Tui.Bottom; minimumSize: 20; size: App.size; length: App.length\n"+
			"  Frame { title: \"panel\"; Text { text: \"inside\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.size": 50, "App.length": 100}))
	s.WaitForText(t, "the page underneath")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the drawer at 50%", func(string) bool { return panelTop(s) == 10 })
	onScreenLoop(t, s, func() { // after the minimum is set, as a preference that changes is
		if err := s.Program.Set("App.size", 5); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the drawer at its minimum, 20% of 20 rows", func(string) bool { return panelTop(s) == 16 })
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.length", 5); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the drawer at its minimum length, 20% of 60 columns", func(string) bool {
		line := strings.Split(s.String(), "\n")[19]
		return col(line, "┘")-col(line, "└") == 11
	})
}

// A drag of one cell is one cell: a 60-wide bottom drawer dragged one column narrower is 59 wide,
// not 58 as flooring each way made it.
func TestADragOfOneCellIsOneCell(t *testing.T) {
	s, rec := runResizableDrawer(t, "bottom", true)
	x, y, _ := find(s, "◥")
	drag(s, t, x, y, -1, 0)
	s.WaitFor(t, "resized", func(string) bool { return len(rec.all()) == 2 })
	s.WaitFor(t, "59 wide", func(string) bool {
		line := strings.Split(s.String(), "\n")[19] // the bottom border: the grip covers the top's corner
		return col(line, "┘")-col(line, "└") == 58
	})
	// two columns narrower is 58 of 60: 96.7%, said as 97, the nearest, not 96
	x, y, _ = find(s, "◥")
	drag(s, t, x, y, -1, 0)
	s.WaitFor(t, "resized again", func(string) bool { return len(rec.all()) == 4 })
	if got := rec.all()[3].Raw; got != "97" {
		t.Errorf("58 of 60 columns reported as length %s, want 97", got)
	}
}

// A side drawer grows across by its width: a left one, 30% of 60 columns (18), dragged 6 columns
// right is 24 wide, 40%.
func TestASideDrawerGrowsAcrossByItsWidth(t *testing.T) {
	s, rec := runResizableDrawer(t, "left", true)
	x, y, _ := find(s, "◢")
	drag(s, t, x, y, 6, 0)
	s.WaitFor(t, "resized", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "40" || got[1].Raw != "100" {
		t.Fatalf("resized(%s, %s), want (40, 100)", got[0].Raw, got[1].Raw)
	}
	s.WaitFor(t, "24 wide", func(string) bool { gx, _, ok := find(s, "◢"); return ok && gx == 23 })
}

// minimumLength holds the length along the edge: raised past a drawer's length, it widens it; a
// drag stops at it; a length set below it is raised to it. A bottom drawer 30% of 60 columns (18)
// is 30 wide at a minimum of 50%.
func TestADrawersMinimumLengthHoldsItsLength(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Text { text: \"the page underneath\" }\n"+
			" Drawer { id: d; resizable: true; edge: Tui.Bottom; size: 30; length: App.length; minimumLength: App.min\n"+
			"  onResized: App.log(size, length)\n"+
			"  Frame { title: \"panel\"; Text { text: \"inside\" } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.length": 30, "App.min": 20}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "the page underneath")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	wide := func(w int) func(string) bool {
		return func(string) bool {
			line := strings.Split(s.String(), "\n")[19]
			return col(line, "┘")-col(line, "└") == w-1
		}
	}
	s.WaitFor(t, "18 of 60 columns", wide(18))
	set := func(name string, v int) {
		onScreenLoop(t, s, func() {
			if err := s.Program.Set(name, v); err != nil {
				t.Error(err)
			}
		})
	}
	set("App.min", 50)
	s.WaitFor(t, "widened to the minimum, 30 columns", wide(30))
	x, y, _ := find(s, "◥")
	drag(s, t, x, y, -50, 0)
	s.WaitFor(t, "resized at the minimum length", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all()[1].Raw; got != "50" {
		t.Fatalf("dragged far narrower, length is %s, want the minimum 50", got)
	}
	set("App.length", 10)
	set("App.length", 40) // moved off, then below again: each set is held
	set("App.length", 10)
	s.WaitFor(t, "held at 30 columns", wide(30))
}

// A resizable drawer at the top, or centred, is anchored there, its grip on its bottom right: the
// top one's on row 5 (6 rows of 20); a centred one 50% long, 30 of 60 columns, starts at column 15
// and row 7 (6 rows high), its grip at 44,12.
func TestAResizableDrawerIsAnchoredAtTheTopOrCentred(t *testing.T) {
	s, _ := runResizableDrawer(t, "top", true)
	s.WaitFor(t, "the grip at the top drawer's bottom right", func(string) bool {
		x, y, ok := find(s, "◢")
		return ok && x == 59 && y == 5 && panelTop(s) == 0
	})
	c := decltest.Run(t, 60, 20,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n"+
			" Text { text: \"the page underneath\" }\n"+
			" Drawer { id: d; resizable: true; edge: Tui.Center; size: 30; length: 50\n"+
			"  Frame { title: \"panel\"; Text { text: \"inside\" } } } }")))
	c.WaitForText(t, "the page underneath")
	onScreenLoop(t, c, func() {
		if err := c.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	c.WaitFor(t, "the grip at the centred drawer's bottom right", func(string) bool {
		x, y, ok := find(c, "◢")
		return ok && x == 44 && y == 12 && panelTop(c) == 7
	})
}
