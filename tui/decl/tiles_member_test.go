package decl_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tiles_member_test.go: a window declared in a TileManager, as a document writes it. The Drawer is
// a member: its TileManager.* attached properties say where its tile is, bound ones move it live,
// and its own edge (the right) and size are where it goes when released.

// memberDoc is an 80×24 Window whose TileManager governs the Drawer ex over a left tile of
// App.exSize percent, beside the page; ex's own place is a right-docked third.
const memberDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager {
  Drawer { id: ex; modal: false; windowResize: true; movable: true; minimizable: true; edge: Tui.Right; size: 30; closeOnQ: false
    TileManager.edge: App.exEdge; TileManager.edges: "left right"; TileManager.size: App.exSize; TileManager.minimumSize: 5
    TileManager.tiled: App.exTiled
    onPlaced: App.log("placed", x, y, width, height); onResized: App.log("resized", size, length)
    Frame { title: "EXPLORER"; Text { text: "e" } } }
  Tile { id: pg; edge: Tui.Center; Frame { title: "PAGE"; Text { text: "p" } } }
  onTileChanged: App.log("tile", key, operation, edge, size, tiled)
 }
}`

// memberSources are memberDoc's bound values at the start: tiled at the left, a quarter wide.
func memberSources() map[string]any {
	return map[string]any{"App.exSize": 25, "App.exTiled": true, "App.exEdge": "left"}
}

func runMember(t *testing.T, doc string) (*decltest.Screen, *calls) {
	t.Helper()
	rec := &calls{}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(memberSources()),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	return s, rec
}

// call runs a method on the loop.
func call(t *testing.T, s *decltest.Screen, id, method string) {
	t.Helper()
	onScreenLoop(t, s, func() {
		if err := s.Program.Call(id, method); err != nil {
			t.Error(err)
		}
	})
}

// set changes a bound value on the loop.
func set(t *testing.T, s *decltest.Screen, name string, v any) {
	t.Helper()
	onScreenLoop(t, s, func() {
		if err := s.Program.Set(name, v); err != nil {
			t.Error(err)
		}
	})
}

// A member never opened takes no cells: the page has the whole manager, and nothing is drawn where
// its tile would be.
func TestANeverOpenedMemberLeavesTheManagerToThePage(t *testing.T) {
	s, _ := runMember(t, memberDoc)
	titleAt(t, s, "PAGE", 2, 0)
	if strings.Contains(s.String(), "EXPLORER") {
		t.Fatalf("a Drawer never opened is drawn:\n%s", s.String())
	}
}

// Opened, the member is over its tile, a quarter of the manager down the left, and the page beside
// it. TileManager.tiled bound false releases it to its own place, a right-docked third, and the page
// takes the whole manager; true tiles it again. The Drawer is the same node throughout.
func TestAGovernedDrawerIsOverItsTileAndReleasedToItsOwnPlace(t *testing.T) {
	s, _ := runMember(t, memberDoc)
	node, _ := s.Program.Find("ex")
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
	set(t, s, "App.exTiled", false)
	titleAt(t, s, "EXPLORER", 58, 0)
	titleAt(t, s, "PAGE", 2, 0)
	set(t, s, "App.exTiled", true)
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
	if now, _ := s.Program.Find("ex"); now != node {
		t.Fatal("a change of TileManager.tiled rebuilt the Drawer")
	}
}

// Closed is hidden: a closed member gives its cells back and stays governed, and opened again it is
// in its tile.
func TestAClosedMemberGivesItsCellsBack(t *testing.T) {
	s, _ := runMember(t, memberDoc)
	call(t, s, "ex", "open")
	titleAt(t, s, "PAGE", 22, 0)
	call(t, s, "ex", "close")
	titleAt(t, s, "PAGE", 2, 0)
	if strings.Contains(s.String(), "EXPLORER") {
		t.Fatalf("a closed member is drawn:\n%s", s.String())
	}
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
}

// Bound TileManager.size and TileManager.edge move the tile live: 40% wide, then at the right.
func TestAMembersBoundTilePlaceMovesItsTile(t *testing.T) {
	s, _ := runMember(t, memberDoc)
	node, _ := s.Program.Find("ex")
	call(t, s, "ex", "open")
	titleAt(t, s, "PAGE", 22, 0)
	set(t, s, "App.exSize", 40)
	titleAt(t, s, "PAGE", 34, 0)
	set(t, s, "App.exEdge", "right")
	titleAt(t, s, "PAGE", 2, 0)
	titleAt(t, s, "EXPLORER", 50, 0)
	if now, _ := s.Program.Find("ex"); now != node {
		t.Fatal("a change of TileManager.size or .edge rebuilt the Drawer")
	}
}

// altDrag is an Alt drag with btn from (x0, y0) to (x1, y1).
func altDrag(t *testing.T, s *decltest.Screen, btn tui.MouseButton, x0, y0, x1, y1 int) {
	t.Helper()
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: btn, Mods: tui.ModAlt, X: x0, Y: y0},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: btn, Mods: tui.ModAlt, X: x1, Y: y1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: btn, Mods: tui.ModAlt, X: x1, Y: y1})
}

// A governed Drawer's resize is its tile's: Alt-right on the shared border widens the tile, and the
// manager reports it (tileChanged), but the Drawer's own bookkeeping doesn't run: no placed, no
// resized, and released it is back at its own place and size, not floating where the drag ended.
func TestAGovernedDrawersResizeCommitsThroughItsTileAlone(t *testing.T) {
	s, rec := runMember(t, memberDoc)
	call(t, s, "ex", "open")
	titleAt(t, s, "PAGE", 22, 0)
	altDrag(t, s, tui.MouseRight, 19, 10, 31, 10)
	titleAt(t, s, "PAGE", 34, 0)
	s.WaitFor(t, "tileChanged for the resize", func(string) bool {
		return slices.Contains(rec.all(), "tile ex resize left 40 true")
	})
	for _, c := range rec.all() {
		if strings.HasPrefix(c, "placed") || strings.HasPrefix(c, "resized") {
			t.Fatalf("a governed Drawer's resize ran its own bookkeeping: %q among %q", c, rec.all())
		}
	}
	set(t, s, "App.exTiled", false)
	titleAt(t, s, "EXPLORER", 58, 0)
}

// A resizable Drawer's new edge builds it a new window; governed, that window's core takes its
// tile's target too, so a resize still commits through the tile.
func TestADrawersNewEdgeWhileGovernedStillCommitsThroughItsTile(t *testing.T) {
	doc := strings.Replace(memberDoc, "edge: Tui.Right; size: 30", "edge: App.ownEdge; size: 30", 1)
	rec := &calls{}
	src := memberSources()
	src["App.ownEdge"] = "right"
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(src),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	call(t, s, "ex", "open")
	titleAt(t, s, "PAGE", 22, 0)
	set(t, s, "App.ownEdge", "bottom")
	titleAt(t, s, "EXPLORER", 2, 0)
	altDrag(t, s, tui.MouseRight, 19, 10, 31, 10)
	titleAt(t, s, "PAGE", 34, 0)
	s.WaitFor(t, "tileChanged for the resize", func(string) bool {
		return slices.Contains(rec.all(), "tile ex resize left 40 true")
	})
}

// A reload that rebuilds a member (a constructor property of its own changed) rebuilds its
// TileManager with it, which takes the member's live TileManager.* properties again: a change of
// TileManager.tiled still tiles and releases it.
func TestAMembersLivePropertiesHoldAcrossAReloadThatRebuildsIt(t *testing.T) {
	s, _ := runMember(t, memberDoc)
	before, _ := s.Program.Find("ex")
	reloadScreen(t, s, strings.Replace(memberDoc, "closeOnQ: false", "closeOnQ: true", 1))
	after, _ := s.Program.Find("ex")
	if after == before {
		t.Fatal("the reload kept the Drawer: it exercises no rebuild")
	}
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
	set(t, s, "App.exTiled", false)
	titleAt(t, s, "EXPLORER", 58, 0)
	set(t, s, "App.exTiled", true)
	titleAt(t, s, "EXPLORER", 2, 0)
}

// A TileManager refuses a member it cannot govern, each for its own reason: a Drawer without window
// controls (it has no window), a reserving Drawer, one sized by a grip, two at the centre among its
// Tiles and windows, and a Tile carrying a window's TileManager.* place.
func TestATileManagerRefusesAMemberItCannotGovern(t *testing.T) {
	for name, c := range map[string]struct{ body, why string }{
		"no window":  {`Drawer { Frame { title: "d"; Text { text: "d" } } } Tile { edge: Tui.Center; Text { text: "p" } }`, "has no window"},
		"reserve":    {`Drawer { windowResize: true; reserve: true; Frame { title: "d"; Text { text: "d" } } } Tile { edge: Tui.Center; Text { text: "p" } }`, "no reserve"},
		"grip":       {`Drawer { movable: true; resizable: true; Frame { title: "d"; Text { text: "d" } } } Tile { edge: Tui.Center; Text { text: "p" } }`, "resizable's grip"},
		"two centre": {`Drawer { windowResize: true; TileManager.edge: Tui.Center; Frame { title: "d"; Text { text: "d" } } } Tile { edge: Tui.Center; Text { text: "p" } }`, "at most one child at Tui.Center"},
		"tile place": {`Tile { edge: Tui.Left; TileManager.size: 30; Text { text: "l" } } Tile { edge: Tui.Center; Text { text: "p" } }`, "is a window's"},
	} {
		src := "import tui 1.0\nWindow { TileManager { " + c.body + " } }"
		err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src)))
		if err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v, want refused because it %q", name, err, c.why)
		}
	}
}

// A live attached property changed under a parent that registered nowhere for it is refused,
// naming the property and the parent, rather than rebuilding the child or dropping the change.
func TestALiveAttachedChangeUnderAParentThatTakesNoneIsRefused(t *testing.T) {
	shelf := tuidecl.Type{Name: "Shelf",
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			f := tui.NewFlex(tui.Vertical)
			for _, c := range b.Children {
				f.Add(c)
			}
			return f, nil, nil
		}}
	reg := tuidecl.StdRegistry()
	tuidecl.RegisterAttached(reg, "Slot", "side")
	tuidecl.LiveAttached(reg, "Slot", "side")
	tuidecl.Honour(reg, "Shelf", "Slot")
	a := tuidecl.New(reg, append(tuidecl.StdProperties(), tuidecl.WithTypes(shelf), tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	parse := func(src string) qml.SpecTree {
		spec, err := qml.New(qml.WithName("main.qml")).Parse([]byte("import tui 1.0\n" + src))
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}
	if err := tr.Mount(parse(`Shelf { Text { id: a; Slot.side: "left"; text: "a" } }`)); err != nil {
		t.Fatal(err)
	}
	before, _ := tr.NodeByID("a")
	_, err := tr.Reconcile(parse(`Shelf { Text { id: a; Slot.side: "right"; text: "a" } }`))
	if err == nil || !strings.Contains(err.Error(), "Slot.side") || !strings.Contains(err.Error(), "Shelf") {
		t.Fatalf("a live Slot.side change under a Shelf that takes none: %v, want refused naming Slot.side and Shelf", err)
	}
	if after, _ := tr.NodeByID("a"); after != before {
		t.Error("the refused change rebuilt the child")
	}
}

// A TileManager under a docked bar is placed below it, and so are its members: the Window tells the
// manager where its Dock put it, and a member's tile is offset into the overlay's frame by that.
func TestAMemberUnderADockedBarIsAtItsTileBelowTheBar(t *testing.T) {
	doc := strings.Replace(memberDoc, "Window {\n TileManager {", "Window {\n Text { Dock.edge: Tui.Top; text: \"BAR\" }\n TileManager {", 1)
	s, _ := runMember(t, doc)
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 1)
	titleAt(t, s, "PAGE", 22, 1)
}

// A fill member that is not shown is out of play: the last strip takes what is left, and with the
// member opened the strip is back at its size beside it.
func TestAnAwayFillMemberLeavesTheManagerToTheStrips(t *testing.T) {
	doc := `import tui 1.0
Window {
 TileManager {
  Tile { edge: Tui.Right; size: 25; Frame { title: "RIGHT"; Text { text: "r" } } }
  Drawer { id: ed; modal: false; windowResize: true; TileManager.edge: Tui.Center; Frame { title: "EDITOR"; Text { text: "e" } } }
 }
}`
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(doc)))
	titleAt(t, s, "RIGHT", 2, 0)
	call(t, s, "ed", "open")
	titleAt(t, s, "EDITOR", 2, 0)
	titleAt(t, s, "RIGHT", 62, 0)
}
