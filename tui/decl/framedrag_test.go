package decl_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// framedrag_test.go: a plain drag on a window's frame (WindowCore.BeginFrameDrag), on a backend
// that declares Capabilities.FrameDrag, as gui's window does, and on one that doesn't, as a
// terminal doesn't.

// frameScreen runs a Window's body on a 40 by 10 screen whose backend declares FrameDrag or not,
// with App.log its recorder.
func frameScreen(t *testing.T, frameDrag bool, body string, rec *calls) *decltest.Screen {
	t.Helper()
	b := tui.NewTestBackend(40, 10, tui.WithTestCapabilities(tui.Capabilities{FrameDrag: frameDrag}))
	return decltest.RunOn(t, b, nil,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+body+"\n}")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
}

// plainDrag is a left drag with no modifier from (x0, y0) to (x1, y1).
func plainDrag(t *testing.T, s *decltest.Screen, x0, y0, x1, y1 int) {
	t.Helper()
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x0, Y: y0, Count: 1},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: x1, Y: y1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x1, Y: y1})
}

// windowOf is the window behind id: its bounds and state, read on the loop.
func windowOf(t *testing.T, s *decltest.Screen, id string) (r tui.Rect, st widget.WindowState) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		w, ok := c.(interface{ WindowBehavior() *widget.WindowCore })
		if !ok {
			t.Errorf("%s (%T) is no window", id, c)
			return
		}
		r, st = w.WindowBehavior().Bounds(), w.WindowBehavior().State()
	})
	return r, st
}

// floatAt10 is a movable, resizable FloatWindow at (10, 0), 10 by 5 on the 40 by 10 screen.
const floatAt10 = ` Text { text: "page" }
 FloatWindow { id: w; movable: true; resizable: true
  floatX: 25; floatY: 0; floatWidth: 25; floatHeight: 50
  Frame { title: "win"; Text { text: "in" } } }`

// On a terminal a plain press on the frame does nothing: the title row doesn't move the window and
// a border doesn't resize it. The Alt chord still moves it.
func TestATerminalsWindowTakesNoPlainFrameDrag(t *testing.T) {
	s := frameScreen(t, false, floatAt10, &calls{})
	s.WaitForText(t, "win")
	waitRect(t, s, "w", "at its percentages", tui.Rect{X: 10, W: 10, H: 5})
	plainDrag(t, s, 13, 0, 18, 2)
	plainDrag(t, s, 19, 2, 24, 2)
	plainDrag(t, s, 14, 4, 14, 7)
	if r, _ := windowOf(t, s, "w"); r != (tui.Rect{X: 10, W: 10, H: 5}) {
		t.Fatalf("a plain drag on a terminal's window moved or resized it to %+v", r)
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 13, Y: 2},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 18, Y: 4},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 18, Y: 4})
	waitRect(t, s, "w", "the Alt chord's move", tui.Rect{X: 15, Y: 2, W: 10, H: 5})
}

// On gui's backend a plain drag on the title row moves the window, its corners included.
func TestAPlainDragOnTheTitleRowMovesAWindow(t *testing.T) {
	for _, x := range []int{10, 13, 19} { // the left corner, the title, the right corner
		s := frameScreen(t, true, floatAt10, &calls{})
		s.WaitForText(t, "win")
		waitRect(t, s, "w", "at its percentages", tui.Rect{X: 10, W: 10, H: 5})
		plainDrag(t, s, x, 0, x+5, 2)
		waitRect(t, s, "w", "moved by the title row", tui.Rect{X: 15, Y: 2, W: 10, H: 5})
	}
}

// Each side, the bottom and each bottom corner resizes the window from that handle.
func TestAPlainDragOnEachBorderResizesAWindowFromIt(t *testing.T) {
	for _, c := range []struct {
		name           string
		x0, y0, x1, y1 int
		want           tui.Rect
	}{
		{"left", 10, 2, 6, 2, tui.Rect{X: 6, W: 14, H: 5}},
		{"right", 19, 2, 24, 2, tui.Rect{X: 10, W: 15, H: 5}},
		{"bottom", 14, 4, 14, 7, tui.Rect{X: 10, W: 10, H: 8}},
		{"bottom-left", 10, 4, 8, 6, tui.Rect{X: 8, W: 12, H: 7}},
		{"bottom-right", 19, 4, 21, 6, tui.Rect{X: 10, W: 12, H: 7}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := frameScreen(t, true, floatAt10, &calls{})
			s.WaitForText(t, "win")
			waitRect(t, s, "w", "at its percentages", tui.Rect{X: 10, W: 10, H: 5})
			plainDrag(t, s, c.x0, c.y0, c.x1, c.y1)
			waitRect(t, s, "w", "resized from the "+c.name, c.want)
		})
	}
}

