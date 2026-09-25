package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// visible_test.go: `visible` on every type — Qt's Item.visible.

func visibleDoc(extra string) string {
	return "import tui 1.0\nimport demo 1.0\nFlex { direction: Tui.Vertical\n" +
		" Text { text: \"FIRST\" }\n Text { text: \"MIDDLE\"" + extra + " }\n Text { text: \"LAST\" } }"
}

func rowOfText(s *decltest.Screen, text string) int {
	for i, row := range strings.Split(s.String(), "\n") {
		if strings.Contains(row, text) {
			return i
		}
	}
	return -1
}

// A bound `visible` follows its source: hidden, the item takes no row; shown,
// it comes back where it was.
func TestVisibleFollowsItsSource(t *testing.T) {
	s := decltest.Run(t, 20, 4,
		tuidecl.LayoutSource("main.qml", []byte(visibleDoc("; visible: App.shown"))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.shown": false}))
	s.WaitFor(t, "MIDDLE hidden, LAST moved up", func(sc string) bool {
		return !strings.Contains(sc, "MIDDLE") && rowOfText(s, "LAST") == 1
	})
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.shown", true); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "MIDDLE back", func(string) bool {
		return rowOfText(s, "MIDDLE") == 1 && rowOfText(s, "LAST") == 2
	})
}

// A reload that removes `visible: false` shows the item again: the property is
// RESET, as a removed palette role is.
func TestRemovingVisibleOnReloadShowsTheItem(t *testing.T) {
	s := decltest.Run(t, 20, 4,
		tuidecl.LayoutSource("main.qml", []byte(visibleDoc("; visible: false"))),
		tuidecl.Singleton("demo", "1.0", "App"))
	s.WaitFor(t, "hidden", func(sc string) bool { return !strings.Contains(sc, "MIDDLE") })
	var rebuilt, reset int
	onScreenLoop(t, s, func() {
		res, err := s.Program.Reload([]byte(visibleDoc("")))
		if err != nil {
			t.Error(err)
		}
		rebuilt, reset = len(res.Rebuilt), res.Reset
	})
	s.WaitFor(t, "shown", func(sc string) bool { return rowOfText(s, "MIDDLE") == 1 })
	if rebuilt != 0 || reset != 1 {
		t.Errorf("rebuilt %d, reset %d: want the item reset in place, not rebuilt", rebuilt, reset)
	}
}

// A type with nothing on screen to hide refuses it by name.
func TestAShortcutCannotBeHidden(t *testing.T) {
	_, err := mountDoc(t, "Window { Text { }\n Shortcut { sequence: \"Ctrl+X\"; visible: false } }")
	if err == nil || !strings.Contains(err.Error(), "a Shortcut cannot be hidden") {
		t.Fatalf("err = %v, want the Shortcut refused", err)
	}
}
