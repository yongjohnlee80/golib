package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// screenrect_test.go: ScreenRect, where a window is drawn, in the Window's frame. Under a bar docked
// at the top, a tiled window is a row lower on the screen than its tile is in the manager's frame,
// and a floating one is where its Float draws it: one frame for both.

// underBar puts a one-row bar docked at the top of doc's Window, over its TileManager.
func underBar(doc string) string {
	return strings.Replace(doc, "Window {\n TileManager {", "Window {\n Text { Dock.edge: Tui.Top; text: \"BAR\" }\n TileManager {", 1)
}

// screenRect asks the node Program.Find returns for id, as a Go program does, where it is drawn.
func screenRect(t *testing.T, s *decltest.Screen, id string) (r tui.Rect, on bool) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		w, ok := c.(interface{ ScreenRect() (tui.Rect, bool) })
		if !ok {
			t.Fatalf("%s's node %T answers no ScreenRect", id, c)
		}
		r, on = w.ScreenRect()
	})
	return r, on
}

// coreBounds is id's window core's Bounds: its target's own frame.
func coreBounds(t *testing.T, s *decltest.Screen, id string) (r tui.Rect) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		r = c.(interface{ WindowBehavior() *widget.WindowCore }).WindowBehavior().Bounds()
	})
	return r
}

// A FloatWindow tiled under the bar is drawn at its tile a row down, the left quarter, 20 by 23:
// ScreenRect says so where its core's Bounds (the tile's, the manager's frame) is a row higher.
// Released, it floats, and ScreenRect is where its frame is drawn. Minimized, it is not on screen.
func TestAFloatWindowsScreenRectIsWhereItIsDrawnTiledOrFloating(t *testing.T) {
	rec := &calls{}
	s := decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(underBar(floatMemberDoc))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.fwTiled": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	titleAt(t, s, "FLOAT", 2, 1)
	titleAt(t, s, "PAGE", 22, 1)
	if r, on := screenRect(t, s, "fw"); !on || r != (tui.Rect{X: 0, Y: 1, W: 20, H: 23}) {
		t.Fatalf("tiled under the bar: ScreenRect %v %v, want (0,1) 20x23", r, on)
	}
	if b := coreBounds(t, s, "fw"); b.Y != 0 {
		t.Fatalf("the tiled core's Bounds %v: want the manager's frame, a row above the screen's", b)
	}
	set(t, s, "App.fwTiled", false)
	titleAt(t, s, "PAGE", 2, 1)
	r, on := screenRect(t, s, "fw")
	if !on || r.X < 20 || r.W <= 0 || r.H <= 0 {
		t.Fatalf("floating: ScreenRect %v %v, want its own place right of the left quarter", r, on)
	}
	titleAt(t, s, "FLOAT", r.X+2, r.Y) // its frame is drawn there
	if b := coreBounds(t, s, "fw"); b != r {
		t.Fatalf("floating: the core's Bounds %v and ScreenRect %v differ; the overlay's frame is the Window's", b, r)
	}
	call(t, s, "fw", "minimize")
	s.WaitFor(t, "the window minimized", func(string) bool {
		var min bool
		onScreenLoop(t, s, func() {
			c, _ := s.Program.Find("fw")
			min = c.(interface{ WindowBehavior() *widget.WindowCore }).WindowBehavior().State().Minimized
		})
		return min
	})
	if r, on := screenRect(t, s, "fw"); on {
		t.Fatalf("minimized: ScreenRect %v true, want false", r)
	}
}

// A Drawer's node answers the same: never opened or closed, it is not on screen; opened and tiled
// under the bar, it is drawn at its tile a row down.
func TestADrawersScreenRectIsWhereItIsDrawn(t *testing.T) {
	s, _ := runMember(t, underBar(memberDoc))
	if r, on := screenRect(t, s, "ex"); on {
		t.Fatalf("never opened: ScreenRect %v true, want false", r)
	}
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 1)
	if r, on := screenRect(t, s, "ex"); !on || r != (tui.Rect{X: 0, Y: 1, W: 20, H: 23}) {
		t.Fatalf("tiled under the bar: ScreenRect %v %v, want (0,1) 20x23", r, on)
	}
	call(t, s, "ex", "close")
	titleAt(t, s, "PAGE", 2, 1)
	if r, on := screenRect(t, s, "ex"); on {
		t.Fatalf("closed: ScreenRect %v true, want false", r)
	}
}
