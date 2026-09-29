package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// menubar_autohide_test.go: MenuBar.autoHide — the bar takes no row until F10
// or an Alt+letter brings it up, and goes once the keyboard has left it.

func runAutoHideDoc(t *testing.T) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 60, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top; autoHide: App.hideMenu\n  Menu { title: \"&View\"\n"+
			"   MenuItem { text: \"&Wrap\"; onTriggered: App.log(\"wrap\") } } }\n"+
			" Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.hideMenu": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "body")
	return s, rec
}

func firstRow(s *decltest.Screen) string { return strings.SplitN(s.String(), "\n", 2)[0] }

func hidden(t *testing.T, s *decltest.Screen, when string) {
	t.Helper()
	s.WaitFor(t, "the bar hidden "+when, func(sc string) bool {
		return !strings.Contains(sc, "View") && strings.Contains(firstRow(s), "body")
	})
}

func up(t *testing.T, s *decltest.Screen, when string) {
	t.Helper()
	s.WaitFor(t, "the bar up "+when, func(sc string) bool {
		return strings.Contains(firstRow(s), "View") && !strings.Contains(firstRow(s), "body")
	})
}

// TestAnAutoHiddenBarComesUpForAVisit: hidden, it gives its row to the
// document; F10 brings it up, and Escape puts it away. An Alt+letter opens its
// menu, and a row that runs puts it away too.
func TestAnAutoHiddenBarComesUpForAVisit(t *testing.T) {
	s, rec := runAutoHideDoc(t)
	hidden(t, s, "at the start")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10})
	up(t, s, "after F10")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	hidden(t, s, "after Escape")
	s.Keys(t, decltest.Alt('v'))
	s.WaitForText(t, "Wrap")
	up(t, s, "with its menu open")
	s.Keys(t, decltest.Rune('w'))
	s.WaitFor(t, "the row ran", func(string) bool { return len(rec.all()) == 1 })
	hidden(t, s, "after the row ran")
}

// TestAutoHideIsAPreference: the document's binding turns it off and on while
// the program runs; off, the bar stays up after a visit.
func TestAutoHideIsAPreference(t *testing.T) {
	s, _ := runAutoHideDoc(t)
	hidden(t, s, "at the start")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.hideMenu", false); err != nil {
			t.Error(err)
		}
	})
	up(t, s, "once the preference is off")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	up(t, s, "after a visit, with the preference off")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.hideMenu", true); err != nil {
			t.Error(err)
		}
	})
	hidden(t, s, "once the preference is on again")
}
