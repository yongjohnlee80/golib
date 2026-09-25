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

// repeater_test.go: Repeater and Instantiator — a delegate per model row.

func runRepeater(t *testing.T, src string, m *tuidecl.ListModel, rec *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+src)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": m}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.run": rec.handler}))
}

// A Repeater's delegate is instantiated once per row with that row's typed
// roles and index, in the parent's place; the model changing re-expands it —
// an inserted row built, a changed one patched, a removed one gone.
func TestARepeaterInstantiatesItsDelegatePerRow(t *testing.T) {
	m := tuidecl.NewListModel("key", "label")
	m.Reset([]tuidecl.Row{{"key": "a", "label": "alpha"}, {"key": "b", "label": "beta"}})
	s := runRepeater(t, "Flex { direction: Tui.Vertical\n Text { text: \"head\" }\n"+
		" Repeater { model: App.rows\n  Text { text: model.label } }\n Text { text: \"tail\" } }", m, &recorder{})
	s.WaitFor(t, "two rows between head and tail", func(string) bool {
		return rowOfText(s, "head") == 0 && rowOfText(s, "alpha") == 1 && rowOfText(s, "beta") == 2 && rowOfText(s, "tail") == 3
	})
	onScreenLoop(t, s, func() { m.Insert(1, tuidecl.Row{"key": "c", "label": "gamma"}) })
	s.WaitFor(t, "the inserted row", func(string) bool {
		return rowOfText(s, "gamma") == 2 && rowOfText(s, "beta") == 3 && rowOfText(s, "tail") == 4
	})
	onScreenLoop(t, s, func() { m.Set(0, tuidecl.Row{"key": "a", "label": "ALPHA"}) })
	s.WaitFor(t, "the changed row", func(sc string) bool { return strings.Contains(sc, "ALPHA") && !strings.Contains(sc, "alpha") })
	onScreenLoop(t, s, func() { m.Remove(1, 1) })
	s.WaitFor(t, "the removed row", func(sc string) bool { return !strings.Contains(sc, "gamma") && rowOfText(s, "tail") == 3 })
}

// A handler in a delegate runs with its OWN row's value, whatever moved around
// it.
func TestADelegatesHandlerRunsWithItsOwnRow(t *testing.T) {
	m, rec := tuidecl.NewListModel("key", "label", "id"), &recorder{}
	m.Reset([]tuidecl.Row{{"key": "a", "label": "alpha", "id": "cmd.a"}, {"key": "b", "label": "beta", "id": "cmd.b"}})
	s := runRepeater(t, "Flex { direction: Tui.Vertical\n Repeater { model: App.rows\n"+
		"  Button { text: model.label; onClicked: App.run(model.id, index) } } }", m, rec)
	s.WaitForText(t, "beta")
	onScreenLoop(t, s, func() { m.Insert(0, tuidecl.Row{"key": "z", "label": "zeta", "id": "cmd.z"}) })
	s.WaitForText(t, "zeta")
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	tab := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
	s.Keys(t, tab, tab, tab, enter) // the third button: beta
	s.WaitFor(t, "a click", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "cmd.b" || got[1].Raw != "2" {
		t.Errorf("clicked %q at index %q, want beta's cmd.b at 2", got[0].Raw, got[1].Raw)
	}
}

// A menu bar from a catalog-shaped model: categories, each with its rows,
// through a nested Instantiator whose `model: model.rows` reads the outer row.
func TestAnInstantiatorBuildsAMenuFromAModel(t *testing.T) {
	rows := tuidecl.NewListModel("key", "label", "id", "enabled")
	rows.Reset([]tuidecl.Row{{"key": "x", "label": "Exit", "id": "app.quit", "enabled": true}})
	cats := tuidecl.NewListModel("key", "label", "rows")
	cats.Reset([]tuidecl.Row{{"key": "home", "label": "&Home", "rows": rows}})
	rec := &recorder{}
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top\n  Instantiator { model: App.menu\n"+
			"   Menu { title: model.label\n    Instantiator { model: model.rows\n"+
			"     MenuItem { text: model.label; enabled: model.enabled; onTriggered: App.run(model.id) } } } } }\n"+
			" Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.menu": cats}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.run": rec.handler}))
	s.WaitForText(t, "Home")
	s.Keys(t, decltest.Alt('h'))
	s.WaitForText(t, "Exit")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "triggered", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0].Raw; got != "app.quit" {
		t.Errorf("triggered %q, want app.quit", got)
	}
	onScreenLoop(t, s, func() { cats.Insert(1, tuidecl.Row{"key": "view", "label": "&View", "rows": rows}) })
	s.WaitForText(t, "View")
}

