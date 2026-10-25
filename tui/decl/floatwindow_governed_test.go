package decl_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// floatwindow_governed_test.go: a FloatWindow declared in a TileManager, as a document writes it
// (tiles_member_test.go does the same for a Drawer). Its TileManager.* attached properties say where
// its tile is; its own percentages, (50%, 20%) 40% by 50%, are where it floats when released: at
// (40,5) 32 by 12 on the 80 by 24 screen, its title at (42,5).

const floatMemberDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager {
  FloatWindow { id: fw; movable: true; resizable: true; minimizable: true
    floatX: 50; floatY: 20; floatWidth: 40; floatHeight: 50
    TileManager.edge: Tui.Left; TileManager.size: 25; TileManager.minimumSize: 5
    TileManager.tiled: App.fwTiled
    onPlaced: App.log("placed", x, y, width, height)
    Frame { title: "FLOAT"; Text { id: inner; text: "f" } } }
  Tile { id: pg; edge: Tui.Center; Frame { title: "PAGE"; Text { text: "p" } } }
  onTileChanged: App.log("tile", key, operation, edge, size, tiled)
 }
}`

func runFloatMember(t *testing.T) (*decltest.Screen, *calls) {
	t.Helper()
	rec := &calls{}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(floatMemberDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.fwTiled": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	return s, rec
}

// innerFloat is the Float the FloatWindow's content is in, read on the loop by fn.
func innerFloat(t *testing.T, s *decltest.Screen, fn func(f *widget.Float)) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find("inner")
		in, ok := c.(interface{ Context() *tui.Context })
		if !ok {
			t.Errorf("inner (%T) has no context", c)
			return
		}
		if f, ok := in.Context().Ancestor(func(v tui.Component) bool { _, ok := v.(*widget.Float); return ok }).(*widget.Float); ok {
			fn(f)
		}
	})
}

// innerFloating reports whether the Float the FloatWindow's content is in reads floating (the GUI's
// shadow follows it).
func innerFloating(t *testing.T, s *decltest.Screen) (on bool) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find("inner")
		in, ok := c.(interface{ Context() *tui.Context })
		if !ok {
			t.Errorf("inner (%T) has no context", c)
			return
		}
		f, ok := in.Context().Ancestor(func(v tui.Component) bool { _, ok := v.(*widget.Float); return ok }).(*widget.Float)
		on = ok && f.Floating()
	})
	return on
}

// A FloatWindow opens with its Window, so a member wanting to be tiled is in its tile from the first
// frame, with nothing called: a member never opened would be away (r2.8).
func TestAFloatWindowMemberIsTiledFromTheFirstFrame(t *testing.T) {
	s, _ := runFloatMember(t)
	titleAt(t, s, "FLOAT", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
}

// A FloatWindow that wants to be tiled but doesn't fit at start-up (its tile's minimum, 70, and the
// page's, 20, are more than the Window's 80) is never governed, not even for a frame: its core is
// never retargeted (it changes once, at its Bind; a Govern and a Release would be two more), and it
// is at its own place, the page the whole manager.
func TestAWantedFloatWindowThatDoesNotFitIsNeverGoverned(t *testing.T) {
	doc := strings.Replace(floatMemberDoc, "TileManager.minimumSize: 5", "TileManager.minimumSize: 70", 1)
	doc = strings.Replace(doc, "Tile { id: pg; edge: Tui.Center;", "Tile { id: pg; edge: Tui.Center; minimumSize: 20;", 1)
	changes := 0
	s := decltest.RunWith(t, 80, 24, func(p *tuidecl.Program) error {
		c, _ := p.Find("fw")
		w, ok := c.(interface{ WindowBehavior() *widget.WindowCore })
		if !ok {
			t.Fatalf("fw (%T) has no window", c)
		}
		w.WindowBehavior().Observe(func() { changes++ }) // the loop's, read on it below
		return nil
	}, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.fwTiled": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": (&calls{}).handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	titleAt(t, s, "FLOAT", 42, 5)
	titleAt(t, s, "PAGE", 2, 0)
	onScreenLoop(t, s, func() {})
	var n int
	onScreenLoop(t, s, func() { n = changes })
	if n != 1 {
		t.Fatalf("its core changed %d times, want once (its Bind): more is a Govern and a Release, where it should never have been governed", n)
	}
}

// Governed, it is over its tile, docked (no shadow); TileManager.tiled bound false releases it to
// its own percentages, floating, its gestures its own again (a move moves it and raises placed), and
// true tiles it again. It is the same node throughout.
func TestAGovernedFloatWindowIsReleasedToItsOwnPlace(t *testing.T) {
	s, rec := runFloatMember(t)
	node, _ := s.Program.Find("fw")
	titleAt(t, s, "FLOAT", 2, 0)
	if innerFloating(t, s) {
		t.Error("governed, the FloatWindow reads floating: its fallback must be docked")
	}
	set(t, s, "App.fwTiled", false)
	titleAt(t, s, "FLOAT", 42, 5)
	titleAt(t, s, "PAGE", 2, 0)
	if !innerFloating(t, s) {
		t.Error("released, the FloatWindow does not read floating")
	}
	altDrag(t, s, tui.MouseLeft, 50, 10, 55, 12)
	titleAt(t, s, "FLOAT", 47, 7)
	s.WaitFor(t, "placed, after a move of its own", func(string) bool {
		return slices.ContainsFunc(rec.all(), func(c string) bool { return strings.HasPrefix(c, "placed") })
	})
	set(t, s, "App.fwTiled", true)
	titleAt(t, s, "FLOAT", 2, 0)
	if now, _ := s.Program.Find("fw"); now != node {
		t.Fatal("a change of TileManager.tiled rebuilt the FloatWindow")
	}
}

// A governed FloatWindow's resize is its tile's: Alt-right on the shared border widens the tile and
// the manager reports it, but its own bookkeeping doesn't run, no placed. A move is refused: a tiled
// window isn't moved on its own. Released, it is back where it floated, its percentages untouched.
func TestAGovernedFloatWindowsResizeCommitsThroughItsTileAlone(t *testing.T) {
	s, rec := runFloatMember(t)
	titleAt(t, s, "PAGE", 22, 0)
	altDrag(t, s, tui.MouseRight, 19, 10, 31, 10)
	titleAt(t, s, "PAGE", 34, 0)
	s.WaitFor(t, "tileChanged for the resize", func(string) bool {
		return slices.Contains(rec.all(), "tile fw resize left 40 true")
	})
	altDrag(t, s, tui.MouseLeft, 5, 10, 50, 14)
	onScreenLoop(t, s, func() {})
	titleAt(t, s, "FLOAT", 2, 0)
	for _, c := range rec.all() {
		if strings.HasPrefix(c, "placed") {
			t.Fatalf("a governed FloatWindow's resize ran its own bookkeeping: %q among %q", c, rec.all())
		}
	}
	set(t, s, "App.fwTiled", false)
	titleAt(t, s, "FLOAT", 42, 5)
}

// Minimized while governed, it draws nothing and gives its cells back; released so, it stays
// hidden; restored, it is at its own place.
func TestAGovernedFloatWindowMinimizedStaysHiddenUntilRestored(t *testing.T) {
	s, _ := runFloatMember(t)
	titleAt(t, s, "PAGE", 22, 0)
	call(t, s, "fw", "minimize")
	titleAt(t, s, "PAGE", 2, 0)
	if strings.Contains(s.String(), "FLOAT") {
		t.Fatalf("a minimized member is drawn:\n%s", s.String())
	}
	set(t, s, "App.fwTiled", false)
	onScreenLoop(t, s, func() {})
	if strings.Contains(s.String(), "FLOAT") {
		t.Fatalf("released while minimized, the FloatWindow is drawn:\n%s", s.String())
	}
	call(t, s, "fw", "restore")
	titleAt(t, s, "FLOAT", 42, 5)
}

// A governed FloatWindow's maximum doesn't hold its tile: the tile composition sizes it (D2). With
// maximumWidth 15 below its 20-cell tile, Alt-right on the shared border widens the tile to 40, with
// no jump to the maximum on the first step, and resizeBy widens it further. Released, its own resize
// is held to the maximum again.
func TestAGovernedFloatWindowsMaximumDoesNotHoldItsTile(t *testing.T) {
	doc := strings.Replace(floatMemberDoc, "floatHeight: 50", "floatHeight: 50; maximumWidth: 15", 1)
	rec := &calls{}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.fwTiled": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	titleAt(t, s, "PAGE", 22, 0)
	altDrag(t, s, tui.MouseRight, 19, 10, 31, 10)
	titleAt(t, s, "PAGE", 34, 0)
	s.WaitFor(t, "tileChanged for the resize", func(string) bool {
		return slices.Contains(rec.all(), "tile fw resize left 40 true")
	})
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("fw", "resizeBy", 8, 0); err != nil {
			t.Error(err)
		}
	})
	titleAt(t, s, "PAGE", 42, 0)
	set(t, s, "App.fwTiled", false)
	titleAt(t, s, "FLOAT", 50, 5) // its own 32 cells held to 15, centred where they were
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("fw", "resizeBy", 20, 0); err != nil {
			t.Error(err)
		}
	})
	onScreenLoop(t, s, func() {})
	if got := windowRect(t, s, "fw"); got.W != 15 {
		t.Fatalf("released, a resize 20 wider: %+v, want held at its maximum, 15", got)
	}
}

// Tiled while a drag of its own is in flight, it is over its tile, and the release of the cancelled
// drag leaves it there. The TileManager governs in its commit phase, where the drag's capture may
// end; governed inside its Layout, cancelling the drag would panic.
func TestAFloatWindowTiledMidDragIsOverItsTile(t *testing.T) {
	s, _ := runFloatMember(t)
	set(t, s, "App.fwTiled", false)
	titleAt(t, s, "FLOAT", 42, 5)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 50, Y: 10},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 55, Y: 12})
	titleAt(t, s, "FLOAT", 47, 7)
	set(t, s, "App.fwTiled", true)
	titleAt(t, s, "FLOAT", 2, 0)
	tile := windowRect(t, s, "fw")
	s.Keys(t, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 55, Y: 12})
	if got := windowRect(t, s, "fw"); got != tile {
		t.Fatalf("the release of the cancelled drag moved it off its tile: %+v, want %+v", got, tile)
	}
	titleAt(t, s, "FLOAT", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
}

// A member's tile can be left no cells (its minimum 0, its size shrunk live to 0): the FloatWindow
// is governed at its docked fallback, its tile's edge, vertically centred there, at its content's
// own size (too narrow for its title), and a live TileManager.edge moves it to the new edge. Its
// place is read from its Float's content rectangle.
func TestAGovernedFloatWindowWithNoCellsFollowsItsTilesEdge(t *testing.T) {
	doc := strings.Replace(floatMemberDoc, "TileManager.edge: Tui.Left; TileManager.size: 25; TileManager.minimumSize: 5",
		`TileManager.edge: App.fwEdge; TileManager.edges: "left right"; TileManager.size: App.fwSize; TileManager.minimumSize: 0`, 1)
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.fwTiled": true, "App.fwEdge": "left", "App.fwSize": 25}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": (&calls{}).handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	titleAt(t, s, "FLOAT", 2, 0)
	floatAt := func(what string, ok func(r tui.Rect) bool) {
		t.Helper()
		s.WaitFor(t, what, func(string) bool {
			var r tui.Rect
			innerFloat(t, s, func(f *widget.Float) { r = f.ContentRect() })
			return r.W > 0 && ok(r)
		})
	}
	set(t, s, "App.fwSize", 0)
	titleAt(t, s, "PAGE", 2, 0)
	floatAt("no cells: docked at its tile's edge, the left, centred", func(r tui.Rect) bool { return r.X == 0 && r.Y > 0 && r.W < 20 })
	set(t, s, "App.fwEdge", "right")
	floatAt("no cells: docked at its tile's new edge, the right", func(r tui.Rect) bool { return r.X+r.W == 80 && r.Y > 0 })
}
