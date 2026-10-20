package decl_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// reserveDoc is a Window with bars, a page, and two reserving Drawers: a window-controlled one on
// App.edge and a plain one at the bottom.
const reserveDoc = `import tui 1.0
import demo 1.0
Window {
 MenuBar { Dock.edge: Tui.Top; Menu { title: "File"; MenuItem { text: "Quit" } } }
 Probe { }
 StatusBar { Dock.edge: Tui.Bottom; left: "status-left" }
 Drawer { id: l; edge: App.edge; size: App.size; floating: App.floating; reserve: true; modal: false
  movable: true; windowResize: true; minimizable: true; maximizable: true
  Frame { title: "LEFT"; Text { text: "L" } } }
 Drawer { id: b; edge: Tui.Bottom; size: 30; reserve: true; modal: false
  Frame { title: "BOTTOM"; Text { text: "B" } } }
}`

// probe is the page: it draws PAGE at its top-left cell and records the width it was laid out at,
// which a page under a maximized panel still has.
type probe struct {
	widget.Base
	width atomic.Int64
}

func (p *probe) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
	p.width.Store(int64(sz.W))
	return sz
}

func (p *probe) Render(s tui.Surface) {
	for i, r := range "PAGE" {
		s.SetCell(i, 0, string(r), style.Style{})
	}
}

