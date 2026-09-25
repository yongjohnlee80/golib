package decl_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// focus_test.go: forceActiveFocus() — Qt's, on every item: focus moved INTO it.

const panesDoc = `import tui 1.0
import demo 1.0
import demo.panes 1.0
Window {
 Shortcut { sequence: "Ctrl+R"; onActivated: right.forceActiveFocus() }
 Split { orientation: Tui.Horizontal
  Pane { id: left }
  Flex { id: right; direction: Tui.Vertical
   Button { text: "b"; onClicked: App.log("b") }
   Button { text: "c"; onClicked: App.log("c") } } } }`

// Pane is a component whose root is a Frame — which takes no focus — holding a
// Button: the shape of a pane in a real program.
var panes = fstest.MapFS{"Pane.qml": {Data: []byte(`Frame {
 Button { text: "a"; onClicked: App.log("a") } }`)}}

func runPanes(t *testing.T) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte(panesDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Components(panes, ".", "demo.panes", "1.0"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "c")
	return s, rec
}

var space = tui.KeyEvent{Kind: tui.KeyPress, Code: ' '}

// awayOnB puts focus on b and PROVES it — Space presses b — so the keys are
// handled before whatever the test does next from Go (keys arrive
// asynchronously; a later Go call could otherwise run first).
func awayOnB(t *testing.T, s *decltest.Screen, rec *recorder) {
	t.Helper()
	s.Keys(t, decltest.Ctrl('r'), space)
	s.WaitFor(t, "b pressed", func(string) bool { return logged(rec) == "b" })
}

// From a document: a Flex takes no focus, so its first Button does.
func TestForceActiveFocusFromADocument(t *testing.T) {
	s, rec := runPanes(t)
	s.Keys(t, decltest.Ctrl('r'), space)
	s.WaitFor(t, "pressed", func(string) bool { return len(rec.all()) == 1 })
	if got := logged(rec); got != "b" {
		t.Errorf("Space after right.forceActiveFocus() pressed %q, want the pane's first button b", got)
	}
}

// From Go, by id, on a component whose root is a Frame: the Button inside it.
func TestForceActiveFocusFromGoOnAComponent(t *testing.T) {
	s, rec := runPanes(t)
	awayOnB(t, s, rec)
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("left", "forceActiveFocus"); err != nil {
			t.Error(err)
		}
	})
	s.Keys(t, space)
	s.WaitFor(t, "pressed", func(string) bool { return len(rec.all()) == 2 })
	if got := logged(rec); got != "b,a" {
		t.Errorf("Space after Call(left, forceActiveFocus) pressed %q, want the Pane's button a", got)
	}
}

// A hidden item cannot take focus, and is left as it is, as in Qt.
func TestForceActiveFocusOnAHiddenItemDoesNothing(t *testing.T) {
	s, rec := runPanes(t)
	awayOnB(t, s, rec)
	onScreenLoop(t, s, func() {
		if c, ok := s.Program.Find("left"); ok {
			c.(interface{ SetVisible(bool) }).SetVisible(false)
		}
		if err := s.Program.Call("left", "forceActiveFocus"); err != nil {
			t.Errorf("a hidden item's forceActiveFocus() = %v, want nothing done", err)
		}
	})
	s.Keys(t, space)
	s.WaitFor(t, "pressed", func(string) bool { return len(rec.all()) == 2 })
	if got := logged(rec); got != "b,b" {
		t.Errorf("focus left b for a hidden pane: pressed %q", got)
	}
}

