package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

const checkBoxDoc = "import tui 1.0\nimport demo 1.0\nWindow { palette.window: \"blue\"; palette.windowText: \"white\"\n" +
	" Flex { direction: Tui.Horizontal\n" +
	"  CheckBox { text: \"&Lexical\"; checked: App.lexical; onToggled: App.log(\"toggled\"); onClicked: App.log(\"clicked\") }\n" +
	"  CheckBox { text: \"Rerank\" } } }"

func runCheckBoxes(t *testing.T) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 50, 4,
		tuidecl.LayoutSource("main.qml", []byte(checkBoxDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.lexical": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "Lexical")
	return s, rec
}

// A CheckBox draws "[x] text" or "[ ] text" and nothing more: no button's brackets around it.
func TestACheckBoxIsItsBoxAndItsText(t *testing.T) {
	s, _ := runCheckBoxes(t)
	s.WaitForText(t, "[x] Lexical")
	s.WaitForText(t, "[ ] Rerank")
	if strings.Contains(s.String(), "[ [") || strings.Contains(s.String(), "] ]") {
		t.Fatalf("a CheckBox is wrapped in a button's brackets:\n%s", s)
	}
}

// A CheckBox paints on its surroundings' colours (palette.window, windowText), as a Text does,
// not on a button's surface.
func TestACheckBoxWearsItsSurroundingsColours(t *testing.T) {
	s, _ := runCheckBoxes(t)
	waitBG(t, s, "] Rerank", ansi(blue))
	if fg := cellOf(t, s, "Rerank").Attrs.FG; fg != ansi(white) {
		t.Errorf("the text is %+v, want windowText (white)", fg)
	}
}

// Space toggles the focused box, as a click does, and raises toggled then clicked; checked set by
// the host draws the box again; its mnemonic is the text's.
func TestACheckBoxToggles(t *testing.T) {
	s, rec := runCheckBoxes(t)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: ' '})
	s.WaitForText(t, "[ ] Lexical")
	s.WaitFor(t, "toggled then clicked", func(string) bool {
		got := rec.all()
		return len(got) == 2 && got[0].Raw == "toggled" && got[1].Raw == "clicked"
	})
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.lexical", false); err != nil {
			t.Error(err)
		}
		if err := s.Program.Set("App.lexical", true); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "[x] Lexical")
	if strings.Contains(s.String(), "&Lexical") {
		t.Fatalf("the mnemonic's & is drawn:\n%s", s)
	}
}
