package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// delegatechooser_test.go: DelegateChooser — a delegate per row, chosen by a role.

// homeMenu is a catalog-shaped Home menu: an item, a submenu, an item — the
// submenu in the MIDDLE, where two Instantiators could not keep it.
func homeMenu() (*tuidecl.ListModel, *tuidecl.ListModel) {
	conns := tuidecl.NewListModel("key", "kind", "label", "id")
	conns.Reset([]tuidecl.Row{
		{"key": "sel", "kind": "item", "label": "&Select…", "id": "conn.select"},
		{"key": "edit", "kind": "item", "label": "&Edit…", "id": "conn.manage"},
	})
	rows := tuidecl.NewListModel("key", "kind", "label", "id", "rows")
	rows.Reset([]tuidecl.Row{
		{"key": "ws", "kind": "item", "label": "&Workspaces…", "id": "workspace.manage"},
		{"key": "conns", "kind": "submenu", "label": "&DB conns", "rows": conns},
		{"key": "exit", "kind": "item", "label": "E&xit", "id": "app.quit"},
	})
	return rows, conns
}

const chooserMenuDoc = "import tui 1.0\nimport demo 1.0\nWindow {\n" +
	" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&Home\"\n   Instantiator { model: App.home\n" +
	"    DelegateChooser { role: \"kind\"\n" +
	"     DelegateChoice { roleValue: \"item\"; MenuItem { text: model.label; onTriggered: App.run(model.id) } }\n" +
	"     DelegateChoice { roleValue: \"submenu\"; Menu { title: model.label\n" +
	"      Instantiator { model: model.rows; MenuItem { text: model.label; onTriggered: App.run(model.id) } } } } } } } }\n" +
	" Text { text: \"body\" } }"

func runChooserMenu(t *testing.T, rows *tuidecl.ListModel) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 60, 12,
		tuidecl.LayoutSource("main.qml", []byte(chooserMenuDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.home": rows}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.run": rec.handler}))
	s.WaitForText(t, "Home")
	return s, rec
}

// Items and a submenu from one model, each row its own kind of delegate, in
// the model's order; the submenu's rows run their commands.
func TestADelegateChooserBuildsAMenuWithASubmenuInOrder(t *testing.T) {
	rows, _ := homeMenu()
	s, rec := runChooserMenu(t, rows)
	s.Keys(t, decltest.Alt('h'))
	s.WaitForText(t, "Exit")
	ws, db, exit := rowOfText(s, "Workspaces"), rowOfText(s, "DB conns"), rowOfText(s, "Exit")
	if !(ws < db && db < exit) {
		t.Fatalf("rows out of the model's order (Workspaces %d, DB conns %d, Exit %d):\n%s", ws, db, exit, s.String())
	}
	s.Keys(t, decltest.Rune('d'))
	s.WaitForText(t, "Select")
	s.Keys(t, decltest.Rune('e'))
	s.WaitFor(t, "triggered", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0].Raw; got != "conn.manage" {
		t.Errorf("the submenu's Edit ran %q, want conn.manage", got)
	}
}

// A row whose role changes is built again as its new choice; the rows beside
// it are untouched.
func TestARowWhoseKindChangesIsItsNewChoice(t *testing.T) {
	rows, conns := homeMenu()
	s, rec := runChooserMenu(t, rows)
	onScreenLoop(t, s, func() {
		rows.Set(0, tuidecl.Row{"key": "ws", "kind": "submenu", "label": "&Workspaces", "rows": conns})
	})
	s.Keys(t, decltest.Alt('h'))
	s.WaitForText(t, "Exit")
	s.Keys(t, decltest.Rune('w')) // now a submenu: it opens, and runs nothing
	s.WaitForText(t, "Select")
	onScreenLoop(t, s, func() {})
	if len(rec.all()) != 0 {
		t.Errorf("the row, now a submenu, ran %v", rec.all())
	}
}

