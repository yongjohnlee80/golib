package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// withTable gives the Editor declared with id e a table document view, turned on, as a host
// does (AutoDoc for a CSV note): the switch's reason to be shown.
func withTable(p *tuidecl.Program) error {
	c, _ := p.Find("e")
	ed, ok := tuidecl.EditorOf(c)
	if !ok {
		return nil
	}
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	return nil
}

func switchDoc(body string) tuidecl.ProgramOption {
	return tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n"+body+"\n}"))
}

const sw = widget.ModeSwitchRaw + widget.ModeSwitchSep + widget.ModeSwitchRendered

// An Editor's modeSwitch places the switch: "title" on its Frame's title row (the Frame holds it
// in its title slot, built from the tree as written), true and "row" on the editor's own first
// row, false nowhere. "title" with no Frame round the Editor keeps it on the editor's row, so the
// switch is never lost.
func TestAnEditorsModeSwitchPlacesTheSwitch(t *testing.T) {
	row := func(s *decltest.Screen, y int) string { return strings.Split(s.String(), "\n")[y] }
	for _, c := range []struct {
		name, body string
		onTitle    bool // on the frame's top row (row 0)
		onRow      int  // the row it is on, -1 for none
	}{
		{"title in a Frame", `Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: "title" } }`, true, 0},
		{"row in a Frame", `Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: "row" } }`, false, 1},
		{"true in a Frame", `Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: true } }`, false, 1},
		{"false in a Frame", `Frame { title: "notes.csv"; Editor { id: e; text: "a,b"; modeSwitch: false } }`, false, -1},
		{"title with no Frame", `Editor { id: e; text: "a,b"; modeSwitch: "title" }`, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := decltest.RunWith(t, 50, 6, withTable, switchDoc(c.body))
			if c.onRow < 0 {
				s.WaitForText(t, "a,b")
				if strings.Contains(s.String(), widget.ModeSwitchRaw) {
					t.Fatalf("modeSwitch false: a switch is drawn:\n%s", s)
				}
				return
			}
			s.WaitForText(t, sw)
			got := -1
			for y, r := range strings.Split(s.String(), "\n") {
				if strings.Contains(r, sw) {
					got = y
					break
				}
			}
			if got != c.onRow || strings.Count(s.String(), widget.ModeSwitchRaw) != 1 {
				t.Fatalf("the switch on row %d (want %d), once:\n%s", got, c.onRow, s)
			}
			if c.onTitle && !strings.HasPrefix(row(s, 0), "┌ notes.csv ") {
				t.Errorf("modeSwitch \"title\": row 0 %q; want the frame's title row, its title kept", row(s, 0))
			}
			if c.onTitle && !strings.HasPrefix(row(s, 1), "│a,b") {
				t.Errorf("modeSwitch \"title\": row 1 %q; want the text on the editor's first row", row(s, 1))
			}
		})
	}
}

// modeSwitch takes true, false, "row" or "title"; any other value is refused, naming them.
func TestAnEditorRefusesAModeSwitchItDoesNotHave(t *testing.T) {
	_, err := tuidecl.NewProgram(switchDoc(`Frame { Editor { modeSwitch: "side" } }`))
	if err == nil || !strings.Contains(err.Error(), "modeSwitch") || !strings.Contains(err.Error(), `"title"`) {
		t.Fatalf(`modeSwitch: "side": %v; want it refused, naming the values it takes`, err)
	}
}
