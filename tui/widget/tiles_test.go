package widget_test

import (
	"math"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tilesFix is a manager of three tiles, AutoDB's shape: an explorer down the left, the query in the
// centre, the results along the bottom beside the explorer. Each tile holds a Button, so focus has
// somewhere to be.
type tilesFix struct {
	t       *testing.T
	h       *harness
	m       *widget.Tiles
	dock    *widget.MinimizedWindows
	buttons map[string]*widget.Button
	events  []widget.TilesChangedEvent // appended on the loop; read through onLoop
}

type tileSpec struct {
	key   string
	place widget.TilePlace
}

func defaultTiles() []tileSpec {
	return []tileSpec{
		{"explorer", widget.TilePlace{Edge: tui.DockLeft, Edges: []tui.DockEdge{tui.DockLeft, tui.DockRight}, Size: 25, Min: 10, Minimizable: true}},
		{"query", widget.TilePlace{Edge: tui.DockCenter, Min: 10}},
		{"results", widget.TilePlace{Edge: tui.DockBottom, Edges: []tui.DockEdge{tui.DockBottom, tui.DockTop}, Size: 40, Min: 5, Minimizable: true}},
	}
}

func newTiles(t *testing.T, w, h int, specs []tileSpec) *tilesFix {
	t.Helper()
	f := &tilesFix{t: t, dock: widget.NewMinimizedWindows(), buttons: map[string]*widget.Button{}}
	f.m = widget.NewTiles(widget.WithTilesCollector(f.dock),
		widget.WithTilesChanged(func(e widget.TilesChangedEvent) { f.events = append(f.events, e) }))
	for _, s := range specs {
		b := widget.NewButton(s.key)
		f.buttons[s.key] = b
		f.m.Add(s.key, b, s.place)
	}
	f.h = startApp(t, f.m, w, h)
	f.h.settle()
	return f
}

// rect is where the last layout placed a tile.
func (f *tilesFix) rect(key string) tui.Rect {
	var r tui.Rect
	f.h.onLoop(func() { r = f.m.Tile(key).Window().Core().Bounds() })
	return r
}

func (f *tilesFix) wantRects(when string, want map[string]tui.Rect) {
	f.t.Helper()
	for key, r := range want {
		if got := f.rect(key); got != r {
			f.t.Errorf("%s: %s at %+v, want %+v", when, key, got, r)
		}
	}
}

func (f *tilesFix) changes() []widget.TilesChangedEvent {
	var out []widget.TilesChangedEvent
	f.h.onLoop(func() { out = append(out, f.events...) })
	return out
}

func (f *tilesFix) size(key string) float64 {
	var s float64
	f.h.onLoop(func() { s = f.m.Tile(key).Place().Size })
	return s
}

// drag is a whole pointer gesture: an Alt press, one motion, the release.
func (f *tilesFix) drag(button tui.MouseButton, x0, y0, x1, y1 int) {
	f.press(button, x0, y0)
	f.motion(button, x1, y1)
	f.h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: button, X: x1, Y: y1})
	f.h.barrier(nil)
}

func (f *tilesFix) press(button tui.MouseButton, x, y int) {
	f.h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: button, Mods: tui.ModAlt, X: x, Y: y})
	f.h.barrier(nil)
}

func (f *tilesFix) motion(button tui.MouseButton, x, y int) {
	f.h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: button, X: x, Y: y})
	f.h.barrier(nil)
}

