package decl_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// propagate_test.go holds palette propagation to its promises, P1–P9, on what
// reaches the SCREEN: the background of the cell a text
// is painted in. A palette the adapter computed and never painted passes every
// structural test.

func ansi(n uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorANSI, Index: n} }

var terminalDefault = tui.CellColor{}

const (
	blue  = 4
	red   = 1
	green = 2
	white = 7
)

// cellOf is the cell a text begins in, or fails the test.
func cellOf(t *testing.T, s *decltest.Screen, text string) tui.Cell {
	t.Helper()
	grid := s.Backend.Snapshot()
	for _, row := range grid {
		var line strings.Builder
		cols := []int{}
		for x, c := range row {
			line.WriteString(c.Content)
			for range len(c.Content) {
				cols = append(cols, x)
			}
		}
		if i := strings.Index(line.String(), text); i >= 0 {
			return row[cols[i]]
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", text, s.String())
	return tui.Cell{}
}

// waitBG waits for text to be painted on bg.
func waitBG(t *testing.T, s *decltest.Screen, text string, bg tui.CellColor) {
	t.Helper()
	s.WaitFor(t, text+" on its background", func(string) bool {
		for _, row := range s.Backend.Snapshot() {
			var line strings.Builder
			cols := []int{}
			for x, c := range row {
				line.WriteString(c.Content)
				for range len(c.Content) {
					cols = append(cols, x)
				}
			}
			if i := strings.Index(line.String(), text); i >= 0 {
				return row[cols[i]].Attrs.BG == bg
			}
		}
		return false
	})
}

// onScreenLoop runs fn on the program's loop and waits for it.
func onScreenLoop(t *testing.T, s *decltest.Screen, fn func()) {
	t.Helper()
	done := make(chan struct{})
	s.Program.Post(func() { fn(); close(done) })
	<-done
}

func runDoc(t *testing.T, src string, extra ...tuidecl.ProgramOption) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 6, append([]tuidecl.ProgramOption{tuidecl.LayoutSource("main.qml", []byte(src))}, extra...)...)
}

func reloadScreen(t *testing.T, s *decltest.Screen, src string) {
	t.Helper()
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload([]byte(src)) })
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
}

// P1 — a child with no palette paints in its parent's.
func TestP1AChildPaintsInItsParentsPalette(t *testing.T) {
	s := runDoc(t, "import tui 1.0\nWindow { palette.window: \"blue\"\n Text { text: \"hello\" } }")
	waitBG(t, s, "hello", ansi(blue))
}

// P2 + P3 — an own role wins; an unset one still comes from the parent; and
// both pass through a Flex, which wears no role at all.
func TestP2P3AnOwnRoleWinsAndTheRestInheritThroughANodeThatWearsNone(t *testing.T) {
	s := runDoc(t, "import tui 1.0\nWindow { palette.window: \"blue\"; palette.windowText: \"white\"\n"+
		" Flex { direction: Tui.Vertical\n"+
		"  Text { text: \"own\"; palette.window: \"red\" }\n"+
		"  Text { text: \"inherited\" } } }")
	waitBG(t, s, "inherited", ansi(blue))
	waitBG(t, s, "own", ansi(red))
	if fg := cellOf(t, s, "own").Attrs.FG; fg != ansi(white) {
		t.Errorf("the own-background text lost the inherited windowText: %+v", fg)
	}
}

// P4 — a parent's role moved by a binding restyles the subtree WITHOUT a
// rebuild: the editor is the same widget, and keeps what was typed.
func TestP4ALiveRoleChangeRestylesWithoutRebuilding(t *testing.T) {
	s := runDoc(t, "import tui 1.0\nimport demo 1.0\nWindow { palette.base: App.bg\n Editor { id: ed; text: \"typed\" } }",
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.bg": "blue"}))
	waitBG(t, s, "typed", ansi(blue))
	var before *widget.Editor
	onScreenLoop(t, s, func() {
		before, _ = tuidecl.FindAs[*widget.Editor](s.Program, "ed")
		before.SetValue("kept")
	})
	waitBG(t, s, "kept", ansi(blue))
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.bg", "green"); err != nil {
			t.Error(err)
		}
	})
	waitBG(t, s, "kept", ansi(green))
	onScreenLoop(t, s, func() {
		if after, _ := tuidecl.FindAs[*widget.Editor](s.Program, "ed"); after != before {
			t.Error("the editor was rebuilt by a palette change")
		}
	})
}

// P5 — a reload adding a node under a coloured parent paints it; a reload
// removing the parent's role returns the children to golib's own look.
func TestP5AReloadPaintsNewNodesAndRemovingARoleRestoresTheDefault(t *testing.T) {
	head := "import tui 1.0\nWindow { Flex { id: col; direction: Tui.Vertical; "
	s := runDoc(t, head+"palette.window: \"blue\"\n Text { id: a; text: \"first\" } } }")
	waitBG(t, s, "first", ansi(blue))
	reloadScreen(t, s, head+"palette.window: \"blue\"\n Text { id: a; text: \"first\" }\n Text { text: \"second\" } } }")
	waitBG(t, s, "second", ansi(blue))

	var before tui.Component
	onScreenLoop(t, s, func() { before, _ = s.Program.Find("a") })
	reloadScreen(t, s, head+"\n Text { id: a; text: \"first\" }\n Text { text: \"second\" } } }")
	waitBG(t, s, "first", terminalDefault)
	waitBG(t, s, "second", terminalDefault)
	onScreenLoop(t, s, func() {
		if after, _ := s.Program.Find("a"); after != before {
			t.Error("removing the parent's role rebuilt the child")
		}
	})
}

