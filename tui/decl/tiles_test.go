package decl_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tiles_test.go: a TileManager of Tiles, as a document writes it: laid out by the manager, its
// signals raised on the tile that owns the change, its methods through the tile's window.

// calls records each handler call as one line: its arguments' raw values, apart by spaces.
type calls struct {
	mu  sync.Mutex
	got []string
}

func (c *calls) handler(args []qml.SpecValue) error {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.Raw
	}
	c.mu.Lock()
	c.got = append(c.got, strings.Join(parts, " "))
	c.mu.Unlock()
	return nil
}

func (c *calls) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got...)
}

const tilesDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager {
  Tile { id: ex; edge: Tui.Left; edges: "left right"; size: App.exSize; minimumSize: 5; minimizable: true
         onResized: App.log("ex resized", size); onDocked: App.log("ex docked", edge)
         onChanged: App.log("ex changed", operation, maximized, hidden)
         Frame { title: "EXPLORER"; Text { text: "e" } } }
  Tile { id: q; edge: Tui.Center; onResized: App.log("q resized", size)
         Frame { title: "QUERY"; Text { text: "q" } } }
  Tile { id: r; edge: Tui.Bottom; edges: "bottom top"; size: 40; onResized: App.log("r resized", size)
         Frame { title: "RESULTS"; Text { text: "r" } } }
 }
}`

func runTiles(t *testing.T) (*decltest.Screen, *calls) {
	t.Helper()
	rec := &calls{}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(tilesDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.exSize": 25}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "RESULTS")
	return s, rec
}

// titleAt waits for the screen to show title at (x, y): its frame's corner two cells before it.
func titleAt(t *testing.T, s *decltest.Screen, title string, x, y int) {
	t.Helper()
	s.WaitFor(t, title+" placed", func(sc string) bool {
		gx, gy, ok := at(sc, title)
		return ok && gx == x && gy == y
	})
}

// The manager lays its Tiles out from their bound sizes: the explorer's 25% of 80 down the left,
// the results' 40% of 24 along the bottom beside it, the query the rest.
func TestATileManagerLaysOutItsTiles(t *testing.T) {
	s, _ := runTiles(t)
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "QUERY", 22, 0)
	titleAt(t, s, "RESULTS", 22, 14)
}

// A border dragged from inside the query is the explorer's: the explorer raises resized with its
// new size, and the query, which began the drag, raises nothing.
func TestABorderDraggedFromTheQueryResizesTheExplorer(t *testing.T) {
	s, rec := runTiles(t)
	titleAt(t, s, "QUERY", 22, 0)
	s.Keys(t,
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 22, Y: 5},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 26, Y: 5},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 26, Y: 5})
	titleAt(t, s, "QUERY", 26, 0)
	s.WaitFor(t, "the explorer's resized", func(string) bool { return len(rec.all()) >= 2 })
	got := rec.all()
	if got[0] != "ex resized 30" || !strings.HasPrefix(got[1], "ex changed resize") {
		t.Fatalf("signals: %q", got)
	}
}

// hide and show go through the tile's window: changed says hidden, the dock's window reports it
// minimized, and the query and results take the explorer's strip.
func TestATilesHideIsItsWindowsMinimize(t *testing.T) {
	s, rec := runTiles(t)
	titleAt(t, s, "QUERY", 22, 0)
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("ex", "hide"); err != nil {
			t.Error(err)
		}
	})
	titleAt(t, s, "QUERY", 2, 0)
	titleAt(t, s, "RESULTS", 2, 14)
	var minimized bool
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find("ex")
		minimized = c.(interface{ WindowBehavior() *widget.WindowCore }).WindowBehavior().State().Minimized
	})
	if !minimized {
		t.Fatal("the explorer's window is not minimized")
	}
	if got := rec.all(); len(got) != 1 || got[0] != "ex changed minimize false true" {
		t.Fatalf("signals: %q", got)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("ex", "show"); err != nil {
			t.Error(err)
		}
	})
	titleAt(t, s, "QUERY", 22, 0)
}

// A tile dragged to the far side of the manager re-docks there and raises docked with its edge.
func TestATileDraggedAcrossRedocks(t *testing.T) {
	s, rec := runTiles(t)
	titleAt(t, s, "EXPLORER", 2, 0)
	s.Keys(t,
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 5},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 77, Y: 5},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 77, Y: 5})
	titleAt(t, s, "EXPLORER", 62, 0)
	titleAt(t, s, "QUERY", 2, 0)
	s.WaitFor(t, "docked", func(string) bool { return len(rec.all()) >= 1 })
	if got := rec.all(); got[0] != "ex docked right" {
		t.Fatalf("signals: %q", got)
	}
}

// A TileManager has Tiles only, exactly one of them at the centre, and a Tile's edges are edge
// names.
func TestATileManagerRefusesAWrongShape(t *testing.T) {
	for name, body := range map[string]string{
		"no centre":   `Tile { edge: Tui.Left; Text { text: "a" } } Tile { edge: Tui.Right; Text { text: "b" } }`,
		"two centres": `Tile { edge: Tui.Center; Text { text: "a" } } Tile { edge: Tui.Center; Text { text: "b" } }`,
		"not a Tile":  `Tile { edge: Tui.Center; Text { text: "a" } } Text { text: "b" }`,
		"bad edges":   `Tile { edge: Tui.Center; Text { text: "a" } } Tile { edge: Tui.Left; edges: "left sideways"; Text { text: "b" } }`,
	} {
		src := "import tui 1.0\nWindow { TileManager { " + body + " } }"
		if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src))); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}
