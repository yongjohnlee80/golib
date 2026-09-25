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

// A TableView shows the MODEL's columns: two queries with different columns on
// the same view, nothing rebound — ColumnsReset redraws the header and rows.
func TestATableViewShowsTheModelsColumnsAsTheyChange(t *testing.T) {
	m, rec := tuidecl.NewListModel("id", "name", "engine"), &recorder{}
	m.SetColumns(tuidecl.Column{Role: "id", Title: "ID"}, tuidecl.Column{Role: "name", Title: "NAME"})
	m.Reset([]tuidecl.Row{{"id": 1, "name": "prod"}})
	s := runModelDoc(t, `TableView { model: App.people; onActivated: App.use(index) }`, m, rec)
	s.WaitFor(t, "the first result", func(sc string) bool {
		return strings.Contains(sc, "ID") && strings.Contains(sc, "NAME") && strings.Contains(sc, "prod")
	})
	onScreenLoop(t, s, func() {
		m.SetColumns(tuidecl.Column{Role: "engine", Title: "ENGINE"})
		m.Reset([]tuidecl.Row{{"engine": "postgres"}, {"engine": "mysql"}})
	})
	s.WaitFor(t, "the second result, other columns", func(sc string) bool {
		return strings.Contains(sc, "ENGINE") && strings.Contains(sc, "mysql") && !strings.Contains(sc, "NAME")
	})
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab},
		tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown},
		tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "activated", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0]; got.Raw != "1" {
		t.Errorf("activated(index) = %q, want 1", got.Raw)
	}
}

// Declared TableViewColumns show fixed roles under fixed titles, whatever
// columns the model has.
func TestDeclaredTableViewColumnsOverrideTheModels(t *testing.T) {
	m := people()
	s := runModelDoc(t, "TableView { model: App.people\n"+
		` TableViewColumn { role: "name"; title: "WHO"; width: 8 }`+"\n"+
		` TableViewColumn { role: "id"; title: "NO"; width: 0 } }`, m, &recorder{})
	s.WaitFor(t, "the declared columns", func(sc string) bool {
		return strings.Contains(sc, "WHO") && strings.Contains(sc, "NO") && strings.Contains(sc, "bob")
	})
}

// A TableView holds only its columns.
func TestATableViewHoldsTableViewColumnsOnly(t *testing.T) {
	_, err := mountDoc(t, "TableView { Text { } }")
	if err == nil || !strings.Contains(err.Error(), "TableViewColumns only") {
		t.Fatalf("err = %v, want a Text child refused", err)
	}
}

// A TreeView asks its model for a row's children the first time it opens —
// once — shows them when the host sets them, and raises activated/expanded
// with the row's Index, which the handler receives as the host's own value.
func TestATreeViewLoadsChildrenWhenAskedAndPassesTheIndex(t *testing.T) {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "db", "label": "database"}, HasChildren: true},
		{Row: tuidecl.Row{"key": "x", "label": "leafrow"}},
	})
	var fetches []tuidecl.Index
	m.OnFetch = func(ix tuidecl.Index) { fetches = append(fetches, ix) }
	opened, used := &recorder{}, &recorder{}
	s := decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { model: App.tree; textRole: "label"; onActivated: App.use(index); onExpanded: App.opened(index) }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": used.handler, "App.opened": opened.handler}))
	s.WaitFor(t, "the top rows", func(sc string) bool { return strings.Contains(sc, "database") && strings.Contains(sc, "leafrow") })
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, enter) // open "database"
	s.WaitFor(t, "expanded", func(string) bool { return len(opened.all()) == 1 })
	var nFetch int
	onScreenLoop(t, s, func() {
		nFetch = len(fetches)
		if nFetch == 1 {
			m.SetChildren(&fetches[0], []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "t", "label": "users"}}})
		}
	})
	if nFetch != 1 || fetches[0].Row != 0 || fetches[0].Parent != nil {
		t.Fatalf("fetches %+v, want one, for the top row 0", fetches)
	}
	if ix, ok := opened.all()[0].Obj.(tuidecl.Index); !ok || ix.Row != 0 {
		t.Errorf("expanded(index) carried %+v, want the row's Index", opened.all()[0])
	}
	s.WaitFor(t, "the child", func(sc string) bool { return rowOfText(s, "users") == rowOfText(s, "database")+1 })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}, enter) // activate "users"
	s.WaitFor(t, "activated", func(string) bool { return len(used.all()) == 1 })
	ix, ok := used.all()[0].Obj.(tuidecl.Index)
	if !ok || ix.Row != 0 || ix.Parent == nil || ix.Parent.Row != 0 {
		t.Errorf("activated(index) carried %+v, want users' Index: row 0 under row 0", used.all()[0])
	}
	// Closed and opened again: its children are loaded, so no second fetch.
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyUp}, enter, enter)
	s.WaitFor(t, "opened again", func(string) bool { return len(opened.all()) >= 2 })
	onScreenLoop(t, s, func() { nFetch = len(fetches) })
	if nFetch != 1 {
		t.Errorf("reopening fetched again: %d fetches", nFetch)
	}
}