var homeRects = map[string]tui.Rect{
	"explorer": {X: 0, Y: 0, W: 30, H: 30},
	"query":    {X: 30, Y: 0, W: 90, H: 18},
	"results":  {X: 30, Y: 18, W: 90, H: 12},
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Strips take their depth in declaration order, each its percent of the manager across its edge,
// and the centre takes the rest: the explorer the whole height, the results the width beside it.
func TestTilesShareTheRectStripsInOrderCentreTheRest(t *testing.T) {
	f := newTiles(t, 120, 30, defaultTiles())
	f.wantRects("laid out", homeRects)
}

// When the minimums don't fit, the strips give back their cells above Min (last-declared first),
// then the centre its Min, then the strips their Min; the requests are kept, so a wider manager
// gets the asked sizes back.
func TestTilesInfeasibleMinimumsGiveWayInOrder(t *testing.T) {
	t.Run("strips above Min, last-declared first", func(t *testing.T) {
		f := newTiles(t, 100, 10, []tileSpec{
			{"explorer", widget.TilePlace{Edge: tui.DockLeft, Size: 60, Min: 20}},
			{"query", widget.TilePlace{Edge: tui.DockCenter, Min: 10}},
			{"inspector", widget.TilePlace{Edge: tui.DockRight, Size: 50, Min: 10}},
		})
		f.wantRects("100 wide", map[string]tui.Rect{
			"explorer": {X: 0, Y: 0, W: 60, H: 10}, "inspector": {X: 70, Y: 0, W: 30, H: 10}, "query": {X: 60, Y: 0, W: 10, H: 10}})
	})
	t.Run("the centre's Min next", func(t *testing.T) {
		specs := defaultTiles()[:2]
		specs[0].place.Min, specs[1].place.Min = 20, 15
		f := newTiles(t, 30, 10, specs)
		f.wantRects("30 wide", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 20, H: 10}, "query": {X: 20, Y: 0, W: 10, H: 10}})
	})
	t.Run("the strips' Min last, and the request survives", func(t *testing.T) {
		specs := defaultTiles()[:2]
		specs[0].place.Min, specs[1].place.Min = 20, 15
		f := newTiles(t, 12, 10, specs)
		f.wantRects("12 wide", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 12, H: 10}, "query": {X: 12, Y: 0, W: 0, H: 10}})
		if s := f.size("explorer"); s != 25 {
			t.Fatalf("the clamp rewrote the request: %v", s)
		}
		f.h.tb.InjectResize(120, 30)
		f.h.settle()
		f.wantRects("widened", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 30, H: 30}, "query": {X: 30, Y: 0, W: 90, H: 30}})
	})
}

// Hiding goes through the tile's window whatever the entry point: its strip goes to the tiles
// beside it, the window reports Minimized, the collector lists it, and focus leaves it.
func TestTilesHideGoesThroughTheWindow(t *testing.T) {
	f := newTiles(t, 120, 30, defaultTiles())
	explorer := func() *widget.WindowMod { return f.m.Tile("explorer").Window() }
	hiddenRects := map[string]tui.Rect{"query": {X: 0, Y: 0, W: 120, H: 18}, "results": {X: 0, Y: 18, W: 120, H: 12}}
	check := func(when string, hidden bool) {
		t.Helper()
		var minimized, focusIn bool
		var entries int
		f.h.onLoop(func() {
			minimized = explorer().Core().State().Minimized
			entries = len(f.dock.Entries())
			focusIn = f.h.app.FocusWithin(explorer())
		})
		if minimized != hidden || (entries == 1) != hidden || (hidden && focusIn) {
			t.Fatalf("%s: minimized=%v entries=%d focus in explorer=%v, want hidden=%v", when, minimized, entries, focusIn, hidden)
		}
		if hidden {
			f.wantRects(when, hiddenRects)
		} else {
			f.wantRects(when, homeRects)
		}
	}
	var ok bool
	f.h.onLoop(func() { f.h.app.FocusInto(f.buttons["explorer"]); ok = f.m.Hide("explorer") })
	f.h.settle()
	if !ok {
		t.Fatal("Hide refused")
	}
	check("Tiles.Hide", true)
	f.h.onLoop(func() { ok = f.dock.Entries()[0].Restore() })
	f.h.settle()
	check("the dock's restore", false)
	f.h.onLoop(func() { ok = explorer().Minimize() })
	f.h.settle()
	check("the window's Minimize", true)
	f.h.onLoop(func() { ok = f.m.Show("explorer") })
	f.h.settle()
	check("Tiles.Show", false)
	if ev := f.changes(); len(ev) != 4 || !ev[0].Hidden || ev[1].Hidden || ev[0].Key != "explorer" {
		t.Fatalf("events: %+v", ev)
	}
}

