package decl_test

import (
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tiles_minaxes_test.go: a tile's minimum on each axis, as a document writes it: a Tile's own
// minimumWidth and minimumHeight, and a member's TileManager.minimumWidth and .minimumHeight, live.

// minAxesDoc is an 80×24 Window: the Drawer ex a member asking 10% at the left, App.exMinW columns
// or 6 rows across its edge, beside a page that wants 40 columns and 8 rows. ex's own place is a
// right-docked third.
const minAxesDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager {
  Drawer { id: ex; modal: false; windowResize: true; movable: true; minimizable: true; edge: Tui.Right; size: 30; closeOnQ: false
    TileManager.edge: App.exEdge; TileManager.edges: "left bottom"; TileManager.size: 10
    TileManager.minimumWidth: App.exMinW; TileManager.minimumHeight: 6
    Frame { title: "EXPLORER"; Text { text: "e" } } }
  Tile { id: pg; edge: Tui.Center; minimumWidth: 40; minimumHeight: 8; Frame { title: "PAGE"; Text { text: "p" } } }
 }
}`

// The member keeps App.exMinW columns at the left, live: 20, then 30. At 50, it and the page's 40
// no longer fit 80, so it is released to its own place; back at 20 it tiles again. Re-docked to the
// bottom it keeps 6 rows, not 20, and the page (8 rows at least, so a 24-row Window holds it) the
// rest. The Drawer is the same node throughout.
func TestAMembersMinimumOnEachAxisIsLiveAndTheCentresBothHold(t *testing.T) {
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(minAxesDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.exEdge": "left", "App.exMinW": 20}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	node, _ := s.Program.Find("ex")
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
	set(t, s, "App.exMinW", 30)
	titleAt(t, s, "PAGE", 32, 0)
	set(t, s, "App.exMinW", 50)
	titleAt(t, s, "EXPLORER", 58, 0)
	titleAt(t, s, "PAGE", 2, 0)
	set(t, s, "App.exMinW", 20)
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
	set(t, s, "App.exEdge", "bottom")
	titleAt(t, s, "EXPLORER", 2, 18)
	titleAt(t, s, "PAGE", 2, 0)
	if now, _ := s.Program.Find("ex"); now != node {
		t.Fatal("a change of TileManager.minimumWidth or .edge rebuilt the Drawer")
	}
}
