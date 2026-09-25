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

// A ComboBox's currentIndex is writable, as Qt's is: bound to a host source,
// it chooses that row, and -1 — or a row the model does not have — chooses
// none. A host sets it when the choices are filled after the ComboBox exists,
// where Qt, too, leaves nothing chosen.
func TestAComboBoxsCurrentIndexIsSetByItsBinding(t *testing.T) {
	m, rec := people(), &recorder{}
	s := decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			` ComboBox { id: who; model: App.people; textRole: "name"; valueRole: "id"; currentIndex: App.pick }`+"\n"+
			` Shortcut { sequence: "Ctrl+G"; onActivated: App.use(who.currentValue) } }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.people": m, "App.pick": 1}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
	s.WaitForText(t, "bob")
	read := func(n int) string {
		s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'g', Mods: tui.ModCtrl})
		s.WaitFor(t, "the read", func(string) bool { return len(rec.all()) == n })
		return rec.all()[n-1].Raw
	}
	if got := read(1); got != "2" {
		t.Errorf("currentIndex: 1 chose currentValue %q, want 2 (bob's id)", got)
	}
	for i, pick := range []int{-1, 7} {
		onScreenLoop(t, s, func() {
			if err := s.Program.Set("App.pick", pick); err != nil {
				t.Error(err)
			}
		})
		if got := read(2 + i); got != "" {
			t.Errorf("currentIndex: %d chose currentValue %q, want none", pick, got)
		}
	}
}

// The motivating order: the ComboBox exists before its choices — mounted over
// an empty model, filled later — so nothing is chosen, as in Qt; the host then
// sets currentIndex, and that row is chosen.
func TestAComboBoxFilledAfterItExistsIsChosenByItsHost(t *testing.T) {
	m, rec := tuidecl.NewListModel("key", "name", "id"), &recorder{}
	s := decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			` ComboBox { id: who; model: App.people; textRole: "name"; valueRole: "id"; currentIndex: App.pick }`+"\n"+
			` Shortcut { sequence: "Ctrl+G"; onActivated: App.use(who.currentValue) } }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.people": m, "App.pick": -1}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
	read := func(n int) string {
		s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'g', Mods: tui.ModCtrl})
		s.WaitFor(t, "the read", func(string) bool { return len(rec.all()) == n })
		return rec.all()[n-1].Raw
	}
	onScreenLoop(t, s, func() {
		m.Reset([]tuidecl.Row{{"key": "a", "name": "ann", "id": 1}, {"key": "b", "name": "bob", "id": 2}})
	})
	if got := read(1); got != "" {
		t.Errorf("rows arriving after the ComboBox chose %q; Qt chooses nothing then", got)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.pick", 1); err != nil {
			t.Error(err)
		}
	})
	if got := read(2); got != "2" {
		t.Errorf("the host's currentIndex: 1 chose %q, want 2 (bob's id)", got)
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

// A result with NO columns — a statement that returns none — is a table with
// none: the view shows an empty header, and takes columns again after.
func TestATableViewShowsAResultWithNoColumns(t *testing.T) {
	m := tuidecl.NewListModel("id", "name")
	m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"})
	m.Reset([]tuidecl.Row{{"id": 1, "name": "prod"}})
	s := runModelDoc(t, `TableView { model: App.people }`, m, &recorder{})
	s.WaitForText(t, "NAME")
	onScreenLoop(t, s, func() { m.SetColumns(); m.Reset(nil) })
	s.WaitFor(t, "no columns", func(sc string) bool { return !strings.Contains(sc, "NAME") && !strings.Contains(sc, "prod") })
	onScreenLoop(t, s, func() {
		m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"})
		m.Reset([]tuidecl.Row{{"id": 2, "name": "staging"}})
	})
	s.WaitForText(t, "staging")
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
	open := tui.KeyEvent{Kind: tui.KeyPress, Code: 'l'}
	shut := tui.KeyEvent{Kind: tui.KeyPress, Code: 'h'}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, open) // open "database"
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
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyUp}, shut, open)
	s.WaitFor(t, "opened again", func(string) bool { return len(opened.all()) >= 2 })
	onScreenLoop(t, s, func() { nFetch = len(fetches) })
	if nFetch != 1 {
		t.Errorf("reopening fetched again: %d fetches", nFetch)
	}
}

// A host that decides a row is a folder opens it with the view's
// toggleExpanded, given the Index the row's activated carried — Enter opens a
// connection's schemas where it scaffolds a query on a table.
func TestATreeViewOpensARowItsHostSaysIsAFolder(t *testing.T) {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "conn", "label": "prod"}, HasChildren: true}})
	m.OnFetch = func(ix tuidecl.Index) {
		m.SetChildren(&ix, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "s", "label": "public"}}})
	}
	var s *decltest.Screen
	s = decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { id: tree; model: App.tree; textRole: "label"; onActivated: App.use(index) }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": func(args []qml.SpecValue) error {
			return s.Program.Call("tree", "toggleExpanded", args[0].Obj)
		}}))
	s.WaitForText(t, "prod")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "the folder opened", func(sc string) bool { return rowOfText(s, "public") == rowOfText(s, "prod")+1 })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "the folder closed", func(sc string) bool { return !strings.Contains(sc, "public") })
}