// The centre never hides, minimizable or not, and never changes edge but by a swap.
func TestTilesTheCentreNeverHidesNorMovesButBySwap(t *testing.T) {
	specs := defaultTiles()
	specs[1].place.Minimizable = true
	f := newTiles(t, 120, 30, specs)
	var hid, minimized, centre, toCentre, offEdges, redock bool
	f.h.onLoop(func() {
		hid = f.m.Hide("query")
		minimized = f.m.Tile("query").Window().Core().State().Minimized
		centre = f.m.SetEdge("query", tui.DockLeft)
		toCentre = f.m.SetEdge("explorer", tui.DockCenter)
		offEdges = f.m.SetEdge("explorer", tui.DockTop)
	})
	if hid || minimized || centre || toCentre || offEdges {
		t.Fatalf("hid=%v minimized=%v centre moved=%v to centre=%v off its edges=%v", hid, minimized, centre, toCentre, offEdges)
	}
	f.wantRects("refused", homeRects)
	f.h.onLoop(func() { redock = f.m.SetEdge("explorer", tui.DockRight) })
	f.h.settle()
	if !redock {
		t.Fatal("an allowed re-dock was refused")
	}
	f.wantRects("re-docked right", map[string]tui.Rect{"explorer": {X: 90, Y: 0, W: 30, H: 30}, "query": {X: 0, Y: 0, W: 90, H: 18}})
}

// Maximize is layout only: the tile fills the manager, nothing is minimized or collected, and
// restore gives every tile back its exact rect.
func TestTilesMaximizeIsLayoutOnly(t *testing.T) {
	f := newTiles(t, 120, 30, defaultTiles())
	var ok bool
	f.h.onLoop(func() { ok = f.m.Maximize("results") })
	f.h.settle()
	if !ok {
		t.Fatal("Maximize refused")
	}
	f.wantRects("maximized", map[string]tui.Rect{"results": {X: 0, Y: 0, W: 120, H: 30}})
	var entries int
	var minimized bool
	f.h.onLoop(func() {
		entries = len(f.dock.Entries())
		minimized = f.m.Tile("explorer").Window().Core().State().Minimized
	})
	if entries != 0 || minimized {
		t.Fatalf("maximize minimized: entries=%d explorer minimized=%v", entries, minimized)
	}
	f.h.onLoop(func() { ok = f.m.Restore() })
	f.h.settle()
	f.wantRects("restored", homeRects)
}

// Alt-right drag in any tile moves the shared border nearest the press, and the strip that owns it
// is resized and reported, whichever tile the drag began in.
func TestTilesAltRightMovesTheNearestSharedBorder(t *testing.T) {
	for _, c := range []struct {
		name           string
		x0, y0, x1, y1 int
		owner, by      string
		size           float64
		want           map[string]tui.Rect
	}{
		{"the explorer's own inner side", 25, 10, 35, 10, "explorer", "explorer", 100 * 40.0 / 120,
			map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 40, H: 30}, "query": {X: 40, Y: 0, W: 80, H: 18}}},
		{"the query's left side: the explorer's", 35, 5, 25, 5, "explorer", "query", 100 * 20.0 / 120,
			map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 20, H: 30}, "query": {X: 20, Y: 0, W: 100, H: 18}}},
		{"the query's bottom side: the results'", 80, 16, 80, 12, "results", "query", 100 * 16.0 / 30,
			map[string]tui.Rect{"query": {X: 30, Y: 0, W: 90, H: 14}, "results": {X: 30, Y: 14, W: 90, H: 16}}},
		{"the results' left side: the explorer's", 33, 25, 38, 25, "explorer", "results", 100 * 35.0 / 120,
			map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 35, H: 30}, "results": {X: 35, Y: 18, W: 85, H: 12}}},
		{"the results' own top side", 80, 19, 80, 16, "results", "results", 100 * 15.0 / 30,
			map[string]tui.Rect{"results": {X: 30, Y: 15, W: 90, H: 15}, "query": {X: 30, Y: 0, W: 90, H: 15}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newTiles(t, 120, 30, defaultTiles())
			f.drag(tui.MouseRight, c.x0, c.y0, c.x1, c.y1)
			f.wantRects("after the drag", c.want)
			ev := f.changes()
			if len(ev) != 1 || ev[0].Key != c.owner || ev[0].By != c.by || ev[0].Operation != widget.WindowResize || !near(ev[0].Size, c.size) {
				t.Fatalf("events: %+v, want one resize of %s by %s to %v", ev, c.owner, c.by, c.size)
			}
			if s := f.size(c.owner); !near(s, c.size) {
				t.Fatalf("%s's request = %v, want %v", c.owner, s, c.size)
			}
		})
	}
}

