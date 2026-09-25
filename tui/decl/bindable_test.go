package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// bindable_test.go: the properties Qt binds and golib took only at
// construction — Frame.title, Dialog.title, Dialog.helpText, Editor.text,
// Split.ratio — follow their sources.

func setSource(t *testing.T, s *decltest.Screen, name string, v any) {
	t.Helper()
	onScreenLoop(t, s, func() {
		if err := s.Program.Set(name, v); err != nil {
			t.Error(err)
		}
	})
}

func TestFrameTitleAndEditorTextFollowTheirSources(t *testing.T) {
	s := decltest.Run(t, 40, 5,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Frame { title: App.title\n Editor { text: App.body } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.title": "first", "App.body": "alpha"}))
	s.WaitFor(t, "the first title and text", func(sc string) bool {
		return strings.Contains(sc, "first") && strings.Contains(sc, "alpha")
	})
	setSource(t, s, "App.title", "second")
	setSource(t, s, "App.body", "beta")
	s.WaitFor(t, "both followed", func(sc string) bool {
		return strings.Contains(sc, "second") && strings.Contains(sc, "beta") && !strings.Contains(sc, "alpha")
	})
}

func TestDialogTitleAndHelpFollowTheirSources(t *testing.T) {
	s := decltest.Run(t, 50, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Window {\n Text { text: \"under\" }\n Dialog { id: d; title: App.title; helpText: App.help\n  Text { text: \"body\" } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.title": "Before", "App.help": ""}))
	s.WaitForText(t, "under")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "┌ Before ")
	setSource(t, s, "App.title", "After")
	setSource(t, s, "App.help", "Esc closes")
	s.WaitFor(t, "the title and help followed", func(sc string) bool {
		return strings.Contains(sc, "┌ After ") && strings.Contains(sc, "Esc closes")
	})
	rows := strings.Split(s.String(), "\n")
	help := rowOfText(s, "Esc closes")
	if help < 2 || !strings.Contains(rows[help-2], "├") {
		t.Errorf("a help line set later has no rule above it:\n%s", s.String())
	}
}

func TestSplitRatioFollowsItsSource(t *testing.T) {
	s := decltest.Run(t, 40, 3,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Split { orientation: Tui.Horizontal; ratio: App.ratio\n Text { text: \"L\" }\n Text { text: \"R\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.ratio": 0.25}))
	col := func() int { return strings.Index(strings.Split(s.String(), "\n")[0], "R") }
	s.WaitFor(t, "R a quarter across", func(string) bool { c := col(); return c > 5 && c < 15 })
	setSource(t, s, "App.ratio", 0.75)
	s.WaitFor(t, "R three quarters across", func(string) bool { return col() > 25 })
}