// toggleExpanded takes one row's Index, and a row the view shows: no argument,
// a value that is not an Index, and an Index no row answers to are each
// refused, so a stale Index from an old tree cannot open another row.
func TestATreeViewsToggleExpandedRefusesWhatIsNotAShownRow(t *testing.T) {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "conn", "label": "prod"}, HasChildren: true}})
	s := decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { id: tree; model: App.tree; textRole: "label" }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}))
	s.WaitForText(t, "prod")
	for name, args := range map[string][]any{
		"no argument":   nil,
		"not an Index":  {"conn"},
		"no row shown":  {tuidecl.Index{Row: 5}},
		"two arguments": {tuidecl.Index{Row: 0}, tuidecl.Index{Row: 0}},
	} {
		var err error
		onScreenLoop(t, s, func() { err = s.Program.Call("tree", "toggleExpanded", args...) })
		if err == nil || !strings.Contains(err.Error(), "toggleExpanded") {
			t.Errorf("%s: err = %v, want toggleExpanded to refuse it", name, err)
		}
	}
}

// toggleExpanded refuses a row the view knows but does not show: a child whose
// parent was opened, loaded, and closed again.
func TestATreeViewsToggleExpandedRefusesAHiddenChild(t *testing.T) {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "conn", "label": "prod"}, HasChildren: true}})
	m.OnFetch = func(ix tuidecl.Index) {
		m.SetChildren(&ix, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "s", "label": "public"}, HasChildren: true}})
	}
	s := decltest.Run(t, 30, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { id: tree; model: App.tree; textRole: "label" }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}))
	s.WaitForText(t, "prod")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: 'l'})
	s.WaitForText(t, "public")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'h'})
	s.WaitFor(t, "closed", func(sc string) bool { return !strings.Contains(sc, "public") })
	child := tuidecl.Index{Row: 0, Parent: &tuidecl.Index{Row: 0}}
	var err error
	onScreenLoop(t, s, func() { err = s.Program.Call("tree", "toggleExpanded", child) })
	if err == nil || !strings.Contains(err.Error(), "no row at that index is shown") {
		t.Errorf("toggling a child hidden under a closed parent: err = %v, want it refused", err)
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

// A key holding the path separator cannot make two rows one: children set for
// the root "a/b" land under it, not under the child "b" of "a".
func TestTreeKeysWithTheSeparatorDoNotCollide(t *testing.T) {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "a", "label": "rootA"}, HasChildren: true},
		{Row: tuidecl.Row{"key": "a/b", "label": "rootAB"}, HasChildren: true},
	})
	var fetches []tuidecl.Index
	m.OnFetch = func(ix tuidecl.Index) { fetches = append(fetches, ix) }
	s := decltest.Run(t, 30, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { model: App.tree; textRole: "label" }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}))
	s.WaitForText(t, "rootAB")
	key := func(c rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: c} }
	open, down := key('l'), key(tui.KeyDown)
	s.Keys(t, key(tui.KeyTab), open) // open rootA
	s.WaitFor(t, "asked for rootA's", func(string) bool { var n int; onScreenLoop(t, s, func() { n = len(fetches) }); return n == 1 })
	onScreenLoop(t, s, func() {
		m.SetChildren(&fetches[0], []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "b", "label": "childB"}, HasChildren: true}})
	})
	s.WaitForText(t, "childB")
	s.Keys(t, down, open) // open childB: its path is a, then b
	s.Keys(t, down, open) // open rootAB: its path is the one key a/b
	s.WaitFor(t, "asked for both", func(string) bool { var n int; onScreenLoop(t, s, func() { n = len(fetches) }); return n == 3 })
	onScreenLoop(t, s, func() {
		m.SetChildren(&fetches[2], []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "x", "label": "underAB"}}})
	})
	s.WaitFor(t, "rootAB's child under rootAB", func(string) bool {
		return rowOfText(s, "underAB") == rowOfText(s, "rootAB")+1
	})
}

// An empty key is a key: the record chosen under it stays chosen as rows are
// inserted before it.
func TestAnEmptyKeyIsAKeyLikeAnyOther(t *testing.T) {
	m, rec := tuidecl.NewListModel("key", "name", "id"), &recorder{}
	m.Reset([]tuidecl.Row{{"key": "", "name": "ann", "id": 1}, {"key": "b", "name": "bob", "id": 2}})
	s := runModelDoc(t, "Window {\n"+
		` ComboBox { id: who; model: App.people; textRole: "name"; valueRole: "id"; placeholderText: "pick" }`+"\n"+
		` Shortcut { sequence: "Ctrl+G"; onActivated: App.use(who.currentValue) } }`, m, rec)
	s.WaitForText(t, "pick")
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, enter)
	s.WaitForText(t, "bob")
	s.Keys(t, enter)
	s.WaitFor(t, "ann chosen", func(sc string) bool { return strings.Contains(sc, "ann") && !strings.Contains(sc, "bob") })
	onScreenLoop(t, s, func() { m.Insert(0, tuidecl.Row{"key": "z", "name": "zed", "id": 9}) })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'g', Mods: tui.ModCtrl})
	s.WaitFor(t, "a read", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0].Raw; got != "1" {
		t.Errorf("currentValue = %q, want ann's 1", got)
	}
}
