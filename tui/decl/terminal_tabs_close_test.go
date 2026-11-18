//go:build linux || darwin

package decl_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget/terminal"
)

// A TerminalTabs declared with tabCloseButtons draws a × at the start of every tab, in a movable
// Drawer here. A press on a tab's ×, dragged five cells and released, emits tabCloseRequested once
// with that tab's id, and nothing else: the tab stays, the current tab stays, and the Drawer stays
// where it was, the press being the ×'s. The mutants, named here: the signal not emitted (the
// handler never runs), and the × press left unclaimed (the tab chosen).
func TestATerminalTabsCloseMarkEmitsTabCloseRequested(t *testing.T) {
	var mu sync.Mutex
	var got []string
	handlers := map[string]decl.HandlerFunc{"App.closed": func(args []qml.SpecValue) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, args[0].Raw)
		return nil
	}}
	said := func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }
	layout := "import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"page\" }\n" +
		" Drawer { id: d; modal: false; movable: true; edge: Tui.Bottom; size: 50; length: 100\n" +
		"  TerminalTabs { id: terminals; tabCloseButtons: true; onTabCloseRequested: App.closed(id) } } }"
	s := decltest.Run(t, 60, 16, tuidecl.LayoutSource("main.qml", []byte(layout)),
		tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Handlers(handlers))
	var tabs *terminal.TerminalTabs
	var one, two terminal.TerminalTabID
	s.WaitFor(t, "mounted", func(string) bool { _, ok := s.Program.Find("terminals"); return ok })
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find("terminals")
		tabs, _ = tuidecl.TerminalTabsOf(c)
		one, _ = tabs.Add("one")
		two, _ = tabs.Add("two")
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "× two")
	x, y := textAt(t, s, "× two")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: x + 5, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x + 5, Y: y})
	s.WaitFor(t, "tabCloseRequested(two)", func(string) bool { return len(said()) > 0 })
	if g := said(); len(g) != 1 || g[0] != string(two) {
		t.Fatalf("tabCloseRequested gave %v; want [%s] once", g, two)
	}
	onScreenLoop(t, s, func() {
		if tabs.Active() != one || len(tabs.Tabs()) != 2 {
			t.Errorf("after the ×: active %q, %d tabs; want one still current and both tabs there", tabs.Active(), len(tabs.Tabs()))
		}
	})
	if nx, ny := textAt(t, s, "× two"); nx != x || ny != y {
		t.Fatalf("the × press and drag moved the Drawer: two's × from (%d,%d) to (%d,%d)", x, y, nx, ny)
	}
	if !strings.Contains(s.String(), "× one") {
		t.Fatalf("one lost its ×:\n%s", s.String())
	}
}
