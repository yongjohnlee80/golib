package decl_test

import (
	"fmt"
	"strings"
	"testing"

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
