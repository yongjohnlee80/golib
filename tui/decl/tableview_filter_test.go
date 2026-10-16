package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// shows reports whether the screen shows each of in and none of out.
func shows(sc string, in, out []string) bool {
	for _, s := range in {
		if !strings.Contains(sc, s) {
			return false
		}
	}
	for _, s := range out {
		if strings.Contains(sc, s) {
			return false
		}
	}
	return true
}

// filter shows the rows its WHERE text keeps, sorted by orderBy, and reads back as set; text that
// does not parse leaves the filter as it was and filterError says why; a new result is filtered by
// it too; a filter keeping nothing leaves no current row.
func TestACellCursorTableViewFiltersByFilter(t *testing.T) {
	read := &recorder{}
	m := scores() // carol 41, alice 9, bob 100
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
    TableView { id: v; model: App.rows; cellCursor: true; orderBy: "n DESC"; filter: App.filter }
    Shortcut { sequence: "Ctrl+G"; onActivated: App.read(v.filter) }
    Shortcut { sequence: "Ctrl+E"; onActivated: App.read(v.filterError) }
    Shortcut { sequence: "Ctrl+R"; onActivated: App.read(v.currentIndex) }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": m, "App.filter": "N > 10"}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.read": read.handler}))
	s.WaitFor(t, "filtered by N > 10, sorted by N DESC", func(sc string) bool {
		return shows(sc, []string{"bob", "carol"}, []string{"alice"}) && rowsInOrder(s, "bob", "carol")
	})
	readBack := func(key rune) string {
		t.Helper()
		n := len(read.all())
		s.Keys(t, decltest.Ctrl(key))
		s.WaitFor(t, "read", func(string) bool { return len(read.all()) == n+1 })
		return read.all()[n].Raw
	}
	set := func(text string) {
		t.Helper()
		onScreenLoop(t, s, func() {
			if err := s.Program.Set("App.filter", text); err != nil {
				t.Error(err)
			}
		})
	}
	if got := readBack('g'); got != "N > 10" {
		t.Errorf("filter reads %q, want it as set", got)
	}
	if got := readBack('e'); got != "" {
		t.Errorf("filterError reads %q after text that parsed", got)
	}

	set("nope = 1")
	if got := readBack('e'); got != `no field "nope"` {
		t.Errorf("filterError %q", got)
	}
	if got := readBack('g'); got != "nope = 1" || !shows(s.String(), []string{"bob", "carol"}, []string{"alice"}) {
		t.Errorf("after text that did not parse: filter %q; the filter must stay as it was\n%s", got, s.String())
	}

	set("name LIKE 'a%' OR n = 100")
	s.WaitFor(t, "alice and bob", func(sc string) bool {
		return shows(sc, []string{"alice", "bob"}, []string{"carol"}) && rowsInOrder(s, "bob", "alice")
	})
	if got := readBack('e'); got != "" {
		t.Errorf("filterError reads %q after text that parsed", got)
	}

	onScreenLoop(t, s, func() {
		m.Reset([]tuidecl.Row{{"name": "anna", "n": 3}, {"name": "max", "n": 1}, {"name": "zed", "n": 100}})
	})
	s.WaitFor(t, "the new result filtered", func(sc string) bool {
		return shows(sc, []string{"anna", "zed"}, []string{"max", "bob"}) && rowsInOrder(s, "zed", "anna")
	})

	set("n > 1000")
	s.WaitFor(t, "no row shown", func(sc string) bool { return shows(sc, nil, []string{"anna", "zed", "max"}) })
	if got := readBack('r'); got != "-1" {
		t.Errorf("currentIndex %s with no row shown, want -1", got)
	}
	set("")
	s.WaitFor(t, "every row", func(sc string) bool { return shows(sc, []string{"anna", "zed", "max"}, nil) })
	if got := readBack('r'); got != "2" {
		t.Errorf("currentIndex %s once every row shows, want 2, zed's model row on the first row", got)
	}
}

// filter, like orderBy, needs a cellCursor.
func TestARowCursorTableViewHasNoFilter(t *testing.T) {
	err := tuidecl.Check(
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nTableView { model: App.rows; filter: \"n > 1\" }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": scores()}))
	if err == nil || !strings.Contains(err.Error(), "needs cellCursor") {
		t.Errorf("filter on a row-cursor TableView: err %v", err)
	}
}