// What forceActiveFocus() refuses: an item with nothing on screen, arguments.
func TestWhatForceActiveFocusRefuses(t *testing.T) {
	s, _ := runPanes(t)
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("left", "forceActiveFocus", "x"); err == nil || !strings.Contains(err.Error(), "takes no arguments") {
			t.Errorf("with an argument: %v", err)
		}
	})
	// Not focusable by design: a Text, and a Flex holding only Texts.
	for _, id := range []string{"label", "labels"} {
		t2 := decltest.Run(t, 40, 6,
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nFlex { direction: Tui.Vertical\n"+
				" Text { id: label; text: \"one\" }\n Flex { id: labels; Text { text: \"two\" } } }")))
		t2.WaitForText(t, "two")
		onScreenLoop(t, t2, func() {
			if err := t2.Program.Call(id, "forceActiveFocus"); err == nil || !strings.Contains(err.Error(), "not focusable by design") {
				t.Errorf("%s.forceActiveFocus() = %v, want refused as not focusable by design", id, err)
			}
		})
	}
	rec := &recorder{}
	m := decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&File\"; MenuItem { id: row; text: \"&Go\" } } }\n Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	m.WaitForText(t, "body")
	onScreenLoop(t, m, func() {
		if err := m.Program.Call("row", "forceActiveFocus"); err == nil || !strings.Contains(err.Error(), "nothing on screen to focus") {
			t.Errorf("a menu row's forceActiveFocus() = %v, want refused", err)
		}
	})
}

// opaqueFocus is a custom component that takes focus but exposes no Context()
// accessor — a valid tui.Component, which forceActiveFocus must reach without
// one. It records a Space it receives, which only the focused component gets.
type opaqueFocus struct{ pressed atomic.Bool }

func (*opaqueFocus) Init(*tui.Context) {}
func (*opaqueFocus) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: 6, H: 1})
}
func (*opaqueFocus) Render(tui.Surface) {}
func (o *opaqueFocus) HandleEvent(ev tui.Event) bool {
	if k, ok := ev.(tui.KeyEvent); ok && k.Code == ' ' {
		o.pressed.Store(true)
		return true
	}
	return false
}
func (*opaqueFocus) AcceptsFocus() bool { return true }

// A focusable custom component without a Context() accessor is focused like
// any other: the call goes through the program's own focus owner.
func TestForceActiveFocusReachesACustomComponentWithoutAContextAccessor(t *testing.T) {
	target := &opaqueFocus{}
	s := decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nFlex { direction: Tui.Vertical\n"+
			" Button { text: \"first\" }\n Opaque { id: target } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(tuidecl.Type{Name: "Opaque", Build: func(tuidecl.Build) (tui.Component, []string, error) {
			return target, nil, nil
		}}))
	s.WaitForText(t, "first")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("target", "forceActiveFocus"); err != nil {
			t.Errorf("forceActiveFocus on a custom component = %v", err)
		}
	})
	s.Keys(t, space)
	s.WaitFor(t, "the component got Space", func(string) bool { return target.pressed.Load() })
}

// A Dialog is on screen as its modal: closed, forceActiveFocus leaves it be;
// open, focus moves into it — to the button it nominates, OK — even from the
// Cancel the user has moved to.
func TestForceActiveFocusOnADialogIsItsModals(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Button { text: \"body\"; onClicked: App.log(\"body\") }\n"+
			" Dialog { id: d; standardButtons: Dialog.Ok | Dialog.Cancel\n"+
			"  onAccepted: App.log(\"ok\"); onRejected: App.log(\"cancel\")\n"+
			"  Shortcut { sequence: \"Ctrl+K\"; onActivated: App.log(\"mark\") }\n"+
			"  Text { text: \"sure?\" } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "body")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "forceActiveFocus"); err != nil {
			t.Errorf("a closed dialog's forceActiveFocus() = %v, want nothing done", err)
		}
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, decltest.Ctrl('k')) // to Cancel, then the mark
	s.WaitFor(t, "the mark", func(string) bool { return logged(rec) == "mark" })
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "forceActiveFocus"); err != nil {
			t.Errorf("an open dialog's forceActiveFocus() = %v", err)
		}
	})
	s.Keys(t, space)
	s.WaitFor(t, "answered", func(string) bool { return len(rec.all()) == 2 })
	if got := logged(rec); got != "mark,ok" {
		t.Errorf("Space after an open dialog's forceActiveFocus() logged %q, want mark,ok", got)
	}
}

// A Shortcut is a declaration, never on screen: refused.
func TestForceActiveFocusRefusesADeclaration(t *testing.T) {
	s := decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Shortcut { id: sc; sequence: \"Ctrl+K\" }\n Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"))
	s.WaitForText(t, "body")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("sc", "forceActiveFocus"); err == nil || !strings.Contains(err.Error(), "nothing on screen to focus") {
			t.Errorf("a Shortcut's forceActiveFocus() = %v, want refused", err)
		}
	})
}
