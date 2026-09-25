package decl_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// syntax_test.go holds SyntaxHighlighter to ADR-tui-0015's S5–S8, on the
// screen: the colour of the cell a word is painted in.

func fgOf(t *testing.T, s *decltest.Screen, text string) tui.CellColor {
	t.Helper()
	return cellOf(t, s, text).Attrs.FG
}

func waitFG(t *testing.T, s *decltest.Screen, text string, fg tui.CellColor) {
	t.Helper()
	s.WaitFor(t, text+" in its colour", func(string) bool {
		for _, row := range s.Backend.Snapshot() {
			var line strings.Builder
			cols := []int{}
			for x, c := range row {
				line.WriteString(c.Content)
				for range len(c.Content) {
					cols = append(cols, x)
				}
			}
			if i := strings.Index(line.String(), text); i >= 0 {
				return row[cols[i]].Attrs.FG == fg
			}
		}
		return false
	})
}

var syntaxThemes = fstest.MapFS{
	"themes/dark.qml":  {Data: []byte(`Theme { syntax { keyword: "red"; string: "green" } }`)},
	"themes/light.qml": {Data: []byte(`Theme { syntax { keyword: "blue"; string: "yellow" } }`)},
}

// syntaxDoc sets the syntax roles on the Editor's PARENT: the highlighter
// wears them by inheritance.
func syntaxDoc(theme, definition string) string {
	return "import tui 1.0\nimport demo 1.0\nimport demo.theme." + theme + " 1.0\n" +
		"Flex { syntax.keyword: Theme.syntax.keyword; syntax.string: Theme.syntax.string\n" +
		" Editor { text: \"import Q 1.0\\nText { text: \\\"hi\\\" }\"\n SyntaxHighlighter { definition: " + definition + " } } }"
}

func runSyntax(t *testing.T, src string, extra ...tuidecl.ProgramOption) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 4, append([]tuidecl.ProgramOption{
		tuidecl.LayoutSource("main.qml", []byte(src)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.syntax": "QML"}),
		tuidecl.Themes(syntaxThemes, "themes", "demo.theme", "1.0"),
	}, extra...)...)
}

// S5 — a QML buffer is coloured by the theme's syntax roles, and switching the
// theme import re-colours it.
func TestS5AQMLBufferWearsTheThemesSyntaxColours(t *testing.T) {
	s := runSyntax(t, syntaxDoc("dark", `"QML"`))
	waitFG(t, s, "import", ansi(red))
	waitFG(t, s, `"hi"`, ansi(green))
	if fg := fgOf(t, s, "Text"); fg == ansi(red) || fg == ansi(green) {
		t.Errorf("a type took a keyword or string colour: %+v", fg)
	}
	reloadScreen(t, s, syntaxDoc("light", `"QML"`))
	waitFG(t, s, "import", ansi(blue))
	waitFG(t, s, `"hi"`, ansi(3))
}

// S6 — what is refused, by name.
func TestS6WhatASyntaxHighlighterRefuses(t *testing.T) {
	for doc, want := range map[string]string{
		`Editor { SyntaxHighlighter { definition: "Cobol" } }`:     `"Cobol" is not a registered highlighter; registered: JavaScript, QML`,
		`Flex { SyntaxHighlighter { definition: "QML" } }`:         "the Editor it is declared in, and Flex is not one",
		`SyntaxHighlighter { definition: "QML" }`:                  "the Editor it is declared in, and the root is in none",
		`Editor { Text { } }`:                                      "an Editor holds only a SyntaxHighlighter",
		"Editor { SyntaxHighlighter { }\n SyntaxHighlighter { } }": "one SyntaxHighlighter, got 2",
		`Editor { syntax.keywords: "red" }`:                        "syntax.keywords is not a syntax style; want one of alert, annotation",
		`Flex { syntax.keyword: "rouge" }`:                         "syntax.keyword",
		`Editor { SyntaxHighlighter { theme.keyword: "red" } }`:    "theme.keyword",
	} {
		_, err := mountDoc(t, "import tui 1.0\n"+doc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n err = %v\nwant %q", doc, err, want)
		}
	}
}

// S7 — `definition` bound to a source switches language live; "" is off.
func TestS7TheDefinitionFollowsItsSource(t *testing.T) {
	s := runSyntax(t, syntaxDoc("dark", "App.syntax"))
	waitFG(t, s, "import", ansi(red))
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.syntax", ""); err != nil {
			t.Error(err)
		}
	})
	waitFG(t, s, "import", terminalDefault)
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.syntax", "QML"); err != nil {
			t.Error(err)
		}
	})
	waitFG(t, s, "import", ansi(red))
}

// S8 — a program's own highlighter, registered by name, and nothing else.
func TestS8AProgramsOwnHighlighterNeedsOnlyARegistration(t *testing.T) {
	shout := highlight.HighlighterFunc(func(line string, prev highlight.State) ([]highlight.Span, highlight.State) {
		if i := strings.Index(line, "LOUD"); i >= 0 {
			return []highlight.Span{{Start: i, End: i + 4, Style: highlight.Keyword}}, prev
		}
		return nil, prev
	})
	s := decltest.Run(t, 30, 3,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nEditor { syntax.keyword: \"red\"; text: \"quiet LOUD\"\n SyntaxHighlighter { definition: \"Shout\" } }")),
		tuidecl.Highlighters(highlight.Definition{Name: "Shout", Highlighter: shout}))
	waitFG(t, s, "LOUD", ansi(red))
	if fg := fgOf(t, s, "quiet"); fg != terminalDefault {
		t.Errorf("unhighlighted text took a colour: %+v", fg)
	}
}

// syntax.normal colours the text no span covers, and a style left unset —
// ADR-tui-0015 H2: an unset style paints as Normal.
func TestSyntaxNormalPaintsWhatNoOtherStyleDoes(t *testing.T) {
	s := decltest.Run(t, 30, 3, tuidecl.LayoutSource("main.qml", []byte(
		"import tui 1.0\nEditor { syntax.normal: \"cyan\"; syntax.keyword: \"red\"; text: \"import X 1.0\\n\\\"s\\\"\"\n"+
			" SyntaxHighlighter { definition: \"QML\" } }")))
	waitFG(t, s, "import", ansi(red))
	waitFG(t, s, "X", ansi(cyan))   // Import, unset: as Normal
	waitFG(t, s, `"s"`, ansi(cyan)) // String, unset: as Normal
}

// A reload that removes the syntax roles gives the highlighter the text's
// colours again: the roles are RESET, as palette roles are.
func TestRemovingASyntaxRoleOnReloadResetsIt(t *testing.T) {
	doc := func(role string) string {
		return "import tui 1.0\nFlex { " + role + "\n Editor { text: \"import Q 1.0\"\n SyntaxHighlighter { definition: \"QML\" } } }"
	}
	s := decltest.Run(t, 30, 3, tuidecl.LayoutSource("main.qml", []byte(doc(`syntax.keyword: "red"`))))
	waitFG(t, s, "import", ansi(red))
	reloadScreen(t, s, doc(""))
	waitFG(t, s, "import", terminalDefault)
}
