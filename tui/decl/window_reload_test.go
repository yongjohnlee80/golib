package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// window_reload_test.go: a reload that changes a Window's children changes
// THEM, and leaves the rest mounted — the Window sorts its children into keys,
// menus, dialogs and a dock, and re-sorts them in place. Rebuilding it instead
// would rebuild the whole screen for an edit to one menu.

func windowDoc(extra string) string {
	return "import tui 1.0\nimport demo 1.0\nWindow {\n" +
		" Editor { id: ed; focus: true }\n" +
		" StatusBar { id: bar; Dock.edge: Tui.Bottom; left: \"status\" }\n" + extra + "\n}"
}

func TestAWindowTakesChangedChildrenInPlace(t *testing.T) {
	var s *decltest.Screen
	fired := map[string]int{}
	s = decltest.Run(t, 40, 8, tuidecl.LayoutSource("main.qml", []byte(windowDoc(""))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Commands(map[string]func() error{
			"App.one": func() error { fired["one"]++; return nil },
			"App.two": func() error { fired["two"]++; return nil },
		}))
	s.WaitForText(t, "status")
	s.Keys(t, decltest.Type("ityped")...)
	s.WaitForText(t, "typed")
	var ed *widget.Editor
	var mount tui.NodeID
	onScreenLoop(t, s, func() {
		ed, _ = tuidecl.FindAs[*widget.Editor](s.Program, "ed")
		mount = ed.NodeID()
	})

	// The same widget, the same text — and the same MOUNT: a remount would
	// keep the Go object and its text but give it a new runtime node, and
	// with it lose its subscriptions and in-flight work.
	check := func(what string) {
		t.Helper()
		onScreenLoop(t, s, func() {
			now, _ := tuidecl.FindAs[*widget.Editor](s.Program, "ed")
			if now != ed || now.Value() != "typed" || now.NodeID() != mount {
				t.Errorf("%s: the editor was replaced, remounted or lost its text", what)
			}
		})
	}
	reload := func(what, src string) {
		t.Helper()
		var res struct {
			root bool
			err  error
		}
		onScreenLoop(t, s, func() {
			r, err := s.Program.Reload([]byte(src))
			res.root, res.err = r.RootReplaced, err
		})
		if res.err != nil || res.root {
			t.Fatalf("%s: err %v, root replaced %v", what, res.err, res.root)
		}
		check(what)
	}

	// A new docked child, a new shortcut, a new dialog.
	reload("adding", windowDoc(" Text { id: top; Dock.edge: Tui.Top; text: \"banner\" }\n"+
		" Shortcut { sequence: \"Ctrl+O\"; onActivated: App.one() }\n"+
		" Dialog { id: dlg; title: \"Late\"; Text { text: \"arrived\" } }"))
	s.WaitForText(t, "banner")
	s.Keys(t, decltest.Ctrl('o'))
	s.WaitFor(t, "the new shortcut firing", func(string) bool {
		var n int
		onScreenLoop(t, s, func() { n = fired["one"] })
		return n == 1
	})
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("dlg", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "arrived")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "the dialog closing", func(sc string) bool { return !strings.Contains(sc, "arrived") })

	// The shortcut replaced: the old one no longer fires, the new one does.
	reload("replacing", windowDoc(" Text { id: top; Dock.edge: Tui.Top; text: \"banner\" }\n"+
		" Shortcut { sequence: \"Ctrl+O\"; onActivated: App.two() }"))
	s.Keys(t, decltest.Ctrl('o'))
	s.WaitFor(t, "the replacing shortcut firing", func(string) bool {
		var n int
		onScreenLoop(t, s, func() { n = fired["two"] })
		return n == 1
	})
	onScreenLoop(t, s, func() {
		if fired["one"] != 1 {
			t.Errorf("the removed shortcut fired again: %d", fired["one"])
		}
	})

	// Removing the banner.
	reload("removing", windowDoc(""))
	s.WaitFor(t, "the banner gone", func(sc string) bool { return !strings.Contains(sc, "banner") })
	s.Keys(t, decltest.Type("!")...)
	s.WaitForText(t, "typed!")
}

// TestAWindowReorderedInPlaceKeepsItsChildren: moving the status bar above the
// editor in the file moves it, and mounts nothing anew.
func TestAWindowReorderedInPlaceKeepsItsChildren(t *testing.T) {
	top := "import tui 1.0\nWindow {\n Text { id: a; Dock.edge: Tui.Top; text: \"first\" }\n Text { id: b; Dock.edge: Tui.Top; text: \"second\" }\n Editor { }\n}"
	swapped := "import tui 1.0\nWindow {\n Text { id: b; Dock.edge: Tui.Top; text: \"second\" }\n Text { id: a; Dock.edge: Tui.Top; text: \"first\" }\n Editor { }\n}"
	s := decltest.Run(t, 30, 6, tuidecl.LayoutSource("main.qml", []byte(top)))
	s.WaitForText(t, "first")
	row := func(text string) int {
		for i, r := range strings.Split(s.String(), "\n") {
			if strings.Contains(r, text) {
				return i
			}
		}
		return -1
	}
	if row("first") > row("second") {
		t.Fatalf("fixture order wrong:\n%s", s.String())
	}
	var before tui.Component
	onScreenLoop(t, s, func() { before, _ = s.Program.Find("a") })
	onScreenLoop(t, s, func() {
		if r, err := s.Program.Reload([]byte(swapped)); err != nil || r.RootReplaced || r.Created != 0 {
			t.Errorf("reorder: %+v %v", r, err)
		}
	})
	s.WaitFor(t, "second above first", func(string) bool { return row("second") < row("first") })
	onScreenLoop(t, s, func() {
		if after, _ := s.Program.Find("a"); after != before {
			t.Error("moving a child rebuilt it")
		}
	})
}

// TestAWindowRefusesAStructuralEditOutOfRange: the engine asks for an index
// in range; one that is not is refused, not clamped into a silent misplace.
func TestAWindowRefusesAStructuralEditOutOfRange(t *testing.T) {
	tr, a := mount(t, "import tui 1.0\nWindow {\n Text { id: one; text: \"a\" }\n Text { id: two; text: \"b\" }\n}", nil, nil)
	win := tr.Root()
	one, _ := tr.NodeByID("one")
	two, _ := tr.NodeByID("two")
	if err := a.InsertChild(win, one, 9); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("insert at 9: %v", err)
	}
	if err := a.MoveChild(win, one, 9); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("move to 9: %v", err)
	}
	// Removing what is not a child leaves the children as they were.
	if err := a.RemoveChild(one, two); err == nil {
		t.Error("removing a child from a Text succeeded")
	}
	if err := a.RemoveChild(win, win); err != nil {
		t.Errorf("removing a node that is not a child: %v", err)
	}
	if err := a.MoveChild(win, two, 0); err != nil {
		t.Errorf("a valid move after the refusals: %v", err)
	}
}