// Two strips on one edge: the centre's left border is the inner one's, measured from where its
// own offer starts, not from the manager's side.
func TestTilesTwoStripsOnOneEdgeMeasureFromTheirOuterSide(t *testing.T) {
	f := newTiles(t, 100, 20, []tileSpec{
		{"a", widget.TilePlace{Edge: tui.DockLeft, Size: 20}},
		{"b", widget.TilePlace{Edge: tui.DockLeft, Size: 30}},
		{"centre", widget.TilePlace{Edge: tui.DockCenter}},
	})
	f.drag(tui.MouseRight, 52, 10, 62, 10)
	f.wantRects("after the drag", map[string]tui.Rect{
		"a": {X: 0, Y: 0, W: 20, H: 20}, "b": {X: 20, Y: 0, W: 40, H: 20}, "centre": {X: 60, Y: 0, W: 40, H: 20}})
	if s := f.size("b"); !near(s, 40) {
		t.Fatalf("b's request = %v, want 40", s)
	}
}

// Escape during a drag restores the layout and reports nothing; a release where the drag began
// reports nothing either; a lone tile has no border to move.
func TestTilesEscapeAndANoOpReleaseChangeNothing(t *testing.T) {
	f := newTiles(t, 120, 30, defaultTiles())
	f.press(tui.MouseRight, 25, 10)
	f.motion(tui.MouseRight, 40, 10)
	f.wantRects("mid-drag, live", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 45, H: 30}})
	f.h.inject(key(tui.KeyEscape))
	f.h.barrier(nil)
	f.wantRects("after Escape", homeRects)
	f.drag(tui.MouseRight, 25, 10, 25, 10)
	if ev := f.changes(); len(ev) != 0 {
		t.Fatalf("Escape or a no-op release reported: %+v", ev)
	}

	lone := newTiles(t, 40, 10, []tileSpec{{"only", widget.TilePlace{Edge: tui.DockCenter}}})
	lone.press(tui.MouseRight, 5, 5)
	var captured bool
	lone.h.onLoop(func() { captured = lone.m.Tile("only").Window().Context().HasPointerCapture() })
	if captured {
		t.Fatal("a lone tile took a resize with no border to move")
	}
}

// A lost capture keeps a resize's reached border and reports it once, and drops a move's staged
// drop: no re-dock the user never released on.
func TestTilesCaptureLossKeepsAResizeAndDropsAMove(t *testing.T) {
	f := newTiles(t, 120, 30, defaultTiles())
	f.press(tui.MouseRight, 25, 10)
	f.motion(tui.MouseRight, 35, 10)
	f.h.onLoop(func() { f.h.app.FocusInto(f.buttons["query"]) })
	f.h.settle()
	f.wantRects("resize, capture lost", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 40, H: 30}})
	if ev := f.changes(); len(ev) != 1 || ev[0].Key != "explorer" || !near(ev[0].Size, 100*40.0/120) {
		t.Fatalf("events after the lost resize: %+v", ev)
	}

	g := newTiles(t, 120, 30, defaultTiles())
	g.press(tui.MouseLeft, 10, 10)
	g.motion(tui.MouseLeft, 115, 10)
	g.h.onLoop(func() { g.h.app.FocusInto(g.buttons["query"]) })
	g.h.settle()
	g.wantRects("move, capture lost", homeRects)
	if ev := g.changes(); len(ev) != 0 {
		t.Fatalf("a lost move reported: %+v", ev)
	}
	if row := g.h.row(0); []rune(row)[90] == '┌' {
		t.Fatalf("the drop preview outlived the gesture: %q", row)
	}
}

// A keyboard step moves the tile's shared border: a strip its own, the centre the first-declared
// owner's. Steps add up before a layout, and 40 one-cell steps at 400 cells are 10%.
func TestTilesKeyboardStepsMoveTheSharedBorder(t *testing.T) {
	f := newTiles(t, 400, 20, defaultTiles()[:2])
	f.h.onLoop(func() {
		w := f.m.Tile("explorer").Window()
		w.ResizeBy(1, 0)
		w.ResizeBy(1, 0) // before a layout: builds on the first
	})
	f.h.settle()
	if got := f.rect("explorer"); got.W != 102 {
		t.Fatalf("two steps before a layout: %d cells, want 102", got.W)
	}
	f.h.onLoop(func() {
		for range 38 {
			f.m.Tile("explorer").Window().ResizeBy(1, 0)
		}
	})
	f.h.settle()
	if s := f.size("explorer"); !near(s, 35) || f.rect("explorer").W != 140 {
		t.Fatalf("40 steps: request %v, %d cells; want 35, 140", s, f.rect("explorer").W)
	}
	f.h.onLoop(func() { f.m.Tile("query").Window().ResizeBy(1, 0) })
	f.h.settle()
	if got := f.rect("explorer"); got.W != 139 {
		t.Fatalf("the centre's step moved the explorer's border to %d, want 139", got.W)
	}
	var refused bool
	lone := newTiles(t, 40, 10, []tileSpec{{"only", widget.TilePlace{Edge: tui.DockCenter}}})
	lone.h.onLoop(func() { refused = !lone.m.Tile("only").Window().ResizeBy(1, 0) })
	if !refused {
		t.Fatal("a lone tile took a keyboard step")
	}
}

