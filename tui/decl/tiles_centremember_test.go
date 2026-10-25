package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tiles_centremember_test.go: a member at the centre is a member first. It minimizes as any member
// does, tiled or floating, and a minimized centre keeps no cells, so the strips are never released
// for it. Only the manager's own centre Tile never hides.

// centreMemberDoc is a 60×24 Window: the page, a FloatWindow at the centre keeping 40×8, and a side
// Drawer at the left keeping 30 columns, each tiled while its source says.
const centreMemberDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager { id: tm
  FloatWindow { id: page; minimizable: true; movable: true; resizable: true
   TileManager.edge: Tui.Center; TileManager.minimumWidth: 40; TileManager.minimumHeight: 8
   TileManager.tiled: App.pageTiled
   Frame { title: "PAGE"; Text { text: "page" } } }
  Drawer { id: side; modal: false; movable: true; windowResize: true; minimizable: true; closeOnQ: false
   TileManager.edge: Tui.Left; TileManager.minimumWidth: 30; TileManager.size: 50
   TileManager.tiled: App.sideTiled
   Frame { title: "SIDE"; Text { text: "side" } } }
 }
}`

func runCentreMember(t *testing.T) *decltest.Screen {
	t.Helper()
	s := decltest.Run(t, 60, 24, tuidecl.LayoutSource("main.qml", []byte(centreMemberDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.pageTiled": false, "App.sideTiled": false}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	return s
}

// minimized reports whether id's window is minimized.
func minimized(t *testing.T, s *decltest.Screen, id string) bool {
	t.Helper()
	var on bool
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		on = c.(interface{ WindowBehavior() *widget.WindowCore }).WindowBehavior().State().Minimized
	})
	return on
}

// The page minimizes floating, and minimizes tiled at the centre too: the same window, the same
// capability, wherever it is placed. Restored, it is over its tile again, the whole manager.
func TestACentreMemberMinimizesTiledAsItDoesFloating(t *testing.T) {
	s := runCentreMember(t)
	call(t, s, "page", "minimize")
	s.WaitFor(t, "the floating page minimized", func(string) bool { return minimized(t, s, "page") })
	call(t, s, "page", "restore")
	s.WaitFor(t, "the floating page restored", func(string) bool { return !minimized(t, s, "page") })
	set(t, s, "App.pageTiled", true)
	s.WaitFor(t, "the page over its tile", func(string) bool { return governed(t, s, "page") })
	call(t, s, "page", "minimize")
	s.WaitFor(t, "the tiled page minimized, off the screen", func(sc string) bool {
		return minimized(t, s, "page") && !strings.Contains(sc, "PAGE")
	})
	if !governed(t, s, "page") {
		t.Fatal("the minimized page left its tile; a minimized member stays governed")
	}
	call(t, s, "page", "restore")
	waitScreenRect(t, s, "page", tui.Rect{X: 0, Y: 0, W: 60, H: 24})
}

// The page minimized while it floats, then tiled while hidden, then the side tiled. The hidden page
// keeps no cells, so the side, keeping 30 of the 60, is not released for the page's 40: it is over
// its tile, and, the centre away, takes the whole manager.
func TestAHiddenCentreMemberKeepsNoCellsFromTheStrips(t *testing.T) {
	s := runCentreMember(t)
	call(t, s, "page", "minimize")
	s.WaitFor(t, "the floating page minimized", func(string) bool { return minimized(t, s, "page") })
	set(t, s, "App.pageTiled", true)
	s.WaitFor(t, "the hidden page governed", func(string) bool { return governed(t, s, "page") })
	call(t, s, "side", "open")
	set(t, s, "App.sideTiled", true)
	s.WaitFor(t, "the side over its tile", func(string) bool { return governed(t, s, "side") })
	waitScreenRect(t, s, "side", tui.Rect{X: 0, Y: 0, W: 60, H: 24})
	if !governed(t, s, "page") || !minimized(t, s, "page") {
		t.Fatal("the page is no longer the hidden, governed centre")
	}
}

// alongDoc is AutoDoc's base: the page Tile at the centre keeping PAGE, an explorer Drawer at EX
// asking 90%, declared before a todos Drawer at TODOSEDGE asking 30%. TODOS is the todos' minimum:
// its explicit length along its edge, or, Min-only, a minimumSize across it.
const alongDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager { id: tm
  Drawer { id: ex; modal: false; movable: true; windowResize: true; closeOnQ: false
   TileManager.edge: EX; TileManager.size: 90; TileManager.minimumSize: 4; TileManager.tiled: true
   Frame { title: "EXPLORER"; Text { text: "e" } } }
  Drawer { id: todos; modal: false; movable: true; windowResize: true; closeOnQ: false
   TileManager.edge: TODOSEDGE; TileManager.size: 30; TODOS; TileManager.tiled: true
   Frame { title: "TODOS"; Text { text: "t" } } }
  Tile { id: pg; edge: Tui.Center; PAGE; Frame { title: "PAGE"; Text { text: "p" } } }
 }
}`

// alongCase is alongDoc laid out one way: the Window's size, each edge, and the minimums.
type alongCase struct {
	name                 string
	w, h                 int
	ex, todosEdge, page  string
	todosAlong, todosMin string // the todos' explicit along-edge minimum, and their Min-only one
	todosAlone           tui.Rect
}