func runReserving(t *testing.T, w, h int) (*decltest.Screen, *probe) {
	t.Helper()
	page := &probe{}
	s := decltest.Run(t, w, h, tuidecl.LayoutSource("main.qml", []byte(reserveDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.edge": "left", "App.size": 25, "App.floating": false}),
		tuidecl.Types(tuidecl.Type{Name: "Probe", Build: func(tuidecl.Build) (tui.Component, []string, error) { return page, nil, nil }}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	onScreenLoop(t, s, func() {
		for _, id := range []string{"l", "b"} {
			if err := s.Program.Call(id, "open"); err != nil {
				t.Error(err)
			}
		}
	})
	return s, page
}

// at is where text starts on the screen; ok false when it is nowhere.
func at(screen, text string) (x, y int, ok bool) {
	for y, line := range strings.Split(screen, "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return len([]rune(line[:i])), y, true
		}
	}
	return 0, 0, false
}

// layoutIs waits for a screen with the page at pageX and the panels where want says, both read from
// one snapshot: a panel that stayed away from its strip never matches. It waits for the state to be
// reached, so it does not show that no earlier painted frame had them apart.
func layoutIs(t *testing.T, s *decltest.Screen, what string, pageX int, want func(sc string) bool) {
	t.Helper()
	s.WaitFor(t, what, func(sc string) bool {
		x, _, ok := at(sc, "PAGE")
		return ok && x == pageX && want(sc)
	})
}

// A docked, shown reserving Drawer takes its strip between the bars, and the page lays out beside
// it: the left panel is below the menu bar and above the status line, 25% of 60 is 15 cells, so the
// page starts at column 15, and the bottom panel (30% of 20 rows, 6) is beside the left one.
func TestAReservingDrawerTakesItsStripBetweenTheBars(t *testing.T) {
	s, _ := runReserving(t, 60, 20)
	layoutIs(t, s, "the strips", 15, func(sc string) bool {
		lines := strings.Split(sc, "\n")
		_, ly, lok := at(sc, "LEFT")
		bx, by, bok := at(sc, "BOTTOM")
		return strings.Contains(lines[0], "File") && strings.Contains(lines[len(lines)-1], "status-left") &&
			lok && ly == 1 && bok && by == 13 && bx == 17 // "┌ BOTTOM": the frame at 15, the title at 17
	})
}

// The strip and the panel follow a resize together: 25% of 80 is 20.
func TestAReservingDrawersStripFollowsAResize(t *testing.T) {
	s, _ := runReserving(t, 60, 20)
	layoutIs(t, s, "the strips at 60", 15, func(string) bool { return true })
	s.Backend.InjectResize(80, 24)
	layoutIs(t, s, "the strips at 80", 20, func(sc string) bool {
		x, _, ok := at(sc, "BOTTOM")
		return ok && x == 22
	})
}

// Every input to the strip moves the page in the frame the panel moves: closed, minimized and
// floating the page has the whole width; open, restored and docked it is beside the panel again; a
// new size and a new edge move it.
func TestEveryInputToAReservingDrawerMovesThePageWithIt(t *testing.T) {
	s, _ := runReserving(t, 60, 20)
	docked := func(sc string) bool { _, _, ok := at(sc, "LEFT"); return ok }
	gone := func(sc string) bool { _, _, ok := at(sc, "LEFT"); return !ok }
	call := func(method string) {
		onScreenLoop(t, s, func() {
			if err := s.Program.Call("l", method); err != nil {
				t.Error(err)
			}
		})
	}
	set := func(name string, v any) {
		onScreenLoop(t, s, func() {
			if err := s.Program.Set(name, v); err != nil {
				t.Error(err)
			}
		})
	}
	layoutIs(t, s, "open", 15, docked)
	call("close")
	layoutIs(t, s, "closed", 0, gone)
	call("open")
	layoutIs(t, s, "opened again", 15, docked)
	call("minimize")
	layoutIs(t, s, "minimized", 0, gone)
	call("restore")
	layoutIs(t, s, "restored", 15, docked)
	set("App.floating", true)
	layoutIs(t, s, "floating", 0, docked)
	set("App.floating", false)
	layoutIs(t, s, "docked again", 15, docked)
	set("App.size", 40)
	layoutIs(t, s, "40%", 24, docked)
	set("App.edge", "right")
	layoutIs(t, s, "at the right", 0, func(sc string) bool {
		x, _, ok := at(sc, "LEFT")
		return ok && x == 38 // "┌ LEFT" with the frame at column 36, 60 - 24
	})
}

// Maximized, the panel fills the Window and keeps its strip: the page beneath is not laid out
// wider, and on restore the panel is back over the strip and the page where it was.
func TestAMaximizedReservingDrawerKeepsItsStrip(t *testing.T) {
	s, page := runReserving(t, 60, 20)
	layoutIs(t, s, "docked", 15, func(string) bool { return true })
	before := page.width.Load()
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("l", "toggleMaximize"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "maximized", func(sc string) bool { _, _, ok := at(sc, "PAGE"); return !ok })
	if got := page.width.Load(); got != before {
		t.Fatalf("maximized, the page was laid out %d wide; want %d, its width beside the strip", got, before)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("l", "toggleMaximize"); err != nil {
			t.Error(err)
		}
	})
	layoutIs(t, s, "restored", 15, func(sc string) bool { _, y, ok := at(sc, "LEFT"); return ok && y == 1 })
	if got := page.width.Load(); got != before {
		t.Fatalf("restored, the page is %d wide; want %d", got, before)
	}
}

// A Drawer without reserve lies over the page as it always has: the page keeps column 0.
func TestADrawerWithoutReserveStillLiesOverThePage(t *testing.T) {
	doc := strings.ReplaceAll(reserveDoc, "reserve: true; ", "")
	page := &probe{}
	s := decltest.Run(t, 60, 20, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.edge": "left", "App.size": 25, "App.floating": false}),
		tuidecl.Types(tuidecl.Type{Name: "Probe", Build: func(tuidecl.Build) (tui.Component, []string, error) { return page, nil, nil }}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	width := page.width.Load()
	onScreenLoop(t, s, func() { _ = s.Program.Call("l", "open") })
	s.WaitForText(t, "LEFT")
	if got := page.width.Load(); got != width || width != 60 {
		t.Fatalf("an ordinary Drawer opened, the page is %d wide (was %d); want the Window's 60", got, width)
	}
}

// glyphAt is the cell at (x, y) of a screen dump, "" off it.
func glyphAt(screen string, x, y int) string {
	lines := strings.Split(screen, "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	row := []rune(lines[y])
	if x < 0 || x >= len(row) {
		return ""
	}
	return string(row[x])
}

// A keyboard move of a reserving Drawer starts from its strip, between the bars: one column right
// keeps its title on row 1, the strip's top. Started from the Window's edge rectangle (the whole
// height, 20 rows) it lands on row 0, over the menu bar. Moved, it floats and gives its strip back,
// so the bottom panel then spans the width, over the moved panel's lower rows.
func TestAKeyboardMoveOfAReservingDrawerStartsFromItsStrip(t *testing.T) {
	s, _ := runReserving(t, 60, 20)
	layoutIs(t, s, "docked", 15, func(sc string) bool { _, y, ok := at(sc, "LEFT"); return ok && y == 1 })
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("l", "moveBy", 1, 0); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "one column right of the strip", func(sc string) bool {
		x, y, ok := at(sc, "LEFT")
		return ok && x == 3 && y == 1
	})
}

// A keyboard resize of a reserving Drawer starts from its strip too: two columns wider keeps its
// corner at the strip's (column 0, row 1), its frame now reaching column 16.
func TestAKeyboardResizeOfAReservingDrawerStartsFromItsStrip(t *testing.T) {
	s, _ := runReserving(t, 60, 20)
	layoutIs(t, s, "docked", 15, func(sc string) bool { _, y, ok := at(sc, "LEFT"); return ok && y == 1 })
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("l", "resizeBy", 2, 0); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "two columns wider, at the strip's corner", func(sc string) bool {
		x, y, ok := at(sc, "LEFT")
		return ok && x == 2 && y == 1 && glyphAt(sc, 0, 1) == "┌" && glyphAt(sc, 16, 1) == "┐"
	})
}