// A TreeView wants a tree model; a flat one is refused by name.
func TestATreeViewRefusesAFlatModel(t *testing.T) {
	err := tuidecl.Check(
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nTreeView { model: App.flat }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.flat": people()}))
	if err == nil || !strings.Contains(err.Error(), "wants a tree model") {
		t.Fatalf("err = %v, want a flat model refused", err)
	}
}

// The chosen record stays chosen as rows are inserted before it — Qt's
// persistent index, by the model's Key — and one removed is no longer chosen:
// currentValue never reads another record's value.
func TestAComboBoxKeepsTheChosenRecordAcrossChanges(t *testing.T) {
	m, rec := people(), &recorder{}
	s := runModelDoc(t, "Window {\n"+
		` ComboBox { id: who; model: App.people; textRole: "name"; valueRole: "id"; placeholderText: "pick" }`+"\n"+
		` Shortcut { sequence: "Ctrl+G"; onActivated: App.use(who.currentValue) } }`, m, rec)
	s.WaitForText(t, "pick")
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	ctrlG := tui.KeyEvent{Kind: tui.KeyPress, Code: 'g', Mods: tui.ModCtrl}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, enter)
	s.WaitForText(t, "bob")
	s.Keys(t, enter) // ann, id 1
	s.WaitFor(t, "ann chosen", func(sc string) bool { return strings.Contains(sc, "ann") && !strings.Contains(sc, "bob") })
	onScreenLoop(t, s, func() { m.Insert(0, tuidecl.Row{"key": "z", "name": "zed", "id": 9}) })
	s.Keys(t, ctrlG)
	s.WaitFor(t, "a read", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0]; got.Raw != "1" {
		t.Fatalf("after an insert before it, currentValue = %q, want ann's 1", got.Raw)
	}
	onScreenLoop(t, s, func() { m.Remove(1, 1) }) // ann
	s.Keys(t, ctrlG)
	s.WaitFor(t, "a second read", func(string) bool { return len(rec.all()) == 2 })
	if got := rec.all()[1]; got.Raw != "" {
		t.Errorf("after the chosen record was removed, currentValue = %q, want nothing chosen", got.Raw)
	}
}

// A ListView's cursor stays on its record as rows are inserted before it, so
// activating it names that record's row.
func TestAListViewCursorFollowsItsRecord(t *testing.T) {
	m, rec := people(), &recorder{}
	s := runModelDoc(t, `ListView { model: App.people; textRole: "name"; onActivated: App.use(index) }`, m, rec)
	s.WaitForText(t, "bob")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	onScreenLoop(t, s, func() {}) // the Down lands
	onScreenLoop(t, s, func() { m.Insert(0, tuidecl.Row{"key": "z", "name": "zed"}) })
	s.WaitForText(t, "zed")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "activated", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0].Raw; got != "2" || m.At(2)["name"] != "bob" {
		t.Errorf("activated row %q, want 2 — bob's row after the insert", got)
	}
}