// The first choice that matches wins; a choice with no roleValue matches every
// row; a row no choice matches has no delegate.
func TestTheChoiceRules(t *testing.T) {
	m := tuidecl.NewListModel("key", "kind", "label")
	m.Reset([]tuidecl.Row{
		{"key": "a", "kind": "loud", "label": "alpha"},
		{"key": "b", "kind": "quiet", "label": "beta"},
		{"key": "c", "kind": "odd", "label": "gamma"},
	})
	for _, c := range []struct {
		name, choices, want, absent string
	}{
		{"no match, no delegate",
			`DelegateChoice { roleValue: "loud"; Text { text: model.label } }
			 DelegateChoice { roleValue: "quiet"; Text { text: model.label } }`, "beta", "gamma"},
		{"the first match wins, and no roleValue matches all",
			`DelegateChoice { roleValue: "loud"; Text { text: "first" } }
			 DelegateChoice { Text { text: model.label } }
			 DelegateChoice { roleValue: "loud"; Text { text: "second" } }`, "gamma", "second"},
	} {
		src := "Flex { direction: Tui.Vertical\n Repeater { model: App.rows\n  DelegateChooser { role: \"kind\"\n" + c.choices + " } } }"
		s := runRepeater(t, src, m, &recorder{})
		s.WaitForText(t, c.want)
		if strings.Contains(s.String(), c.absent) {
			t.Errorf("%s: %q is shown:\n%s", c.name, c.absent, s.String())
		}
	}
}

// Numbers are equal by value, as Qt's roleValue compares them: 1.0 written in
// the document is the model's 1.
func TestANumberRoleValueMatchesByValue(t *testing.T) {
	m := tuidecl.NewListModel("key", "n")
	m.Reset([]tuidecl.Row{{"key": "a", "n": 1}, {"key": "b", "n": 2}})
	s := runRepeater(t, "Flex { direction: Tui.Vertical\n Repeater { model: App.rows\n"+
		"  DelegateChooser { role: \"n\"\n   DelegateChoice { roleValue: 1.0; Text { text: \"one\" } } } } }", m, &recorder{})
	s.WaitForText(t, "one")
}

// What a DelegateChooser refuses, by name.
func TestWhatADelegateChooserRefuses(t *testing.T) {
	m := people()
	wrap := func(inner string) string {
		return "Flex { Repeater { model: App.rows\n" + inner + " } }"
	}
	for doc, want := range map[string]string{
		"Flex { DelegateChooser { role: \"k\"; DelegateChoice { Text { } } } }":                "a Repeater's or an Instantiator's delegate",
		"Flex { DelegateChoice { Text { } } }":                                                 "a Repeater's or an Instantiator's delegate",
		wrap("DelegateChooser { DelegateChoice { Text { } } }"):                                "needs a role",
		wrap("DelegateChooser { role: 3; DelegateChoice { Text { } } }"):                       "the name of a model role, a string",
		wrap("DelegateChooser { role: \"k\"; column: 1; DelegateChoice { Text { } } }"):        "takes only a role",
		wrap("DelegateChooser { role: \"k\" }"):                                                "at least one DelegateChoice",
		wrap("DelegateChooser { role: \"k\"; Text { } }"):                                      "DelegateChoices only",
		wrap("DelegateChooser { role: \"k\"; DelegateChoice { } }"):                            "exactly one delegate",
		wrap("DelegateChooser { role: \"k\"; DelegateChoice { Text { }\n Text { } } }"):        "exactly one delegate",
		wrap("DelegateChooser { role: \"k\"; DelegateChoice { roleValue: App.x; Text { } } }"): "a string, number or bool",
		wrap("DelegateChooser { role: \"k\"; DelegateChoice { index: 1; Text { } } }"):         "takes only a roleValue",
	} {
		err := tuidecl.Check(
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+doc)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": m, "App.x": "x"}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n err = %v\nwant %q", doc, err, want)
		}
	}
}

