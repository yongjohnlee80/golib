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
