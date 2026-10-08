package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// floatDoc is a movable Drawer at the left edge, a third of a 60×20 Window, whose floating
// placement the host binds, as an application keeping it in its preferences does.
const floatDoc = `import tui 1.0
import demo 1.0
Window { Text { text: "page" }
Drawer { id: d; modal: false; movable: true; windowResize: true; edge: Tui.Left; size: 30; length: 100
    floating: App.floating; floatX: App.x; floatY: App.y; floatWidth: App.w; floatHeight: App.h
    onPlaced: App.log(x, y, width, height)
    onResized: App.resized(size, length)
    Frame { title: "panel"; Editor {} }
} }`

func runFloat(t *testing.T, doc string, floating bool) (*decltest.Screen, *recorder, *recorder) {
	t.Helper()
	placed, resized := &recorder{}, &recorder{}
	s := decltest.Run(t, 60, 20, tuidecl.LayoutSource("float.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.floating": floating, "App.x": 50, "App.y": 25, "App.w": 40, "App.h": 50}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": placed.handler, "App.resized": resized.handler}))
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "panel")
	return s, placed, resized
}

// panelAt is the cell where the panel's title starts: x, y; -1, -1 when it is not on the screen.
func panelAt(s *decltest.Screen) (x, y int) {
	for y, line := range strings.Split(s.String(), "\n") {
		if i := strings.Index(line, "panel"); i >= 0 {
			return len([]rune(line[:i])), y
		}
	}
	return -1, -1
}

func raws(r *recorder) []string {
	var out []string
	for _, v := range r.all() {
		out = append(out, v.Raw)
	}
	return out
}

// A docked movable Drawer dragged with Alt and the left button shows where it goes during the drag,
// floats there when the drag ends, and reports where in percent of the Window, not of the panel; a
// resize of the Window keeps it at those percentages.
func TestAMovableDrawerFloatsWhereTheDragEnds(t *testing.T) {
	s, placed, resized := runFloat(t, floatDoc, false)
	x0, y0 := panelAt(s)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 10},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 35, Y: 12})
	// the panel is the Window's height, so it moves across only
	s.WaitFor(t, "the drag's preview moves the panel", func(string) bool { x, y := panelAt(s); return x == x0+30 && y == y0 })
	if len(placed.all()) != 0 {
		t.Fatalf("placed during the drag: %v", raws(placed))
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 35, Y: 12})
	s.WaitFor(t, "placed once", func(string) bool { return len(placed.all()) == 4 })
	// 30 of 60 columns: x 50; the panel 18 of 60 wide (30) and 20 of 20 tall (100)
	if got := strings.Join(raws(placed), ","); got != "50,0,30,100" {
		t.Fatalf("placed %s, want 50,0,30,100: percent of the Window", got)
	}
	if len(resized.all()) != 0 {
		t.Errorf("a move raised resized: %v", raws(resized))
	}
	s.Backend.InjectResize(120, 40)
	// the title is two cells in from the panel's edge: at 50% of 120 the edge is column 60
	s.WaitFor(t, "the same percent of a larger Window", func(string) bool { x, _ := panelAt(s); return x == 60+x0 })
}

// A floating Drawer resized with Alt and the right button keeps its top-left corner, and reports
// the new size.
func TestAFloatingDrawerResizeKeepsItsCorner(t *testing.T) {
	s, placed, _ := runFloat(t, floatDoc, true)
	x0, y0 := panelAt(s)
	if x0 != 32 || y0 != 5 { // the title is two cells in from the edge
		t.Fatalf("floating with its title at %d,%d, want the edge at x 30 (50%%) and y 5 (25%%)", x0, y0)
	}
	// its bottom-right corner is at column 30+24-1, row 5+10-1
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 52, Y: 13},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 58, Y: 17},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 58, Y: 17})
	s.WaitFor(t, "placed once", func(string) bool { return len(placed.all()) == 4 })
	got := raws(placed)
	if got[0] != "50" || got[1] != "25" || got[2] == "40" || got[3] == "50" {
		t.Fatalf("placed %v: want x 50 and y 25 kept, a new width and height", got)
	}
	if x, y := panelAt(s); x != x0 || y != y0 {
		t.Errorf("the corner moved to %d,%d from %d,%d", x, y, x0, y0)
	}
}

// Escape during a move puts a docked Drawer back at its edge, docked, and reports nothing.
func TestEscapeDuringAMoveDocksTheDrawerAgain(t *testing.T) {
	s, placed, _ := runFloat(t, floatDoc, false)
	x0, y0 := panelAt(s)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 10},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 35, Y: 10})
	s.WaitFor(t, "moving", func(string) bool { x, _ := panelAt(s); return x == x0+30 })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "docked again", func(string) bool { x, y := panelAt(s); return x == x0 && y == y0 })
	if len(placed.all()) != 0 {
		t.Errorf("a cancelled move was placed: %v", raws(placed))
	}
}

// floating: false docks a floating Drawer at its edge again, at its size and length.
func TestFloatingFalseDocksTheDrawer(t *testing.T) {
	s, _, _ := runFloat(t, floatDoc, true)
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.floating", false); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "at the left edge", func(string) bool { x, _ := panelAt(s); return x >= 0 && x <= 2 })
}

// moveBy moves a docked movable Drawer off its edge without a pointer, and reports it once.
func TestMoveByFloatsTheDrawer(t *testing.T) {
	s, placed, _ := runFloat(t, floatDoc, false)
	x0, _ := panelAt(s)
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "moveBy", 6, 0); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "placed", func(string) bool { return len(placed.all()) == 4 })
	s.WaitFor(t, "six cells across", func(string) bool { x, _ := panelAt(s); return x == x0+6 })
	if got := raws(placed); got[0] != "10" {
		t.Errorf("placed %v, want x 10 (6 of 60)", got)
	}
}

// A Drawer that is not movable ignores the move drag, and moveBy reports it unavailable.
func TestADrawerNotMovableIgnoresTheMoveDrag(t *testing.T) {
	doc := strings.Replace(floatDoc, "movable: true; ", "", 1)
	s, placed, _ := runFloat(t, doc, false)
	x0, y0 := panelAt(s)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 10},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 35, Y: 10},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 35, Y: 10})
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "moveBy", 6, 0); err == nil {
			t.Error("moveBy moved a Drawer that is not movable")
		}
	})
	if x, y := panelAt(s); x != x0 || y != y0 || len(placed.all()) != 0 {
		t.Errorf("moved to %d,%d (from %d,%d), placed %v", x, y, x0, y0, raws(placed))
	}
}

// A movable Drawer's two buttons must differ: one drag cannot both move and resize.
func TestAMovableDrawerNeedsTwoButtons(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("bad.qml", []byte(`import tui 1.0
Window { Drawer { movable: true; windowResize: true; moveButton: Tui.RightButton; Text { text: "x" } } }`)))
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("err = %v", err)
	}
}

// Two steps before a layout add up: each starts where the last one asked the panel to be, not
// where it was last drawn.
func TestTwoStepsBeforeALayoutAddUp(t *testing.T) {
	s, placed, _ := runFloat(t, floatDoc, false)
	x0, _ := panelAt(s)
	onScreenLoop(t, s, func() {
		for range 2 {
			if err := s.Program.Call("d", "moveBy", 6, 0); err != nil {
				t.Error(err)
			}
		}
	})
	s.WaitFor(t, "twelve cells across", func(string) bool { x, _ := panelAt(s); return x == x0+12 })
	if got := raws(placed); len(got) != 8 || got[4] != "20" {
		t.Errorf("placed %v, want two: x 10, then x 20 (12 of 60)", got)
	}
}
