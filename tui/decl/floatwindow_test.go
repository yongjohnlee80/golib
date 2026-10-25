package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// floatWindowScreen runs src (a Window's body) on a 40 by 10 screen, App.max its maximum width's
// source and App.esc and App.placed its handlers.
func floatWindowScreen(t *testing.T, body string, esc, placed *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+body+"\n}")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Sources(map[string]any{"App.max": 30}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.esc": esc.handler, "App.placed": placed.handler}))
}

// windowRect is the FloatWindow id's committed bounds.
func windowRect(t *testing.T, s *decltest.Screen, id string) (r tui.Rect) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find(id)
		w, ok := c.(interface{ WindowBehavior() *widget.WindowCore })
		if !ok {
			t.Errorf("%s (%T) is no window", id, c)
			return
		}
		r = w.WindowBehavior().Bounds()
	})
	return r
}

// waitRect waits for the FloatWindow id's bounds to be want; a FloatWindow that moves or resizes
// has them (its core is bound to the Float's window target).
func waitRect(t *testing.T, s *decltest.Screen, id, what string, want tui.Rect) {
	t.Helper()
	var got tui.Rect
	s.WaitFor(t, what, func(string) bool { got = windowRect(t, s, id); return got == want })
	if got != want {
		t.Fatalf("%s: bounds %+v, want %+v", what, got, want)
	}
}

// A FloatWindow opens with its Window, its content holding the start-up focus (`focus: true`
// inside it): what is typed reaches it. Escape is not its own: the Window's Shortcut takes it,
// and the window stays.
func TestAFloatWindowOpensWithItsWindowAndNeverCloses(t *testing.T) {
	esc, placed := &recorder{}, &recorder{}
	s := floatWindowScreen(t, ` Shortcut { sequence: "Escape"; onActivated: App.esc("esc") }
 Text { text: "page" }
 FloatWindow { id: w; Frame { title: "win"; TextField { id: field; focus: true } } }`, esc, placed)
	s.WaitForText(t, "win")
	s.Keys(t, decltest.Type("abc")...)
	s.WaitForText(t, "abc")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "the Window's Escape", func(string) bool { return len(esc.all()) == 1 })
	if !strings.Contains(s.String(), "win") {
		t.Fatalf("Escape closed the FloatWindow:\n%s", s)
	}
	var floating bool
	onScreenLoop(t, s, func() { // the Float gui's painter is given: the one the field is on
		c, _ := s.Program.Find("field")
		in, ok := c.(interface{ Context() *tui.Context })
		if !ok {
			t.Errorf("the field (%T) has no context", c)
			return
		}
		f, ok := in.Context().Ancestor(func(v tui.Component) bool { _, ok := v.(*widget.Float); return ok }).(*widget.Float)
		floating = ok && f.Floating()
	})
	if !floating {
		t.Fatal("a FloatWindow at its percentages does not report floating (the GUI draws it no shadow)")
	}
}

// Its maximum holds its placement, not only a resize: a Window that grows never stretches it past
// maximumWidth, kept centred where its percentages put it, and a maximum that shrinks pulls it in,
// with no drag.
func TestAFloatWindowsMaximumHoldsItsPlacement(t *testing.T) {
	esc, placed := &recorder{}, &recorder{}
	s := floatWindowScreen(t, ` Text { text: "page" }
 FloatWindow { id: w; movable: true; resizable: true; maximumWidth: App.max; Frame { title: "win"; Text { text: "in" } } }`, esc, placed)
	s.WaitForText(t, "win")
	waitRect(t, s, "w", "held to 30 of 40, centred", tui.Rect{X: 5, W: 30, H: 10})
	s.Backend.InjectResize(60, 10)
	waitRect(t, s, "w", "a Window grown to 60: still 30, centred", tui.Rect{X: 15, W: 30, H: 10})
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.max", 16); err != nil {
			t.Error(err)
		}
	})
	waitRect(t, s, "w", "maximumWidth 16, with no drag", tui.Rect{X: 22, W: 16, H: 10})
}

// A drag stops at the maximum, and placed reports where it ended as percentages of the Window.
func TestAFloatWindowsResizeStopsAtItsMaximum(t *testing.T) {
	esc, placed := &recorder{}, &recorder{}
	s := floatWindowScreen(t, ` Text { text: "page" }
 FloatWindow { id: w; movable: true; resizable: true; maximumWidth: 20
  floatX: 0; floatY: 0; floatWidth: 25; floatHeight: 50
  onPlaced: App.placed(x, y, width, height)
  Frame { title: "win"; Text { text: "in" } } }`, esc, placed)
	s.WaitForText(t, "win")
	waitRect(t, s, "w", "at its percentages", tui.Rect{W: 10, H: 5})
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 5, Y: 2},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 35, Y: 2})
	waitRect(t, s, "w", "dragged 30 cells wider: held at 20", tui.Rect{W: 20, H: 5})
	s.Keys(t, tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 35, Y: 2})
	s.WaitFor(t, "placed", func(string) bool { return len(placed.all()) == 4 })
	if got := logged(placed); got != "0,0,50,50" {
		t.Errorf("placed(%s), want 0,0,50,50: 20 of 40 wide", got)
	}
	waitRect(t, s, "w", "kept where the drag ended", tui.Rect{W: 20, H: 5})
}

// A reload that adds a FloatWindow opens it, and leaves the one already open as it was.
func TestAReloadOpensAnAddedFloatWindow(t *testing.T) {
	esc, placed := &recorder{}, &recorder{}
	one := ` Text { text: "page" }
 FloatWindow { id: a; floatWidth: 50; Frame { title: "one"; Text { text: "x" } } }`
	s := floatWindowScreen(t, one, esc, placed)
	s.WaitForText(t, "one")
	reloadScreen(t, s, "import tui 1.0\nimport demo 1.0\nWindow {\n"+one+`
 FloatWindow { id: b; floatX: 50; floatWidth: 50; Frame { title: "two"; Text { text: "y" } } }
}`)
	s.WaitForText(t, "two")
	if !strings.Contains(s.String(), "one") {
		t.Fatalf("the reload closed the first FloatWindow:\n%s", s)
	}
}
