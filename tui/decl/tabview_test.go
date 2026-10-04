package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

var (
	ctrlPageDown = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageDown, Mods: tui.ModCtrl}
	ctrlPageUp   = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageUp, Mods: tui.ModCtrl}
	tabKey       = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
)

func runTabs(t *testing.T, src string, sources map[string]any, rec, moved *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+src)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Sources(sources),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler, "App.moved": moved.handler}))
}

// One tab's content is on screen at a time, under a bar of every title. What was
// typed in a tab is kept while another is current, and a handler reads it from
// there; Ctrl+PageDown and Ctrl+PageUp change tabs from inside one, and each
// change raises currentIndexChanged with the new index.
func TestATabViewShowsOneTabAndKeepsWhatTheOthersHold(t *testing.T) {
	rec, moved := &recorder{}, &recorder{}
	s := runTabs(t, `Window {
 Shortcut { sequence: "Ctrl+R"; onActivated: App.use(name.text, tabs.currentIndex, tabs.count) }
 TabView { id: tabs; onCurrentIndexChanged: App.moved(index)
  Tab { title: "Edit"; TextField { id: name; focus: true } }
  Tab { title: "Advanced"; Text { text: "the advanced settings" } } } }`, nil, rec, moved)
	s.WaitForText(t, "Advanced")
	if strings.Contains(s.String(), "the advanced settings") {
		t.Fatalf("a tab that is not current is on screen:\n%s", s)
	}
	s.Keys(t, decltest.Type("abc")...)
	s.WaitForText(t, "abc")

	s.Keys(t, ctrlPageDown)
	s.WaitForText(t, "the advanced settings")
	if strings.Contains(s.String(), "abc") {
		t.Fatalf("the Edit tab is still on screen:\n%s", s)
	}
	s.Keys(t, decltest.Ctrl('r'))
	s.WaitFor(t, "the handler", func(string) bool { return len(rec.all()) == 3 })
	if got := logged(rec); got != "abc,1,2" {
		t.Errorf("the handler read %q, want the text typed in the hidden tab, the current index and the count", got)
	}
	if got := logged(moved); got != "1" {
		t.Errorf("currentIndexChanged raised %q, want 1", got)
	}

	s.Keys(t, ctrlPageUp)
	s.WaitFor(t, "the Edit tab again", func(sc string) bool {
		return strings.Contains(sc, "abc") && !strings.Contains(sc, "the advanced settings")
	})
}

// A tab that is not current takes no focus: Tab moves among the bar and the
// current tab's controls only.
func TestAHiddenTabTakesNoFocus(t *testing.T) {
	rec, moved := &recorder{}, &recorder{}
	s := runTabs(t, `Window { TabView { onCurrentIndexChanged: App.moved(index)
  Tab { title: "one"; Button { text: "a"; onClicked: App.use("a") } }
  Tab { title: "two"; Button { text: "b"; onClicked: App.use("b") } } } }`, nil, rec, moved)
	s.WaitForText(t, "a")
	for range 4 {
		s.Keys(t, tabKey, space)
	}
	s.WaitFor(t, "a pressed", func(string) bool { return strings.Contains(logged(rec), "a") })
	if strings.Contains(logged(rec), "b") {
		t.Fatalf("a button in the hidden tab was pressed: %q", logged(rec))
	}
	s.Keys(t, ctrlPageDown)
	s.WaitForText(t, "b")
	for range 4 {
		s.Keys(t, tabKey, space)
	}
	s.WaitFor(t, "b pressed", func(string) bool { return strings.Contains(logged(rec), "b") })

	// back on one, the second tab's button is still made, and still out of reach
	s.Keys(t, ctrlPageUp)
	s.WaitFor(t, "the first tab again", func(string) bool { return logged(moved) == "1,0" })
	before := len(rec.all())
	for range 4 {
		s.Keys(t, tabKey, space)
	}
	s.WaitFor(t, "a pressed again", func(string) bool { return len(rec.all()) > before })
	onScreenLoop(t, s, func() {})
	for _, v := range rec.all()[before:] {
		if v.Raw == "b" {
			t.Fatalf("back on the first tab, the hidden second tab's button was pressed: %q", logged(rec))
		}
	}
}