// Alt-left drag near a side the tile may take re-docks it there, the landing rect outlined until
// the release; a side it may not take is no drop.
func TestTilesAltLeftRedocksWithAPreview(t *testing.T) {
	f := newTiles(t, 120, 30, defaultTiles())
	f.press(tui.MouseLeft, 10, 10)
	f.motion(tui.MouseLeft, 115, 10)
	if row := []rune(f.h.row(0)); row[90] != '┌' || row[119] != '┐' {
		t.Fatalf("no preview at the landing rect: %q", string(row))
	}
	f.wantRects("staged", homeRects)
	f.h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 115, Y: 10})
	f.h.barrier(nil)
	f.wantRects("re-docked", map[string]tui.Rect{
		"explorer": {X: 90, Y: 0, W: 30, H: 30}, "query": {X: 0, Y: 0, W: 90, H: 18}, "results": {X: 0, Y: 18, W: 90, H: 12}})
	if ev := f.changes(); len(ev) != 1 || ev[0].Key != "explorer" || ev[0].Edge != tui.DockRight || ev[0].Operation != widget.WindowMove {
		t.Fatalf("events: %+v", ev)
	}

	g := newTiles(t, 120, 30, defaultTiles())
	g.drag(tui.MouseLeft, 10, 10, 60, 2) // the top band: not one of the explorer's edges, and no swap
	g.wantRects("no drop", homeRects)
	g.press(tui.MouseLeft, 10, 10)
	g.motion(tui.MouseLeft, 115, 10)
	g.h.inject(key(tui.KeyEscape))
	g.h.barrier(nil)
	g.wantRects("Escape", homeRects)
	if ev := g.changes(); len(ev) != 0 {
		t.Fatalf("no drop or Escape reported: %+v", ev)
	}
}

// A drop on a tile the dragged one may trade places with swaps them (edge, size and order), even
// inside an edge band; a swap is the one way the centre role moves.
func TestTilesADropOnACompatibleTileSwaps(t *testing.T) {
	specs := defaultTiles()
	specs[0].place.Edges = append(specs[0].place.Edges, tui.DockCenter)
	specs[1].place.Edges = []tui.DockEdge{tui.DockLeft}
	f := newTiles(t, 120, 30, specs)
	f.drag(tui.MouseLeft, 10, 10, 35, 5) // over the query, inside the left band
	f.wantRects("swapped", map[string]tui.Rect{
		"query": {X: 0, Y: 0, W: 30, H: 30}, "explorer": {X: 30, Y: 0, W: 90, H: 18}, "results": {X: 30, Y: 18, W: 90, H: 12}})
	ev := f.changes()
	if len(ev) != 2 || ev[0].Key != "explorer" || ev[0].Edge != tui.DockCenter || ev[1].Key != "query" || ev[1].Edge != tui.DockLeft {
		t.Fatalf("events: %+v", ev)
	}
}

// A re-dock that lands in the rect it began in still commits: the manager is too small for the
// explorer's Min, so it fills the width at either edge, and only a wider manager shows the change.
func TestTilesAnUndersizedRedockStillCommits(t *testing.T) {
	specs := defaultTiles()
	specs[0].place.Min = 20
	f := newTiles(t, 10, 10, specs)
	f.wantRects("undersized", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 10, H: 10}})
	f.drag(tui.MouseLeft, 2, 2, 9, 2)
	f.wantRects("re-docked, the same rect", map[string]tui.Rect{"explorer": {X: 0, Y: 0, W: 10, H: 10}})
	if ev := f.changes(); len(ev) != 1 || ev[0].Edge != tui.DockRight {
		t.Fatalf("the same-rect re-dock was not committed: %+v", ev)
	}
	f.h.tb.InjectResize(120, 30)
	f.h.settle()
	f.wantRects("widened", map[string]tui.Rect{"explorer": {X: 90, Y: 0, W: 30, H: 30}})
}
