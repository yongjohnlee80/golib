package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// treeview_current_test.go: TreeView's currentIndexChanged and setCurrentIndex,
// the row under the cursor as a host reads and sets it.

// sameIndex reports whether a and b address the same position.
func sameIndex(a, b tuidecl.Index) bool {
	for {
		if a.Row != b.Row {
			return false
		}
		if a.Parent == nil || b.Parent == nil {
			return a.Parent == nil && b.Parent == nil
		}
		a, b = *a.Parent, *b.Parent
	}
}

// deepTree is a model whose every level is already loaded: a database, its
// schema, two tables, and a column under the first table.
func deepTree() *tuidecl.TreeListModel {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "db", "label": "database"}, HasChildren: true},
		{Row: tuidecl.Row{"key": "other", "label": "otherdb"}},
	})
	db := tuidecl.Index{Row: 0}
	m.SetChildren(&db, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "s", "label": "public"}, HasChildren: true}})
	schema := tuidecl.Index{Row: 0, Parent: &db}
	m.SetChildren(&schema, []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "t1", "label": "users"}, HasChildren: true},
		{Row: tuidecl.Row{"key": "t2", "label": "orders"}},
	})
	users := tuidecl.Index{Row: 0, Parent: &schema}
	m.SetChildren(&users, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "c", "label": "email"}}})
	return m
}

func runTree(t *testing.T, m *tuidecl.TreeListModel, moved, used *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 30, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { id: tree; model: App.tree; textRole: "label"; `+
			`onCurrentIndexChanged: App.moved(index); onActivated: App.use(index) }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.moved": moved.handler, "App.use": used.handler}))
}

func lastIndex(t *testing.T, r *recorder) tuidecl.Index {
	t.Helper()
	all := r.all()
	if len(all) == 0 {
		t.Fatal("no index recorded")
	}
	ix, ok := all[len(all)-1].Obj.(tuidecl.Index)
	if !ok {
		t.Fatalf("recorded %+v, want an Index", all[len(all)-1])
	}
	return ix
}

// Moving the cursor raises currentIndexChanged with the new row's Index; a key
// that leaves the cursor on the same row raises nothing more.
func TestATreeViewReportsTheRowUnderTheCursor(t *testing.T) {
	moved, used := &recorder{}, &recorder{}
	s := runTree(t, deepTree(), moved, used)
	s.WaitForText(t, "otherdb")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	s.WaitFor(t, "moved to otherdb", func(string) bool {
		all := moved.all()
		if len(all) == 0 {
			return false
		}
		ix, _ := all[len(all)-1].Obj.(tuidecl.Index)
		return sameIndex(ix, tuidecl.Index{Row: 1})
	})
	before := len(moved.all())
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnd}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "activated", func(string) bool { return len(used.all()) == 1 })
	if got := len(moved.all()); got != before {
		t.Fatalf("End on the last row raised currentIndexChanged again: %d events, want %d", got, before)
	}
}

// setCurrentIndex opens a closed row's ancestors and puts the cursor on it,
// without opening the row itself; Enter then activates that row.
func TestATreeViewsSetCurrentIndexRevealsALoadedRow(t *testing.T) {
	moved, used := &recorder{}, &recorder{}
	s := runTree(t, deepTree(), moved, used)
	s.WaitForText(t, "database")
	db := tuidecl.Index{Row: 0}
	schema := tuidecl.Index{Row: 0, Parent: &db}
	users := tuidecl.Index{Row: 0, Parent: &schema}
	var err error
	onScreenLoop(t, s, func() { err = s.Program.Call("tree", "setCurrentIndex", users) })
	if err != nil {
		t.Fatalf("setCurrentIndex: %v", err)
	}
	s.WaitFor(t, "users shown under public", func(string) bool {
		return rowOfText(s, "users") == rowOfText(s, "public")+1 && rowOfText(s, "public") == rowOfText(s, "database")+1
	})
	s.WaitFor(t, "currentIndexChanged named users", func(string) bool {
		all := moved.all()
		if len(all) == 0 {
			return false
		}
		ix, _ := all[len(all)-1].Obj.(tuidecl.Index)
		return sameIndex(ix, users)
	})
	if strings.Contains(s.String(), "email") {
		t.Fatalf("setCurrentIndex opened the row itself:\n%s", s.String())
	}
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "activated", func(string) bool { return len(used.all()) == 1 })
	if got := lastIndex(t, used); !sameIndex(got, users) {
		t.Fatalf("Enter activated %+v, want users", got)
	}
}

// setCurrentIndex on a row that is already shown moves the cursor at once.
func TestATreeViewsSetCurrentIndexOnAShownRow(t *testing.T) {
	moved, used := &recorder{}, &recorder{}
	s := runTree(t, deepTree(), moved, used)
	s.WaitForText(t, "otherdb")
	var err error
	onScreenLoop(t, s, func() { err = s.Program.Call("tree", "setCurrentIndex", tuidecl.Index{Row: 1}) })
	if err != nil {
		t.Fatalf("setCurrentIndex: %v", err)
	}
	s.WaitFor(t, "currentIndexChanged named otherdb", func(string) bool {
		all := moved.all()
		if len(all) == 0 {
			return false
		}
		ix, _ := all[len(all)-1].Obj.(tuidecl.Index)
		return sameIndex(ix, tuidecl.Index{Row: 1})
	})
}

// setCurrentIndex refuses what it cannot reach: no argument, a value that is
// not an Index, a row the model does not have, and a row under a parent whose
// children are still to load (the view does not fetch to reach a row).
func TestATreeViewsSetCurrentIndexRefusesWhatItCannotReach(t *testing.T) {
	m := tuidecl.NewTreeListModel("key", "label")
	m.SetChildren(nil, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "conn", "label": "prod"}, HasChildren: true}})
	moved, used := &recorder{}, &recorder{}
	s := runTree(t, m, moved, used)
	s.WaitForText(t, "prod")
	for name, args := range map[string][]any{
		"no argument":          nil,
		"not an Index":         {"conn"},
		"no such row":          {tuidecl.Index{Row: 5}},
		"two arguments":        {tuidecl.Index{Row: 0}, tuidecl.Index{Row: 0}},
		"parent still to load": {tuidecl.Index{Row: 0, Parent: &tuidecl.Index{Row: 0}}},
	} {
		var err error
		onScreenLoop(t, s, func() { err = s.Program.Call("tree", "setCurrentIndex", args...) })
		if err == nil || !strings.Contains(err.Error(), "setCurrentIndex") {
			t.Errorf("%s: err = %v, want setCurrentIndex to refuse it", name, err)
		}
	}
}
