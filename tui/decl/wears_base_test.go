package decl_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// wears_base_test.go: a view that shows text on its base — a list, a tree, a
// table, a text area, an editor, a preview — wears that base over its whole
// area, the rows past its items and the cells past each row's text too, as
// Qt's item views and text edits fill with Base. Text on bands of base over
// the window's colour is the defect this sweep looks for.

// base, window and highlight are three colours no cell mixes up.
var wearsPalette = `palette.window: "red"; palette.base: "blue"; palette.text: "white"; palette.highlight: "green"; palette.highlightedText: "black"; palette.button: "magenta"; palette.buttonText: "white"; `

func wearsModels() map[string]any {
	list := tuidecl.NewListModel("key", "label")
	list.Reset([]tuidecl.Row{{"key": "a", "label": "alpha"}, {"key": "b", "label": "beta"}})
	tree := tuidecl.NewTreeListModel("key", "label")
	tree.SetChildren(nil, []tuidecl.TreeRow{{Row: tuidecl.Row{"key": "a", "label": "alpha"}}, {Row: tuidecl.Row{"key": "b", "label": "beta"}}})
	return map[string]any{"App.list": list, "App.tree": tree}
}

// TestEveryTextViewWearsItsBaseOverItsWholeArea: each view alone in a Window;
// every cell but the cursor's row wears the base its text wears.
func TestEveryTextViewWearsItsBaseOverItsWholeArea(t *testing.T) {
	for name, view := range map[string]string{
		"ListView":  `ListView { model: App.list; textRole: "label" }`,
		"TreeView":  `TreeView { model: App.tree; textRole: "label" }`,
		"TableView": `TableView { model: App.list; TableViewColumn { role: "label"; title: "NAME"; width: 0 } }`,
		"Editor":    `Editor { text: "alpha\nbeta" }`,
	} {
		t.Run(name, func(t *testing.T) {
			s := decltest.Run(t, 30, 6,
				tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow { "+wearsPalette+view+" }")),
				tuidecl.Singleton("demo", "1.0", "App"),
				tuidecl.Sources(wearsModels()))
			s.WaitForText(t, "beta")
			seen := map[tui.CellColor][]string{}
			for y, row := range s.Backend.Snapshot() {
				for x, c := range row {
					if c.Attrs.BG == ansi(green) || c.Attrs.FG == ansi(green) {
						continue // the cursor's row
					}
					if len(seen[c.Attrs.BG]) < 3 {
						seen[c.Attrs.BG] = append(seen[c.Attrs.BG], fmt.Sprintf("%d,%d%q", x, y, c.Content))
					}
				}
			}
			if _, onBase := seen[ansi(blue)]; len(seen) != 1 || !onBase {
				var parts []string
				for bg, at := range seen {
					parts = append(parts, fmt.Sprintf("%+v at %s", bg, strings.Join(at, " ")))
				}
				t.Fatalf("%s wears %d backgrounds, want its base (blue) alone: %s\n%s", name, len(seen), strings.Join(parts, "; "), s)
			}
		})
	}
}

// TestEveryFieldWearsItsLookAcrossItsRow: text fields fill the row; combo
// boxes wear their look only across the compact control, including its arrow.
func TestEveryFieldWearsItsLookAcrossItsRow(t *testing.T) {
	for name, c := range map[string]struct {
		qml, text string
		role      tui.CellColor // Qt's: a combo box on Button, a text field on Base
	}{
		"ComboBox": {`Flex { direction: Tui.Vertical
 TextField { text: "first" }
 ComboBox { model: App.list; textRole: "label"; currentIndex: 0 } }`, "alpha", ansi(5)}, // magenta: the Button role
		"TextField": {`Flex { direction: Tui.Vertical
 ComboBox { model: App.list; textRole: "label"; currentIndex: 0 }
 TextField { text: "gamma" } }`, "gamma", ansi(blue)},
	} {
		field := c.qml
		t.Run(name, func(t *testing.T) {
			s := decltest.Run(t, 30, 6,
				tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow { "+wearsPalette+field+" }")),
				tuidecl.Singleton("demo", "1.0", "App"),
				tuidecl.Sources(wearsModels()),
				tuidecl.Types(controls.Types()...))
			s.WaitForText(t, c.text)
			// the field under test is the second: not focused
			y := rowOfText(s, c.text)
			if y < 0 {
				t.Fatalf("no %s:\n%s", c.text, s)
			}
			row := s.Backend.Snapshot()[y]
			want := c.role
			fieldWidth := len(row)
			if name == "ComboBox" {
				fieldWidth = 8
			}
			for x, c := range row[:fieldWidth] {
				if c.Attrs.BG != want {
					t.Fatalf("%s's row: cell %d wears %+v, its text %+v:\n%s", name, x, c.Attrs.BG, want, s)
				}
			}
			if name == "ComboBox" && row[fieldWidth].Attrs.BG == want {
				t.Fatalf("ComboBox background extends beyond the compact field:\n%s", s)
			}
		})
	}
}