// Qt's QQmlDelegateChoice::match: equal as values; else both as integers;
// else both as strings — so a roleValue matches across kinds as it does in Qt.
func TestARoleValueMatchesAsQtsDoes(t *testing.T) {
	for _, c := range []struct {
		name  string
		value string // the roleValue, as written
		row   any    // the row's role value
		match bool
	}{
		{"a number and the same number", "1", 1, true},
		{"1.0 and 1, by value", "1.0", 1, true},
		{"a number and its string, as integers", "1", "1", true},
		{"a string and the number it reads as", `"2"`, 2, true},
		{"a string with the number's digits, as integers", "2", "02", true},
		{"a fraction and its string, as strings", "1.5", "1.5", true},
		{"true and 1, as integers", "true", 1, true},
		{"a string and the bool it spells", `"true"`, true, true},
		{"different numbers", "1", 2, false},
		{"a fraction rounds to its integer (qRound)", "1.1", 1, true},
		{"half rounds away from zero", "1.5", 2, true},
		{"half rounds away from zero, below zero", "-1.5", -2, true},
		{"a fraction that rounds elsewhere", "1.5", 1, false},
		{"two strings equal as integers", `"01"`, "1", true},
		{"two different numbers that round apart", "1.4", 2, false},
		{"a word and a number", `"one"`, 1, false},
		{"false and 1", "false", 1, false},
	} {
		m := tuidecl.NewListModel("key", "v")
		m.Reset([]tuidecl.Row{{"key": "a", "v": c.row}})
		s := runRepeater(t, "Flex { direction: Tui.Vertical\n Text { text: \"head\" }\n Repeater { model: App.rows\n"+
			"  DelegateChooser { role: \"v\"\n   DelegateChoice { roleValue: "+c.value+"; Text { text: \"matched\" } } } } }", m, &recorder{})
		s.WaitForText(t, "head")
		if got := strings.Contains(s.String(), "matched"); got != c.match {
			t.Errorf("%s: roleValue %s against a row's %#v matched=%v, want %v", c.name, c.value, c.row, got, c.match)
		}
	}
}

// Every template is the document, whatever rows the model has now: a choice no
// row selects, and the delegate of an empty model, are held to the rules too.
func TestADormantTemplateIsHeldToTheRules(t *testing.T) {
	rows := tuidecl.NewListModel("key", "kind")
	rows.Reset([]tuidecl.Row{{"key": "a", "kind": "item"}})
	empty := tuidecl.NewListModel("key", "kind")
	for doc, want := range map[string]string{
		// a choice no row selects, holding a misplaced chooser
		"Flex { Repeater { model: App.rows\n DelegateChooser { role: \"kind\"\n" +
			"  DelegateChoice { roleValue: \"item\"; Text { } }\n" +
			"  DelegateChoice { roleValue: \"never\"; Flex { DelegateChooser { role: \"k\"; DelegateChoice { Text { } } } } } } } }": "a Repeater's or an Instantiator's delegate",
		// the delegate of an empty model
		"Flex { Repeater { model: App.empty\n Flex { DelegateChoice { Text { } } } } }": "a Repeater's or an Instantiator's delegate",
		// a chooser inside a dormant template, its choice holding a misplaced one
		"Flex { Repeater { model: App.empty\n Flex { Repeater { model: App.rows\n DelegateChooser { role: \"kind\"\n" +
			"  DelegateChoice { Flex { DelegateChoice { Text { } } } } } } } } }": "a Repeater's or an Instantiator's delegate",
		// a dormant nested Repeater with two delegates
		"Flex { Repeater { model: App.empty\n Flex { Repeater { model: App.rows\n Text { }\n Text { } } } } }": "exactly one delegate, got 2",
		// a dormant chooser that is itself unsound
		"Flex { Repeater { model: App.rows\n DelegateChooser { role: \"kind\"\n" +
			"  DelegateChoice { roleValue: \"item\"; Text { } }\n" +
			"  DelegateChoice { roleValue: \"never\"; Flex { Repeater { model: App.rows\n DelegateChooser { } } } } } } }": "needs a role",
	} {
		err := tuidecl.Check(
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+doc)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": rows, "App.empty": empty}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n err = %v\nwant %q", doc, err, want)
		}
	}
}
