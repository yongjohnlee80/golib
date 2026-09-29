package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
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

// interleaveTree is deepTree plus three more top rows whose children load only
// when asked; the host answers those fetches when the test says.
func interleaveTree() (*tuidecl.TreeListModel, *[]tuidecl.Index) {
	m := deepTree()
	top := []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "db", "label": "database"}, HasChildren: true},
		{Row: tuidecl.Row{"key": "other", "label": "otherdb"}},
	}
	for _, k := range []string{"o1", "o2", "o3"} {
		top = append(top, tuidecl.TreeRow{Row: tuidecl.Row{"key": k, "label": "lazy-" + k}, HasChildren: true})
	}
	// Replacing the top level keeps db's loaded subtree only if it is set
	// again, so rebuild the loaded levels under the new top.
	m.SetChildren(nil, top)
	db := tuidecl.Index{Row: 0}
	m.SetChildren(&db, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "s", "label": "public"}, HasChildren: true}})
	schema := tuidecl.Index{Row: 0, Parent: &db}
	m.SetChildren(&schema, []tuidecl.TreeRow{
		{Row: tuidecl.Row{"key": "t1", "label": "users"}, HasChildren: true},
		{Row: tuidecl.Row{"key": "t2", "label": "orders"}},
	})
	var fetches []tuidecl.Index
	m.OnFetch = func(ix tuidecl.Index) { fetches = append(fetches, ix) }
	return m, &fetches
}

func runTreeWithSink(t *testing.T, m *tuidecl.TreeListModel, moved, used *recorder, sunk *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 30, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			`TreeView { id: tree; model: App.tree; textRole: "label"; `+
			`onCurrentIndexChanged: App.moved(index); onActivated: App.use(index) }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tree": m}),
		tuidecl.ErrorSink(func(err error) {
			_ = sunk.handler([]qml.SpecValue{{Kind: qml.SpecValueString, Raw: err.Error()}})
		}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.moved": moved.handler, "App.use": used.handler}))
}

// Unrelated branches finishing their loads while a reveal is still opening
// its row's ancestors neither end the reveal nor move the cursor: only
// changes on the row's own path count against it.
func TestATreeViewsRevealIgnoresUnrelatedBranchesLoading(t *testing.T) {
	m, fetches := interleaveTree()
	moved, used, sunk := &recorder{}, &recorder{}, &recorder{}
	s := runTreeWithSink(t, m, moved, used, sunk)
	s.WaitForText(t, "lazy-o3")
	// Open the three lazy rows, so each waits on the host for its children.
	tab := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
	down := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}
	open := tui.KeyEvent{Kind: tui.KeyPress, Code: 'l'}
	s.Keys(t, tab, down, down, open, down, open, down, open)
	var n int
	s.WaitFor(t, "three fetches asked", func(string) bool {
		onScreenLoop(t, s, func() { n = len(*fetches) })
		return n == 3
	})
	db := tuidecl.Index{Row: 0}
	schema := tuidecl.Index{Row: 0, Parent: &db}
	users := tuidecl.Index{Row: 0, Parent: &schema}
	var err error
	onScreenLoop(t, s, func() {
		// The reveal starts, and before its ancestors can open, the three
		// unrelated loads land.
		err = s.Program.Call("tree", "setCurrentIndex", users)
		for _, ix := range *fetches {
			ix := ix
			m.SetChildren(&ix, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "x", "label": "child-" + m.Key(ix)}}})
		}
	})
	if err != nil {
		t.Fatalf("setCurrentIndex: %v", err)
	}
	s.WaitFor(t, "currentIndexChanged named users", func(string) bool {
		all := moved.all()
		if len(all) == 0 {
			return false
		}
		ix, _ := all[len(all)-1].Obj.(tuidecl.Index)
		return sameIndex(ix, users)
	})
	if got := sunk.all(); len(got) != 0 {
		t.Fatalf("the reveal gave up: %v", got)
	}
}

// A row that disappears from the model while its reveal is opening the
// ancestors ends the reveal with an error, and the cursor is not put on a
// row that took its place.
func TestATreeViewsRevealGivesUpWhenTheRowDisappears(t *testing.T) {
	m, _ := interleaveTree()
	moved, used, sunk := &recorder{}, &recorder{}, &recorder{}
	s := runTreeWithSink(t, m, moved, used, sunk)
	s.WaitForText(t, "database")
	db := tuidecl.Index{Row: 0}
	schema := tuidecl.Index{Row: 0, Parent: &db}
	users := tuidecl.Index{Row: 0, Parent: &schema}
	var err error
	onScreenLoop(t, s, func() {
		err = s.Program.Call("tree", "setCurrentIndex", users)
		// users goes; orders moves up into row 0, the position users had.
		m.SetChildren(&schema, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "t2", "label": "orders"}}})
	})
	if err != nil {
		t.Fatalf("setCurrentIndex: %v", err)
	}
	s.WaitFor(t, "the reveal gave up", func(string) bool {
		for _, v := range sunk.all() {
			if strings.Contains(v.Raw, "setCurrentIndex") {
				return true
			}
		}
		return false
	})
	for _, v := range moved.all() {
		if ix, _ := v.Obj.(tuidecl.Index); sameIndex(ix, users) {
			t.Fatalf("the cursor was put on row %+v, where users used to be", ix)
		}
	}
}

// TestNewChildrenUnderAnOpenRowKeepItOpen: the host replacing the children of
// an open row — a table added to a schema — leaves the row open, the rows open
// below it open, and the cursor on its row; the new row shows.
func TestNewChildrenUnderAnOpenRowKeepItOpen(t *testing.T) {
	m := deepTree()
	used := &recorder{}
	s := runTree(t, m, &recorder{}, used)
	s.WaitForText(t, "database")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}) // the tree takes the keyboard
	s.Keys(t, decltest.Rune('l'), decltest.Rune('j'), decltest.Rune('l'), decltest.Rune('j'), decltest.Rune('l'), decltest.Rune('j'))
	s.WaitForText(t, "email") // database › public › users › email, the cursor on email
	db := tuidecl.Index{Row: 0}
	schema := tuidecl.Index{Row: 0, Parent: &db}
	users := tuidecl.Index{Row: 0, Parent: &schema}
	onScreenLoop(t, s, func() {
		m.SetChildren(&schema, []tuidecl.TreeRow{
			{Row: tuidecl.Row{"key": "t1", "label": "users"}, HasChildren: true},
			{Row: tuidecl.Row{"key": "t3", "label": "invoices"}},
			{Row: tuidecl.Row{"key": "t2", "label": "orders"}},
		})
		m.SetChildren(&users, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "c", "label": "email"}}})
	})
	s.WaitFor(t, "the new table, the open rows still open", func(sc string) bool {
		return strings.Contains(sc, "invoices") && strings.Contains(sc, "email")
	})
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}) // the cursor is still on email
	s.WaitFor(t, "activated", func(string) bool { return len(used.all()) > 0 })
	if got, want := lastIndex(t, used), (tuidecl.Index{Row: 0, Parent: &users}); !sameIndex(got, want) {
		t.Fatalf("Enter activated %+v, want email (%+v): the cursor left its row", got, want)
	}
}
