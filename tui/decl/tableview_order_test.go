package decl_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// rowsInOrder reports whether the screen shows the names top to bottom in this order.
func rowsInOrder(s *decltest.Screen, names ...string) bool {
	for i := 1; i < len(names); i++ {
		a, b := rowOfText(s, names[i-1]), rowOfText(s, names[i])
		if a < 0 || b < 0 || a >= b {
			return false
		}
	}
	return true
}

// orderBy sorts the rows as its text says and reads back as set; text that does not parse leaves
// the sort as it was and orderByError says why; the s key writes its sort back to orderBy and
// raises orderByChanged.
func TestACellCursorTableViewSortsByOrderBy(t *testing.T) {
	changed, read := &recorder{}, &recorder{}
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
    TableView { id: v; model: App.rows; cellCursor: true; sortable: true; orderBy: App.order
        onOrderByChanged: App.changed(orderBy) }
    Shortcut { sequence: "Ctrl+G"; onActivated: App.read(v.orderBy) }
    Shortcut { sequence: "Ctrl+E"; onActivated: App.read(v.orderByError) }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": scores(), "App.order": "n DESC"}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.changed": changed.handler, "App.read": read.handler}))
	s.WaitFor(t, "sorted by N descending", func(string) bool { return rowsInOrder(s, "bob", "carol", "alice") })
	readBack := func(key rune) string {
		t.Helper()
		n := len(read.all())
		s.Keys(t, decltest.Ctrl(key))
		s.WaitFor(t, "read", func(string) bool { return len(read.all()) == n+1 })
		return read.all()[n].Raw
	}
	if got := readBack('g'); got != "n DESC" {
		t.Errorf("orderBy reads %q, want it as set", got)
	}
	if got := readBack('e'); got != "" {
		t.Errorf("orderByError reads %q after text that parsed", got)
	}

	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.order", "nope DESC"); err != nil {
			t.Error(err)
		}
	})
	if got := readBack('e'); got != `no field "nope"` {
		t.Errorf("orderByError %q", got)
	}
	if got := readBack('g'); got != "nope DESC" || !rowsInOrder(s, "bob", "carol", "alice") {
		t.Errorf("after text that did not parse: orderBy %q; the sort must stay as it was\n%s", got, s.String())
	}

	s.Keys(t, key(tui.KeyTab), decltest.Rune('s')) // the cursor's field, NAME
	s.WaitFor(t, "sorted by NAME", func(string) bool { return rowsInOrder(s, "alice", "bob", "carol") })
	s.WaitFor(t, "orderByChanged", func(string) bool { return len(changed.all()) == 1 })
	if got := changed.all()[0].Raw; got != "NAME ASC" {
		t.Errorf("orderByChanged(%q), want NAME ASC", got)
	}
	if got := readBack('g'); got != "NAME ASC" {
		t.Errorf("orderBy after s reads %q", got)
	}
	if got := readBack('e'); got != "" {
		t.Errorf("orderByError after s reads %q", got)
	}
}

// rowNumbers numbers the rows; on a TableView without a cellCursor it, orderBy and contextMenu are
// errors.
func TestACellCursorTableViewNumbersItsRows(t *testing.T) {
	s := runCells(t, `TableView { model: App.rows; cellCursor: true; rowNumbers: true }`, scores(), &recorder{})
	s.WaitFor(t, "numbered rows", func(sc string) bool {
		return strings.Contains(sc, "1  carol") && strings.Contains(sc, "3  bob")
	})
	for _, prop := range []string{"rowNumbers: true", `orderBy: "n"`, "contextMenu: true"} {
		err := tuidecl.Check(
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nTableView { model: App.rows; "+prop+" }")),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": scores()}))
		if err == nil || !strings.Contains(err.Error(), "needs cellCursor") {
			t.Errorf("%s on a row-cursor TableView: err %v", prop, err)
		}
	}
}

// The palette's inactive highlight dresses the cell cursor while the table lacks the keys, and the
// highlight while it has them.
func TestACellCursorTableViewBlursItsCursorFromTheInactivePalette(t *testing.T) {
	flat := tuidecl.NewListModel("name")
	flat.Reset([]tuidecl.Row{{"name": "delta"}})
	s := decltest.Run(t, 40, 12,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
 palette.highlight: "#ffffff"; palette.highlightedText: "#000000"
 palette.inactive.highlight: "#333344"; palette.inactive.highlightedText: "#bbbbbb"
 Flex { direction: Tui.Vertical
  TableView { id: grid; model: App.rows; cellCursor: true; focus: true; Layout.fillHeight: true }
  ListView { id: list; model: App.list; textRole: "name"; Layout.fillHeight: true }
 }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": scores(), "App.list": flat}))
	rgb := func(r, g, b uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b} }
	active := func(c tui.CellAttrs) bool {
		return c.FG == rgb(0, 0, 0) && c.BG == rgb(255, 255, 255) && c.Mask&tui.AttrReverse == 0
	}
	blurred := func(c tui.CellAttrs) bool {
		return c.FG == rgb(0xbb, 0xbb, 0xbb) && c.BG == rgb(0x33, 0x33, 0x44) && c.Mask&tui.AttrReverse == 0
	}
	s.WaitFor(t, "the focused cell cursor", func(sc string) bool {
		return strings.Contains(sc, "carol") && active(viewCell(t, s, "carol"))
	})
	s.Program.Post(func() { _ = s.Program.Call("list", "forceActiveFocus") })
	s.WaitFor(t, "the blurred cell cursor", func(string) bool { return blurred(viewCell(t, s, "carol")) })
	s.Program.Post(func() { _ = s.Program.Call("grid", "forceActiveFocus") })
	s.WaitFor(t, "the cell cursor focused again", func(string) bool { return active(viewCell(t, s, "carol")) })
}

// contextMenu turns the right-click menu on; its rows are the host's, through TableOf. The menu
// opens in the Window's overlay.
func TestACellCursorTableViewOpensTheHostsMenu(t *testing.T) {
	ran := make(chan bool, 1)
	s := decltest.RunWith(t, 40, 10, func(p *tuidecl.Program) error {
		c, ok := tuidecl.FindAs[tui.Component](p, "v")
		if !ok || tuidecl.TableOf(c) == nil || tuidecl.TableOf(c).Menu() == nil {
			return errors.New("no menu on the TableView")
		}
		tuidecl.TableOf(c).Menu().SetRows(func() []widget.MenuItemModel {
			return []widget.MenuItemModel{widget.NewCommand("t.copy", "Copy rows", widget.CoreMenuAction{
				ID: "t.copy", Run: func(*widget.EditorCore) { ran <- true }})}
		})
		return nil
	},
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Window { TableView { id: v; model: App.rows; cellCursor: true; contextMenu: true } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": scores()}))
	s.WaitForText(t, "carol")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 2, Y: 2})
	s.WaitForText(t, "Copy rows")
	s.Keys(t, key(tui.KeyEnter))
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the row did not run")
	}
}