// alongCases are the bottom and left case, AutoDoc's, and its transpose: the explorer at the right,
// the todos at the top keeping 16 columns along it, so the guard on each axis is observed.
var alongCases = []alongCase{
	{name: "todos at the left", w: 80, h: 20, ex: "Tui.Bottom", todosEdge: "Tui.Left", page: "minimumWidth: 40; minimumHeight: 8",
		todosAlong: "TileManager.minimumWidth: 20; TileManager.minimumHeight: 16", todosMin: "TileManager.minimumSize: 20",
		todosAlone: tui.Rect{X: 0, Y: 0, W: 24, H: 20}},
	{name: "todos at the top", w: 20, h: 80, ex: "Tui.Right", todosEdge: "Tui.Top", page: "minimumWidth: 8; minimumHeight: 40",
		todosAlong: "TileManager.minimumWidth: 16; TileManager.minimumHeight: 20", todosMin: "TileManager.minimumSize: 20",
		todosAlone: tui.Rect{X: 0, Y: 0, W: 20, H: 24}},
}

func runAlong(t *testing.T, c alongCase, todosMin string) *decltest.Screen {
	t.Helper()
	doc := strings.NewReplacer("EX;", c.ex+";", "TODOSEDGE;", c.todosEdge+";", "TODOS;", todosMin+";", "PAGE;", c.page+";").Replace(alongDoc)
	s := decltest.Run(t, c.w, c.h, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	return s
}

// The todos tiled at the left keep 16 rows along it. The explorer tiled next, at the bottom and
// declared first, would leave them 8 of the 20, so the two don't fit: the explorer, tiled last, is
// released, and the todos keep the whole height. Transposed, the todos at the top keep 16 columns
// along it, and the explorer at the right would leave them 8 of the 20.
func TestAStripsLengthAlongItsEdgeDecidesWhatFits(t *testing.T) {
	for _, c := range alongCases {
		t.Run(c.name, func(t *testing.T) {
			s := runAlong(t, c, c.todosAlong)
			call(t, s, "todos", "open")
			s.WaitFor(t, "the todos over their tile", func(string) bool { return governed(t, s, "todos") })
			call(t, s, "ex", "open")
			s.WaitFor(t, "the explorer on the screen", func(string) bool { _, on := screenRect(t, s, "ex"); return on })
			waitScreenRect(t, s, "todos", c.todosAlone)
			if governed(t, s, "ex") {
				t.Fatal("the explorer is tiled, leaving the todos shorter along their edge than their 16 cells")
			}
		})
	}
}

// With minimumSize alone, a strip's minimum is across its edge only: the same two tile as before,
// the todos 8 rows high beside the page under the explorer's 12.
func TestAMinOnlyStripLaysOutAsBefore(t *testing.T) {
	s := runAlong(t, alongCases[0], alongCases[0].todosMin)
	call(t, s, "todos", "open")
	call(t, s, "ex", "open")
	s.WaitFor(t, "both over their tiles", func(string) bool { return governed(t, s, "todos") && governed(t, s, "ex") })
	waitScreenRect(t, s, "todos", tui.Rect{X: 0, Y: 0, W: 24, H: 8})
	waitScreenRect(t, s, "ex", tui.Rect{X: 0, Y: 8, W: 80, H: 12})
}

// A member's edge never moves to or from the centre: the page, declared there, stays there when its
// bound TileManager.edge says left.
func TestAMembersEdgeNeverLeavesTheCentre(t *testing.T) {
	doc := strings.Replace(centreMemberDoc, "TileManager.edge: Tui.Center; TileManager.minimumWidth: 40;",
		"TileManager.edge: App.pageEdge; TileManager.minimumWidth: 20;", 1) // 20 and the side's 30 fit 60
	s := decltest.Run(t, 60, 24, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.pageTiled": true, "App.sideTiled": false, "App.pageEdge": "center"}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	waitScreenRect(t, s, "page", tui.Rect{X: 0, Y: 0, W: 60, H: 24})
	set(t, s, "App.pageEdge", "left")
	call(t, s, "side", "open")
	set(t, s, "App.sideTiled", true)
	waitScreenRect(t, s, "page", tui.Rect{X: 30, Y: 0, W: 30, H: 24}) // still the centre, beside the side
}

// orderDoc is an 80×24 Window: the page Tile at the centre keeping 30 columns, a Drawer at each
// side asking a quarter, their minimums live.
const orderDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager { id: tm
  Drawer { id: a; modal: false; movable: true; windowResize: true; closeOnQ: false
   TileManager.edge: Tui.Left; TileManager.size: 25; TileManager.minimumWidth: App.aMin
   Frame { title: "AAA"; Text { text: "a" } } }
  Drawer { id: b; modal: false; movable: true; windowResize: true; closeOnQ: false
   TileManager.edge: Tui.Right; TileManager.size: 25; TileManager.minimumWidth: App.bMin
   Frame { title: "BBB"; Text { text: "b" } } }
  Tile { id: pg; edge: Tui.Center; minimumWidth: 30; Frame { title: "PAGE"; Text { text: "p" } } }
 }
}`

// The release order is the order the members came into play, and one brought back comes in again:
// a and b, tiled from the start, fit. a is closed, b now keeps 40 columns, and a is opened again,
// so the two no longer fit beside the page. a, just brought back, is released, and b keeps its tile,
// though a asked first.
func TestAMemberBroughtBackIsTheMostRecentlyInPlay(t *testing.T) {
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(orderDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.aMin": 20, "App.bMin": 20}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	call(t, s, "a", "open")
	call(t, s, "b", "open")
	waitScreenRect(t, s, "b", tui.Rect{X: 60, Y: 0, W: 20, H: 24})
	call(t, s, "a", "close")
	set(t, s, "App.bMin", 40)
	waitScreenRect(t, s, "b", tui.Rect{X: 40, Y: 0, W: 40, H: 24})
	call(t, s, "a", "open")
	s.WaitFor(t, "a released, b kept", func(string) bool { return !governed(t, s, "a") && governed(t, s, "b") })
	waitScreenRect(t, s, "b", tui.Rect{X: 40, Y: 0, W: 40, H: 24})
}
