package decl_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// views_test.go: ListView and ComboBox over a host model — Qt's model/view.

func people() *tuidecl.ListModel {
	m := tuidecl.NewListModel("key", "name", "id")
	m.Reset([]tuidecl.Row{{"key": "a", "name": "ann", "id": 1}, {"key": "b", "name": "bob", "id": 2}})
	return m
}

// recorder collects handler arguments; handlers run on the loop.
type recorder struct {
	mu  sync.Mutex
	got []qml.SpecValue
}

func (r *recorder) handler(args []qml.SpecValue) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, args...)
	return nil
}

func (r *recorder) all() []qml.SpecValue {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]qml.SpecValue(nil), r.got...)
}

func runModelDoc(t *testing.T, src string, m *tuidecl.ListModel, rec *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+src)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.people": m}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
}

// A ListView shows its model's rows, follows an insert without anything
// rebinding, and raises activated(index) with the row.
func TestAListViewShowsAndFollowsItsModel(t *testing.T) {
	m, rec := people(), &recorder{}
	s := runModelDoc(t, `ListView { model: App.people; textRole: "name"; onActivated: App.use(index) }`, m, rec)
	s.WaitFor(t, "the rows", func(sc string) bool { return strings.Contains(sc, "ann") && strings.Contains(sc, "bob") })
	onScreenLoop(t, s, func() { m.Insert(1, tuidecl.Row{"key": "c", "name": "cat", "id": 3}) })
	s.WaitFor(t, "the inserted row between", func(sc string) bool {
		return rowOfText(s, "cat") == rowOfText(s, "ann")+1 && rowOfText(s, "bob") == rowOfText(s, "cat")+1
	})
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab},
		tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown},
		tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "activated", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0]; got.Kind != qml.SpecValueNumber || got.Raw != "1" {
		t.Errorf("activated(index) = %+v, want the row, 1", got)
	}
}

// A handler reads a ComboBox's currentValue by its id — the chosen row's
// valueRole, typed — and nothing chosen reads as "" and -1.
func TestAComboBoxsCurrentValueIsReadByAHandler(t *testing.T) {
	m, rec := people(), &recorder{}
	s := runModelDoc(t, "Window {\n"+
		` ComboBox { id: who; model: App.people; textRole: "name"; valueRole: "id"; placeholderText: "pick" }`+"\n"+
		` Shortcut { sequence: "Ctrl+G"; onActivated: App.use(who.currentValue, who.currentIndex) } }`, m, rec)
	s.WaitForText(t, "pick")
	ctrlG := tui.KeyEvent{Kind: tui.KeyPress, Code: 'g', Mods: tui.ModCtrl}
	s.Keys(t, ctrlG)
	s.WaitFor(t, "the empty read", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all(); got[0].Raw != "" || got[1].Raw != "-1" {
		t.Errorf("nothing chosen: currentValue, currentIndex = %q, %q; want \"\", -1", got[0].Raw, got[1].Raw)
	}
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, enter)
	s.WaitForText(t, "bob") // the choices, open
	s.Keys(t, enter)        // the first: ann
	s.WaitFor(t, "ann chosen", func(sc string) bool { return strings.Contains(sc, "ann") && !strings.Contains(sc, "bob") })
	s.Keys(t, ctrlG)
	s.WaitFor(t, "the read", func(string) bool { return len(rec.all()) == 4 })
	got := rec.all()[2:]
	if got[0].Kind != qml.SpecValueNumber || got[0].Raw != "1" || got[1].Raw != "0" {
		t.Errorf("currentValue, currentIndex = %+v, %+v; want 1 (ann's id), 0", got[0], got[1])
	}
}

// A model a view no longer shows has no subscriber left: replaced by a reload
// that removes the view, or by another model.
func TestAViewLeavesNoSubscriptionBehind(t *testing.T) {
	m, rec := people(), &recorder{}
	s := runModelDoc(t, "Flex { direction: Tui.Vertical\n ListView { model: App.people; textRole: \"name\" }\n Text { text: \"end\" } }", m, rec)
	s.WaitForText(t, "ann")
	var subs int
	onScreenLoop(t, s, func() { subs = m.Subscribers() })
	if subs != 1 {
		t.Fatalf("subscribers %d, want 1", subs)
	}
	reloadScreen(t, s, "import tui 1.0\nimport demo 1.0\nFlex { direction: Tui.Vertical\n Text { text: \"end\" } }")
	s.WaitFor(t, "the view gone", func(sc string) bool { return !strings.Contains(sc, "ann") })
	onScreenLoop(t, s, func() { subs = m.Subscribers() })
	if subs != 0 {
		t.Errorf("a removed view left %d subscribers", subs)
	}
	other := people()
	s2 := runModelDoc(t, `ListView { model: App.people; textRole: "name" }`, m, rec)
	onScreenLoop(t, s2, func() {
		if err := s2.Program.Set("App.people", other); err != nil {
			t.Error(err)
		}
		subs = m.Subscribers()
	})
	if subs != 0 || other.Subscribers() != 1 {
		t.Errorf("after replacing the model: old %d subscribers, new %d; want 0, 1", subs, other.Subscribers())
	}
}

// A model bound where text is wanted is refused by position.
func TestAModelWhereTextIsWantedIsRefused(t *testing.T) {
	err := tuidecl.Check(
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nText { text: App.people }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.people": people()}))
	if err == nil || !strings.Contains(err.Error(), "want a string, got object") {
		t.Fatalf("err = %v, want the model refused as text", err)
	}
}