// A catalog's offering in a menu: a hidden row takes no row, a disabled one
// cannot be triggered.
func TestMenuRowsFromAModelHonourVisibleAndEnabled(t *testing.T) {
	rows := tuidecl.NewListModel("key", "label", "id", "enabled", "visible")
	rows.Reset([]tuidecl.Row{
		{"key": "a", "label": "Admin", "id": "user.manage", "enabled": true, "visible": false},
		{"key": "b", "label": "Zoom out", "id": "view.zoom_out", "enabled": false, "visible": true},
		{"key": "c", "label": "Exit", "id": "app.quit", "enabled": true, "visible": true},
	})
	rec := &recorder{}
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&Home\"\n   Instantiator { model: App.rows\n"+
			"    MenuItem { text: model.label; enabled: model.enabled; visible: model.visible; onTriggered: App.run(model.id) } } } }\n"+
			" Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": rows}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.run": rec.handler}))
	s.WaitForText(t, "Home")
	s.Keys(t, decltest.Alt('h'))
	s.WaitForText(t, "Exit")
	if strings.Contains(s.String(), "Admin") {
		t.Errorf("a hidden row is shown:\n%s", s.String())
	}
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	s.Keys(t, enter) // on "Zoom out", disabled — or moved past it to Exit
	onScreenLoop(t, s, func() {})
	for _, v := range rec.all() {
		if v.Raw == "view.zoom_out" {
			t.Errorf("a disabled row was triggered")
		}
	}
}

// A delegate's state is its row's: a row inserted before it leaves what was
// typed into it where it was — identity by key, patched, not rebuilt.
func TestADelegateKeepsItsStateWhenRowsAreInsertedBefore(t *testing.T) {
	m := tuidecl.NewListModel("key", "label")
	m.Reset([]tuidecl.Row{{"key": "a", "label": "first"}})
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Flex { direction: Tui.Vertical\n Repeater { model: App.rows\n  TextField { placeholderText: model.label } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Sources(map[string]any{"App.rows": m}))
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	s.Keys(t, decltest.Type("typed")...)
	s.WaitForText(t, "typed")
	onScreenLoop(t, s, func() { m.Insert(0, tuidecl.Row{"key": "z", "label": "zeroth"}) })
	s.WaitForText(t, "zeroth")
	if !strings.Contains(s.String(), "typed") || rowOfText(s, "typed") != rowOfText(s, "zeroth")+1 {
		t.Fatalf("the typed text was lost or moved:\n%s", s.String())
	}
}

// What a Repeater refuses, by name.
func TestWhatARepeaterRefuses(t *testing.T) {
	m := people()
	for doc, want := range map[string]string{
		"Flex { Repeater { model: App.rows } }":                               "exactly one delegate, got 0",
		"Flex { Repeater { Text { } } }":                                      "needs a model",
		"Flex { Repeater { model: App.rows; delegate: 1\n Text { } } }":       `"delegate" is not one of its properties`,
		"Flex { Repeater { model: App.rows; onDone: App.run()\n Text { } } }": "raises no signals",
		"Flex { Repeater { model: App.name\n Text { } } }":                    `holds a string, not a model`,
		"Flex { Repeater { model: App.none\n Text { } } }":                    "not a source holding a model",
		"Repeater { model: App.rows\n Text { } }":                             "root cannot be a Repeater",
	} {
		err := tuidecl.Check(
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+doc)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": m, "App.name": "x"}),
			tuidecl.Handlers(map[string]decl.HandlerFunc{"App.run": (&recorder{}).handler}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n err = %v\nwant %q", doc, err, want)
		}
	}
}

// Replacing the model a Repeater reads re-expands it over the new one, and the
// old one is no longer followed.
func TestARepeaterFollowsItsSourceToANewModel(t *testing.T) {
	a := tuidecl.NewListModel("key", "label")
	a.Reset([]tuidecl.Row{{"key": "1", "label": "old"}})
	b := tuidecl.NewListModel("key", "label")
	b.Reset([]tuidecl.Row{{"key": "2", "label": "new"}})
	s := runRepeater(t, "Flex { Repeater { model: App.rows\n Text { text: model.label } } }", a, &recorder{})
	s.WaitForText(t, "old")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.rows", b); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the new model's rows", func(sc string) bool { return strings.Contains(sc, "new") && !strings.Contains(sc, "old") })
	var subsA, subsB int
	onScreenLoop(t, s, func() { subsA, subsB = a.Subscribers(), b.Subscribers() })
	if subsA != 0 || subsB != 1 {
		t.Errorf("subscribers old %d, new %d; want 0, 1", subsA, subsB)
	}
}
