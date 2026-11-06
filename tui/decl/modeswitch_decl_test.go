package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// withTable gives the Editors declared with ids e and f a table document view, turned on, as a host
// does (AutoDoc for a CSV note): the switch's reason to be shown.
func withTable(p *tuidecl.Program) error {
	for _, id := range []string{"e", "f"} {
		c, _ := p.Find(id)
		ed, ok := tuidecl.EditorOf(c)
		if !ok {
			continue
		}
		ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
		ed.SetRenderedDocument(true)
	}
	return nil
}

// switchRows are the screen's rows that draw a switch, top first.
func switchRows(s *decltest.Screen) []int {
	var rows []int
	for y, r := range strings.Split(s.String(), "\n") {
		if strings.Contains(r, widget.ModeSwitchRaw) {
			rows = append(rows, y)
		}
	}
	return rows
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

// A Frame holds the switch of the one Editor in it however deep it sits, as AutoDoc's page has it: a
// Frame, a Flex in it, a line of text, then a FocusScope with the Editor. The switch is drawn on the
// frame's title row and nowhere else: not on the Editor's own first row, which holds its text.
func TestAFrameHoldsTheSwitchOfItsEditorBelowAFlex(t *testing.T) {
	s := decltest.RunWith(t, 50, 8, withTable, switchDoc(`Frame {
    title: "notes.csv"
    Flex {
        direction: Tui.Vertical
        Text { text: "diagnostics" }
        FocusScope {
            Layout.fillHeight: true
            Editor { id: e; text: "a,b"; modeSwitch: "title" }
        }
    }
}`))
	s.WaitForText(t, sw)
	rows := strings.Split(s.String(), "\n")
	if got := switchRows(s); len(got) != 1 || got[0] != 0 || !strings.HasPrefix(rows[0], "┌ notes.csv ") {
		t.Fatalf("the switch on rows %v; want the frame's title row (0) alone:\n%s", got, s)
	}
	textRow := -1
	for y, r := range rows {
		if strings.Contains(r, "a,b") {
			textRow = y
		}
	}
	if textRow < 0 || strings.Contains(rows[textRow], widget.ModeSwitchRaw) {
		t.Errorf("the Editor's first row %d holds its text, no switch:\n%s", textRow, s)
	}
}

// Two Editors in one Frame: which one's switch the title row would show is a guess, so neither is
// wired, and each draws its own on its first row, the title row none.
func TestAFrameWithTwoEditorsHoldsNeithersSwitch(t *testing.T) {
	s := decltest.RunWith(t, 50, 10, withTable, switchDoc(`Frame {
    title: "two"
    Flex {
        direction: Tui.Vertical
        Editor { id: e; text: "a,b"; modeSwitch: "title"; Layout.fillHeight: true }
        Editor { id: f; text: "c,d"; modeSwitch: "title"; Layout.fillHeight: true }
    }
}`))
	s.WaitFor(t, "both Editors' switches", func(string) bool { return len(switchRows(s)) == 2 })
	if got := switchRows(s); got[0] == 0 {
		t.Errorf("a switch on the frame's title row with two Editors in it (rows %v):\n%s", got, s)
	}
}

// A Frame inside a Frame: the inner one holds its Editor's switch, on its own title row; the outer
// one's title row has none.
func TestANestedFrameHoldsItsOwnEditorsSwitch(t *testing.T) {
	s := decltest.RunWith(t, 50, 10, withTable, switchDoc(`Frame {
    title: "outer"
    Flex {
        direction: Tui.Vertical
        Text { text: "above" }
        Frame {
            title: "inner"
            Layout.fillHeight: true
            Editor { id: e; text: "a,b"; modeSwitch: "title" }
        }
    }
}`))
	s.WaitForText(t, sw)
	rows := strings.Split(s.String(), "\n")
	got := switchRows(s)
	if len(got) != 1 || !strings.Contains(rows[got[0]], "inner") || strings.Contains(rows[0], widget.ModeSwitchRaw) {
		t.Fatalf("the switch on rows %v; want the inner frame's title row alone:\n%s", got, s)
	}
}

// modeSwitch set later moves the switch: from the title row to the Editor's first row, and back.
func TestAModeSwitchSetLaterMovesTheSwitch(t *testing.T) {
	s := decltest.RunWith(t, 50, 8, withTable, switchDoc(`Frame {
    title: "notes.csv"
    Flex {
        direction: Tui.Vertical
        FocusScope { Layout.fillHeight: true; Editor { id: e; text: "a,b"; modeSwitch: "title" } }
    }
}`))
	s.WaitForText(t, sw)
	set := func(v string) {
		t.Helper()
		ch := make(chan error, 1)
		s.Program.Post(func() {
			id, ok := s.Program.Tree().NodeByID("e")
			if !ok {
				ch <- errors.New("no node e")
				return
			}
			ch <- s.Program.Tree().SetProp(id, "modeSwitch", qml.SpecValue{Kind: qml.SpecValueString, Raw: v})
		})
		if err := <-ch; err != nil {
			t.Fatalf("modeSwitch %q: %v", v, err)
		}
	}
	set("row")
	s.WaitFor(t, "the switch on the Editor's row", func(string) bool {
		r := switchRows(s)
		return len(r) == 1 && r[0] != 0
	})
	set("title")
	s.WaitFor(t, "the switch back on the title row", func(string) bool {
		r := switchRows(s)
		return len(r) == 1 && r[0] == 0
	})
}