// A press its content takes starts no drag: a Button on the window's title row is clicked, and
// dragging from it leaves the window where it is. A press inside the content beside the frame is
// the content's: a field there takes the keyboard, and the window doesn't move.
func TestTheContentKeepsThePressesItTakes(t *testing.T) {
	rec := &calls{}
	s := frameScreen(t, true, ` Text { text: "page" }
 FloatWindow { id: w; movable: true; resizable: true
  floatX: 25; floatY: 0; floatWidth: 25; floatHeight: 50
  Flex { Button { text: "go"; onClicked: App.log("go") } Frame { title: "f"; TextField { id: field } } } }`, rec)
	s.WaitForText(t, "[ go ]")
	start, _ := windowOf(t, s, "w")
	bx, by, _ := at(s.String(), "[ go ]")
	if by != start.Y || bx <= start.X || bx+6 > start.X+start.W {
		t.Fatalf("the Button at (%d, %d) is not inside the title row of the window at %+v:\n%s", bx, by, start, s)
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: bx + 2, Y: by, Count: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: bx + 2, Y: by})
	s.WaitFor(t, "the button clicked", func(string) bool { return slices.Contains(rec.all(), "go") })
	plainDrag(t, s, bx+2, by, bx+7, by+3)
	if r, _ := windowOf(t, s, "w"); r != start {
		t.Fatalf("a press the Button took moved the window to %+v, from %+v", r, start)
	}
	fx, fy, _ := at(s.String(), "┌ f")
	plainDrag(t, s, fx+1, fy+1, fx+6, fy+2) // the field, one column in from the frame's left side
	var focused bool
	onScreenLoop(t, s, func() { c, _ := s.Program.Find("field"); focused = s.Program.App().FocusWithin(c) })
	if !focused {
		t.Errorf("a press on the field beside the frame didn't reach it:\n%s", s)
	}
	if r, _ := windowOf(t, s, "w"); r != start {
		t.Fatalf("a press the field took moved or resized the window to %+v, from %+v", r, start)
	}
}

// A governed member refuses a move from its title row. Its side shared with the page resizes their
// border; its screen-edge side refuses, and leaves the shared border where it was. A bottom corner
// resizes the side it shares. Its resize arrows are only over what it shares: its right side and
// the corner beside it, each the side's arrow.
func TestATiledMembersFrameMovesOnlyItsSharedBorder(t *testing.T) {
	b := tui.NewTestBackend(80, 24, tui.WithTestCapabilities(tui.Capabilities{FrameDrag: true}))
	rec := &calls{}
	s := decltest.RunOn(t, b, nil, tuidecl.LayoutSource("main.qml", []byte(memberDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(memberSources()),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	s.WaitForText(t, "PAGE")
	call(t, s, "ex", "open")
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
	plainDrag(t, s, 5, 0, 30, 6)
	plainDrag(t, s, 0, 10, 6, 10)
	titleAt(t, s, "EXPLORER", 2, 0)
	titleAt(t, s, "PAGE", 22, 0)
	for _, c := range rec.all() {
		if strings.HasPrefix(c, "tile ") {
			t.Fatalf("a refused frame drag changed the tile: %q", rec.all())
		}
	}
	var shapes []tui.PointerRegion
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find("ex")
		core := c.(interface{ WindowBehavior() *widget.WindowCore }).WindowBehavior()
		r := core.Bounds()
		shapes = core.FrameShapes(nil, tui.Size{W: r.W, H: r.H})
	})
	want := []tui.PointerRegion{
		{Rect: tui.Rect{X: 19, Y: 1, W: 1, H: 22}, Shape: tui.PointerResizeEW},
		{Rect: tui.Rect{X: 19, Y: 23, W: 1, H: 1}, Shape: tui.PointerResizeEW},
	}
	if !slices.Equal(shapes, want) {
		t.Errorf("a tile's arrows: %+v, want only its shared side's %+v", shapes, want)
	}
	plainDrag(t, s, 19, 10, 31, 10)
	titleAt(t, s, "PAGE", 34, 0)
	s.WaitFor(t, "tileChanged for the resize", func(string) bool {
		return slices.Contains(rec.all(), "tile ex resize left 40 true")
	})
	plainDrag(t, s, 31, 23, 23, 23) // the bottom-right corner: the right side it shares
	titleAt(t, s, "PAGE", 26, 0)
}

// A second press on the title row toggles a Drawer's maximize, both ways. A FloatWindow, which has
// no maximize, stays as it was.
func TestADoubleClickOnTheTitleRowTogglesMaximize(t *testing.T) {
	doubleClick := func(s *decltest.Screen, x, y int) {
		s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y, Count: 1},
			tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y},
			tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y, Count: 2},
			tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	}
	s := frameScreen(t, true, ` Text { text: "page" }
 Drawer { id: dr; modal: false; edge: Tui.Right; size: 50; maximizable: true; closeOnQ: false
  Frame { title: "drawer"; Text { text: "d" } } }`, &calls{})
	call(t, s, "dr", "open")
	s.WaitForText(t, "drawer")
	before, _ := windowOf(t, s, "dr")
	doubleClick(s, before.X+3, before.Y)
	s.WaitFor(t, "maximized", func(string) bool { _, st := windowOf(t, s, "dr"); return st.Maximized })
	r, _ := windowOf(t, s, "dr")
	doubleClick(s, r.X+3, r.Y)
	s.WaitFor(t, "restored", func(string) bool { _, st := windowOf(t, s, "dr"); return !st.Maximized })

	f := frameScreen(t, true, floatAt10, &calls{})
	f.WaitForText(t, "win")
	waitRect(t, f, "w", "at its percentages", tui.Rect{X: 10, W: 10, H: 5})
	doubleClick(f, 13, 0)
	if r, st := windowOf(t, f, "w"); r != (tui.Rect{X: 10, W: 10, H: 5}) || st.Maximized {
		t.Fatalf("a double-click on a FloatWindow's title row: %+v, maximized %v", r, st.Maximized)
	}
}
