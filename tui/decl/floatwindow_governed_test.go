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
