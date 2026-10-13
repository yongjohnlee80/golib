package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// toolTipText is every widget's: a Text, a Button and a ComboBox take it, a bound one follows its
// source, and a literal one is set.
func TestDeclaredToolTipTextOnEveryWidget(t *testing.T) {
	layout := "import tui 1.0\nimport demo 1.0\nWindow { Flex { direction: Tui.Vertical\n" +
		" Text { id: label; text: \"the prose font\"; toolTipText: App.tip }\n" +
		" Button { id: go; text: \"Go\"; toolTipText: \"runs it\" }\n" +
		" ComboBox { id: fonts; toolTipText: App.tip } } }"
	s := decltest.Run(t, 60, 8, tuidecl.LayoutSource("main.qml", []byte(layout)), tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.tip": "the Rendered view and the HTML preview"}))
	s.WaitForText(t, "the prose font")
	tip := func(id string) (text string) {
		onScreenLoop(t, s, func() {
			c, ok := s.Program.Find(id)
			if !ok {
				t.Errorf("no %s", id)
				return
			}
			text = c.(interface{ ToolTip() string }).ToolTip()
		})
		return text
	}
	if got := tip("label"); got != "the Rendered view and the HTML preview" {
		t.Errorf("the Text's tip: %q", got)
	}
	if got := tip("go"); got != "runs it" {
		t.Errorf("the Button's tip: %q", got)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.tip", "changed"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the bound tips follow", func(string) bool { return tip("label") == "changed" && tip("fonts") == "changed" })
}

// A reload that removes toolTipText leaves the item with no tooltip: the property is RESET in
// place, not rebuilt.
func TestRemovingToolTipTextOnReloadResetsIt(t *testing.T) {
	doc := func(prop string) string {
		return "import tui 1.0\nWindow { Text { id: label; text: \"the prose font\"" + prop + " } }"
	}
	s := decltest.Run(t, 40, 4, tuidecl.LayoutSource("main.qml", []byte(doc("; toolTipText: \"in full\""))))
	s.WaitForText(t, "the prose font")
	tip := func() (text string) {
		onScreenLoop(t, s, func() {
			c, _ := s.Program.Find("label")
			text = c.(interface{ ToolTip() string }).ToolTip()
		})
		return text
	}
	if got := tip(); got != "in full" {
		t.Fatalf("before the reload: %q", got)
	}
	var rebuilt, reset int
	onScreenLoop(t, s, func() {
		res, err := s.Program.Reload([]byte(doc("")))
		if err != nil {
			t.Error(err)
		}
		rebuilt, reset = len(res.Rebuilt), res.Reset
	})
	if got := tip(); got != "" || rebuilt != 0 || reset != 1 {
		t.Errorf("after the reload: tip %q, rebuilt %d, reset %d; want none, reset in place", got, rebuilt, reset)
	}
}

// A type with no place on screen refuses a tooltip by name, and the value must be a string.
func TestAToolTipNeedsAWidgetAndAString(t *testing.T) {
	if _, err := mountDoc(t, "Window { Text { }\n Shortcut { sequence: \"Ctrl+X\"; toolTipText: \"x\" } }"); err == nil || !strings.Contains(err.Error(), "cannot show a tooltip") {
		t.Errorf("a Shortcut: err = %v", err)
	}
	if _, err := mountDoc(t, "Window { Text { toolTipText: 3 } }"); err == nil || !strings.Contains(err.Error(), "toolTipText") {
		t.Errorf("a number: err = %v", err)
	}
}