// currentIndex is bindable: the view opens on the tab its source names and
// follows it; an index out of range is ignored.
func TestATabViewsCurrentIndexFollowsItsSource(t *testing.T) {
	rec, moved := &recorder{}, &recorder{}
	s := runTabs(t, `Window { TabView { currentIndex: App.tab
  Tab { title: "one"; Text { text: "first" } }
  Tab { title: "two"; Text { text: "second" } } } }`, map[string]any{"App.tab": 1}, rec, moved)
	s.WaitForText(t, "second")
	if strings.Contains(s.String(), "first") {
		t.Fatalf("opened on the first tab:\n%s", s)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.tab", 0); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the first tab", func(sc string) bool { return strings.Contains(sc, "first") && !strings.Contains(sc, "second") })
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.tab", 7); err != nil {
			t.Error(err)
		}
	})
	onScreenLoop(t, s, func() {})
	if sc := s.String(); !strings.Contains(sc, "first") {
		t.Fatalf("an index out of range changed the tab:\n%s", sc)
	}
}

// The bar wears the palette: the current title highlighted, the others as
// buttons; a reload with other colours restyles it, and one with none gives
// golib's own look back.
func TestATabViewsBarWearsThePalette(t *testing.T) {
	build := func(palette string) string {
		return "import tui 1.0\nWindow { TabView { " + palette + "\n" +
			" Tab { title: \"one\"; Text { text: \"first\" } }\n" +
			" Tab { title: \"two\"; Text { text: \"second\" } } } }"
	}
	s := runDoc(t, build(`palette.highlight: "red"; palette.button: "blue"`))
	waitBG(t, s, "one", ansi(red))
	waitBG(t, s, "two", ansi(blue))
	reloadScreen(t, s, build(`palette.highlight: "blue"; palette.button: "red"`))
	waitBG(t, s, "one", ansi(blue))
	waitBG(t, s, "two", ansi(red))
	reloadScreen(t, s, build(""))
	s.WaitFor(t, "golib's look", func(string) bool {
		return cellOf(t, s, "one").Attrs.BG != ansi(blue) && cellOf(t, s, "two").Attrs.BG != ansi(red)
	})
}

// A reload that adds a Tab rebuilds the view with it.
func TestAReloadAddsATab(t *testing.T) {
	build := func(extra string) string {
		return "import tui 1.0\nWindow { TabView {\n Tab { title: \"one\"; Text { text: \"first\" } }" + extra + " } }"
	}
	s := runDoc(t, build(""))
	s.WaitForText(t, "one")
	reloadScreen(t, s, build("\n Tab { title: \"three\"; Text { text: \"third\" } }"))
	s.WaitForText(t, "three")
}

// A TabView is made of Tabs, each holding one item.
func TestATabViewIsMadeOfTabs(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"no tabs":                {`TabView { }`, "needs at least one Tab"},
		"a child that is no Tab": {`TabView { Text { text: "x" } }`, "holds Tabs only"},
		"a Tab with two items":   {"TabView { Tab { title: \"a\"\n Text { text: \"x\" }\n Text { text: \"y\" } } }", "exactly 1 child"},
		"an empty Tab":           {`TabView { Tab { title: "a" } }`, "exactly 1 child"},
	} {
		err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { "+tc.src+" }")))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// A tab chosen while the view is off screen — its dialog closed — is the one shown when the dialog
// opens again, and choosing it there is no fault: a view unmounted with its dialog takes the index
// and mounts that tab when it is mounted next.
func TestATabChosenWhileItsDialogIsClosed(t *testing.T) {
	rec, moved := &recorder{}, &recorder{}
	s := runTabs(t, `Window {
 Text { text: "page" }
 Dialog { id: dlg; title: "settings"
  TabView { currentIndex: App.tab
   Tab { title: "one"; Text { text: "first" } }
   Tab { title: "two"; Text { text: "second" } } } } }`, map[string]any{"App.tab": 1}, rec, moved)
	s.WaitForText(t, "page")
	call := func(method string) {
		t.Helper()
		var err error
		onScreenLoop(t, s, func() { err = s.Program.Call("dlg", method) })
		if err != nil {
			t.Fatal(err)
		}
	}
	set := func(i int) {
		t.Helper()
		var err error
		onScreenLoop(t, s, func() { err = s.Program.Set("App.tab", i) })
		if err != nil {
			t.Fatal(err)
		}
	}
	call("open")
	s.WaitForText(t, "second")
	call("close")
	s.WaitFor(t, "the dialog closed", func(sc string) bool { return !strings.Contains(sc, "settings") })
	set(0) // the view is unmounted with its dialog
	call("open")
	s.WaitFor(t, "the first tab", func(sc string) bool { return strings.Contains(sc, "first") && !strings.Contains(sc, "second") })
	call("close")
	s.WaitFor(t, "the dialog closed again", func(sc string) bool { return !strings.Contains(sc, "settings") })
	set(1)
	call("open")
	s.WaitFor(t, "the second tab", func(sc string) bool { return strings.Contains(sc, "second") && !strings.Contains(sc, "first") })
}
