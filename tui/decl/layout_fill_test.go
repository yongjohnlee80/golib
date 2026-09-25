package decl_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

func tableRows(n int) *tuidecl.ListModel {
	m := tuidecl.NewListModel("key", "name")
	rows := make([]tuidecl.Row, n)
	for i := range rows {
		rows[i] = tuidecl.Row{"key": fmt.Sprint(i), "name": fmt.Sprintf("row%02d", i)}
	}
	m.Reset(rows)
	return m
}

// A table that fills (Layout.fillHeight) shares what the button row after it
// leaves — the buttons are drawn — as in Qt's ColumnLayout; without it the
// table's own size, in order, would take every row.
func TestATableThatFillsLeavesRoomForTheButtonsAfterIt(t *testing.T) {
	s := decltest.Run(t, 40, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Flex { direction: Tui.Vertical\n"+
			" TableView { model: App.rows; Layout.fillHeight: true; TableViewColumn { role: \"name\"; title: \"NAME\" } }\n"+
			" Flex { direction: Tui.Horizontal\n  Button { text: \"&Add\" }\n  Button { text: \"&Edit\" } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tableRows(30)}))
	s.WaitFor(t, "the buttons", func(sc string) bool { return strings.Contains(sc, "Add") && strings.Contains(sc, "Edit") })
	if !strings.Contains(s.String(), "row07") {
		t.Errorf("the table did not take the rest of the height:\n%s", s.String())
	}
}

// A dialog around a table sizes to the table's rows — a view's implicit height
// is its content — and a longer one is squeezed to the screen.
func TestADialogAroundATableSizesToItsRows(t *testing.T) {
	for _, c := range []struct {
		rows int
		want string
	}{{3, "row02"}, {40, "row05"}} {
		s := decltest.RunWith(t, 50, 24, func(p *tuidecl.Program) error { p.Post(func() { _ = p.Call("d", "open") }); return nil },
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"under\" }\n"+
				" Dialog { id: d; title: \"Q\"; standardButtons: Dialog.Close\n"+
				"  Flex { direction: Tui.Vertical\n"+
				"   TableView { model: App.rows; Layout.fillHeight: true; TableViewColumn { role: \"name\"; title: \"NAME\" } }\n"+
				"   Button { text: \"&Add\" } } } }")),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": tableRows(c.rows)}))
		s.WaitForText(t, "┌ Q ")
		sc := s.String()
		if !strings.Contains(sc, c.want) || !strings.Contains(sc, "Add") || !strings.Contains(sc, "Close") {
			t.Errorf("%d rows: the dialog does not show %s, its button and its Close:\n%s", c.rows, c.want, sc)
		}
	}
}

// Layout.fillHeight is a bool; anything else is refused when the document is
// built, naming the property.
func TestLayoutFillIsABool(t *testing.T) {
	_, err := mountDoc(t, "import tui 1.0\nFlex { direction: Tui.Vertical\n Text { Layout.fillHeight: \"yes\" } }")
	if err == nil || !strings.Contains(err.Error(), "Layout.fillHeight") {
		t.Errorf("Layout.fillHeight: \"yes\": err = %v, want it refused", err)
	}
}

// fillDoc is a column holding a line of text, then table (a TableView, or
// nothing), then a row of buttons.
func fillDoc(table string) string {
	return "import tui 1.0\nimport demo 1.0\n" +
		"Flex { direction: Tui.Vertical\n Text { text: \"top\" }\n" + table +
		" Flex { id: bar; direction: Tui.Horizontal\n  Button { text: \"&Add\" }\n  Button { text: \"&Edit\" } } }"
}

const fillingTable = " TableView { model: App.rows; Layout.fillHeight: true; TableViewColumn { role: \"name\"; title: \"NAME\" } }\n"

// reloadFill reloads s with doc on its loop and returns what the reload did.
func reloadFill(t *testing.T, s *decltest.Screen, doc string) (decl.Result, error) {
	t.Helper()
	var res decl.Result
	var err error
	onScreenLoop(t, s, func() { res, err = s.Program.Reload([]byte(doc)) })
	return res, err
}

// A reload that INSERTS a filling table into a live Flex weights it as a Flex
// built with it would: the buttons after it are still drawn. The Flex is edited
// in place, not rebuilt, so this is the insertion path, not construction's.
func TestATableThatFillsInsertedByAReloadLeavesRoomForTheButtons(t *testing.T) {
	s := decltest.Run(t, 40, 10, tuidecl.LayoutSource("main.qml", []byte(fillDoc(""))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tableRows(30)}))
	s.WaitForText(t, "Add")
	res, err := reloadFill(t, s, fillDoc(fillingTable))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rebuilt) != 0 || res.Created == 0 {
		t.Fatalf("rebuilt %v, created %d: want the table inserted into the Flex in place", res.Rebuilt, res.Created)
	}
	s.WaitFor(t, "the table", func(sc string) bool { return strings.Contains(sc, "row03") })
	if sc := s.String(); !strings.Contains(sc, "Add") || !strings.Contains(sc, "Edit") {
		t.Errorf("the inserted table took the buttons' rows:\n%s", sc)
	}
}

// A reload that CHANGES a child's Layout.fillHeight rebuilds that child — an
// attached property is its parent's to read, at placement — and the new one is
// placed by what it now says.
func TestAReloadThatMakesATableFillLeavesRoomForTheButtons(t *testing.T) {
	still := strings.Replace(fillingTable, "true", "false", 1)
	s := decltest.Run(t, 40, 10, tuidecl.LayoutSource("main.qml", []byte(fillDoc(still))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tableRows(30)}))
	s.WaitForText(t, "row03")
	if strings.Contains(s.String(), "Add") {
		t.Fatalf("baseline: a table that does not fill left room for the buttons:\n%s", s.String())
	}
	if _, err := reloadFill(t, s, fillDoc(fillingTable)); err != nil {
		t.Fatal(err)
	}
	s.WaitFor(t, "the buttons", func(sc string) bool { return strings.Contains(sc, "Add") && strings.Contains(sc, "row03") })
}

// An inserted child's Layout.fillHeight is read as a built one's is: a value
// that is not a bool refuses the reload, naming the property.
func TestAnInsertedChildsLayoutFillIsABool(t *testing.T) {
	s := decltest.Run(t, 40, 10, tuidecl.LayoutSource("main.qml", []byte(fillDoc(""))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tableRows(30)}))
	s.WaitForText(t, "Add")
	_, err := reloadFill(t, s, fillDoc(strings.Replace(fillingTable, "true", "\"yes\"", 1)))
	if err == nil || !strings.Contains(err.Error(), "Layout.fillHeight") {
		t.Errorf("reload with Layout.fillHeight: \"yes\": err = %v, want it refused by name", err)
	}
}
