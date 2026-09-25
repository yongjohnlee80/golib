package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// menu_state_test.go: a MenuItem owns its state, as Qt's does — a user's
// toggle is the item's `checked`, raises its `toggled`, and a handler reads it.

func runMenuDoc(t *testing.T, rows string) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 60, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&View\"\n"+rows+"  } }\n"+
			" Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "View")
	return s, rec
}

// A handler reads a checkable row's `checked` — the state the user's toggle
// left, since the toggle is the item's — and `toggled` fires before
// `triggered`, as Qt's AbstractButton raises them.
func TestAMenuItemOwnsItsChecked(t *testing.T) {
	s, rec := runMenuDoc(t, "   MenuItem { id: wrap; text: \"&Wrap\"; checkable: true\n"+
		"    onToggled: App.log(\"toggled\", wrap.checked); onTriggered: App.log(\"triggered\", wrap.checked) }\n")
	for i, want := range []string{"toggled,true,triggered,true", "toggled,false,triggered,false"} {
		s.Keys(t, decltest.Alt('v'))
		s.WaitForText(t, "Wrap")
		s.Keys(t, decltest.Rune('w'))
		s.WaitFor(t, "the toggle", func(string) bool { return len(rec.all()) == 4*(i+1) })
		all := rec.all()[4*i:]
		var got []string
		for _, v := range all {
			got = append(got, v.Raw)
		}
		if g := strings.Join(got, ","); g != want {
			t.Errorf("toggle %d logged %q, want %q", i+1, g, want)
		}
	}
}

// Radio rows in a group are exclusive AMONG THE ITEMS: choosing one leaves the
// other's `checked` false, read from the item.
func TestRadioRowsAreExclusiveAmongTheItems(t *testing.T) {
	s, rec := runMenuDoc(t, "   MenuItem { id: a; text: \"&Alpha\"; group: \"g\"; checked: true; onToggled: App.log(\"a\", a.checked) }\n"+
		"   MenuItem { id: b; text: \"&Beta\"; group: \"g\"; onToggled: App.log(\"b\", b.checked); onTriggered: App.log(a.checked, b.checked) }\n")
	s.Keys(t, decltest.Alt('v'))
	s.WaitForText(t, "Beta")
	s.Keys(t, decltest.Rune('b'))
	s.WaitFor(t, "triggered", func(string) bool { return len(rec.all()) == 6 })
	// Each item the user's choice changed raises its toggled, as QAction's
	// rule has it — the cleared one first — then the chosen one's triggered.
	if got := logged(rec); got != "a,false,b,true,false,true" {
		t.Errorf("choosing Beta logged %q, want a,false,b,true,false,true", got)
	}
}

// A document's write to `checked` is the item's too, and reads back.
func TestADocumentWriteToCheckedReadsBack(t *testing.T) {
	s, rec := runMenuDoc(t, "   MenuItem { id: wrap; text: \"&Wrap\"; checkable: true; checked: true\n"+
		"    onTriggered: App.log(wrap.checked) }\n")
	s.Keys(t, decltest.Alt('v'))
	s.WaitForText(t, "Wrap")
	s.Keys(t, decltest.Rune('w')) // true → false
	s.WaitFor(t, "triggered", func(string) bool { return len(rec.all()) == 1 })
	if got := logged(rec); got != "false" {
		t.Errorf("a declared checked: true, toggled once, read %q, want false", got)
	}
}

// A document's write AFTER the bar has the row goes through the menu's one
// rule: a bound radio checked by its source clears its group, and both items
// read the result.
func TestABoundCheckedWriteGoesThroughTheMenu(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 60, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&View\"\n"+
			"   MenuItem { id: a; text: \"&Alpha\"; group: \"g\"; checked: true }\n"+
			"   MenuItem { id: b; text: \"&Beta\"; group: \"g\"; checked: App.pickB }\n"+
			"   MenuItem { text: \"&Show\"; onTriggered: App.log(a.checked, b.checked) } } }\n"+
			" Text { text: \"body\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.pickB": false}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "View")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.pickB", true); err != nil {
			t.Error(err)
		}
	})
	s.Keys(t, decltest.Alt('v'))
	s.WaitForText(t, "Show")
	s.Keys(t, decltest.Rune('s'))
	s.WaitFor(t, "shown", func(string) bool { return len(rec.all()) == 2 })
	if got := logged(rec); got != "false,true" {
		t.Errorf("after App.pickB → true, (a.checked, b.checked) = %q, want false,true", got)
	}
}