// P6 — a component's children wear the palette its use site gives it.
func TestP6AComponentsChildrenWearTheUseSitesPalette(t *testing.T) {
	files := fstest.MapFS{"ui/Card.qml": {Data: []byte("Flex { Text { text: \"in the card\" } }")}}
	s := runDoc(t, "import tui 1.0\nimport demo.ui 1.0\nWindow { Card { palette.window: \"green\" } }",
		tuidecl.Components(files, "ui", "demo.ui", "1.0"))
	waitBG(t, s, "in the card", ansi(green))
}

// P7 — no palette anywhere: golib's own look, the terminal's background.
func TestP7ADocumentWithNoPaletteKeepsGolibsLook(t *testing.T) {
	s := runDoc(t, "import tui 1.0\nWindow { Flex { Text { text: \"plain\" } } }")
	waitBG(t, s, "plain", terminalDefault)
}

// P9 — a child's own role removed by a reload: it inherits the parent's live
// role again, and is the same widget.
func TestP9RemovingAnOwnRoleInheritsTheParentsAgain(t *testing.T) {
	head := "import tui 1.0\nimport demo 1.0\nWindow { palette.window: App.bg\n Text { id: t; text: \"child\""
	s := runDoc(t, head+"; palette.window: \"red\" } }",
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.bg": "blue"}))
	waitBG(t, s, "child", ansi(red))
	var before tui.Component
	onScreenLoop(t, s, func() { before, _ = s.Program.Find("t") })

	reloadScreen(t, s, head+" } }")
	waitBG(t, s, "child", ansi(blue))
	// The parent's role is LIVE, and the child follows it now.
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.bg", "green"); err != nil {
			t.Error(err)
		}
	})
	waitBG(t, s, "child", ansi(green))
	onScreenLoop(t, s, func() {
		if after, _ := s.Program.Find("t"); after != before {
			t.Error("removing the child's own role rebuilt it")
		}
	})
}

// TestARoleIsRefusedAtRuntimeAsAtMount: the runtime path says what the mount
// path says — a misspelt role, a bad colour, a node that is not there.
func TestARoleIsRefusedAtRuntimeAsAtMount(t *testing.T) {
	tr, a := mount(t, "import tui 1.0\nText { id: x; text: \"t\" }", nil, nil)
	id, _ := tr.NodeByID("x")
	str := func(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueString, Raw: s} }
	for _, c := range []struct {
		node decl.NodeID
		prop string
		v    qml.SpecValue
		want string
	}{
		{id, "palette.windw", str("red"), "is not a palette role"},
		{id, "palette.window", str("bleu"), "is not a colour"},
		{id + 99, "palette.window", str("red"), "has no component"},
	} {
		err := a.Apply(decl.Application{Node: c.node, Prop: c.prop, Value: c.v})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Apply %s: err = %v, want %q", c.prop, err, c.want)
		}
	}
	if err := a.Reset(id, "palette.windw"); err == nil {
		t.Error("Reset of a misspelt role succeeded")
	}
	if err := a.Reset(id+99, "palette.window"); err == nil {
		t.Error("Reset on a node that is not there succeeded")
	}
	if !a.Resettable("Text", "palette.inactive.highlight") || a.Resettable("Text", "text") {
		t.Error("Resettable answers for the wrong properties")
	}
}

// TestARoleAWidgetDoesNotWearLeavesItsLookAlone: a button colour set on the
// Window reaches the editor, the menu bar and a file dialog, none of which
// wear it — each keeps golib's own look, and nothing is painted in it.
func TestARoleAWidgetDoesNotWearLeavesItsLookAlone(t *testing.T) {
	s := runDoc(t, "import tui 1.0\nWindow { palette.button: \"red\"\n"+
		" MenuBar { Dock.edge: Tui.Top; Menu { title: \"&File\" } }\n"+
		" Editor { text: \"body\" }\n"+
		" FileDialog { id: fd } }")
	waitBG(t, s, "body", terminalDefault)
	if bg := cellOf(t, s, "File").Attrs.BG; bg == ansi(red) {
		t.Error("the menu bar wore a role it does not take")
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("fd", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "Cancel")
}

// TestTheAppWidgetsRefuseWhatTheyCannotBuild: shape and property refusals.
func TestTheAppWidgetsRefuseWhatTheyCannotBuild(t *testing.T) {
	for src, want := range map[string]string{
		"Frame { }":                      "exactly 1 child",
		"Frame { colour: 1\n Text { } }": "colour",
		"Editor { wrap: \"yes\" }":       "wrap: want a bool",
		"StatusBar { Text { } }":         "takes no children",
		"StatusBar { colour: \"red\" }":  "colour",
	} {
		_, err := mountDoc(t, "import tui 1.0\n"+src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", src, err, want)
		}
	}
	tr, a := mount(t, "import tui 1.0\nEditor { id: e }", nil, nil)
	id, _ := tr.NodeByID("e")
	c, _ := a.Component(id)
	if _, ok := tuidecl.EditorOf(c); !ok {
		t.Error("EditorOf did not find the editor")
	}
	if _, ok := tuidecl.EditorOf(nil); ok {
		t.Error("EditorOf found an editor in nil")
	}
}
