package themes_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
)

// vocabulary is every key a theme promises, by group: what the package doc
// lists, and what a layout may bind to whichever theme it imports.
var vocabulary = map[string][]string{
	"app": {"window", "windowText", "button", "buttonText", "highlight", "highlightedText",
		"base", "text", "mid", "light", "inactive.highlight", "inactive.highlightedText"},
	"menu":     {"window", "windowText", "highlight", "highlightedText", "accent"},
	"document": {"window", "windowText", "highlight", "highlightedText", "base", "text", "selection", "selectedText", "cursor", "lineNumber"},
	"status":   {"window", "windowText"},
	"syntax": {"keyword", "controlFlow", "dataType", "attribute", "function", "string", "specialChar",
		"decVal", "float", "baseN", "constant", "comment", "alert", "import", "operator"},
}

func TestTheFourThemesAreOffered(t *testing.T) {
	if got, want := themes.Names(), []string{"dark", "light", "mono", "retro"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

// Every theme defines the whole vocabulary: a layout that binds one key of
// every group, under each theme's import, builds — a theme missing a key would
// refuse it.
func TestEveryThemeDefinesTheWholeVocabulary(t *testing.T) {
	var binds strings.Builder
	for _, group := range []string{"app", "menu", "document", "status", "syntax"} {
		for _, key := range vocabulary[group] {
			fmt.Fprintf(&binds, " Text { text: Theme.%s.%s }\n", group, key)
		}
	}
	for _, name := range themes.Names() {
		t.Run(name, func(t *testing.T) {
			src := "import tui 1.0\nimport tui.theme." + name + " 1.0\nFlex { direction: Tui.Vertical\n" + binds.String() + "}"
			if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src)), themes.Offer()); err != nil {
				t.Fatalf("a layout binding the vocabulary does not build under %s: %v", name, err)
			}
		})
	}
}

// The test above must be able to fail: a key no theme has is refused.
func TestAKeyOutsideTheVocabularyIsRefused(t *testing.T) {
	src := "import tui 1.0\nimport tui.theme.dark 1.0\nText { text: Theme.app.noSuchKey }"
	if err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte(src)), themes.Offer()); err == nil {
		t.Fatal("a binding to Theme.app.noSuchKey built")
	}
}

// A theme's colours reach the screen: the window's palette, bound to the
// theme, paints the text.
func TestAThemeColoursTheScreen(t *testing.T) {
	s := decltest.Run(t, 20, 2,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport tui.theme.retro 1.0\n"+
			"Window { palette.window: Theme.app.window; palette.windowText: Theme.app.windowText\n Text { text: \"hello\" } }")),
		themes.Offer())
	s.WaitForText(t, "hello")
	for _, row := range s.Backend.Snapshot() {
		for _, c := range row {
			if c.Content == "h" {
				grey := tui.CellColor{Kind: tui.CellColorRGB, R: 0xaa, G: 0xaa, B: 0xaa}
				black := tui.CellColor{Kind: tui.CellColorRGB}
				if c.Attrs.BG != grey || c.Attrs.FG != black {
					t.Errorf("retro's app window/windowText did not paint the text: bg %+v fg %+v, want %+v on %+v",
						c.Attrs.BG, c.Attrs.FG, black, grey)
				}
				return
			}
		}
	}
	t.Fatal("no cell holds the text")
}

// TestTheMarkdownHighlighterPaintsOnlyThemedStyles: every style the Markdown
// highlighter gives a span is one the themes colour; any other would paint as
// the text, and the construct it marks would not show.
func TestTheMarkdownHighlighterPaintsOnlyThemedStyles(t *testing.T) {
	themed := map[string]bool{}
	for _, k := range vocabulary["syntax"] {
		themed[k] = true
	}
	doc := []string{"---", "title: t", "---", "# H `c`", "**b** *e* ~~s~~ [l](u) <http://a> [[w]] ![[e]] #t <b>x</b>",
		"- [ ] task", "1. item", "> [!note] n", "> [!warning] w", "> q", "```", "code", "```", "<!-- c", "c -->", "---", "[r]: u",
		"<div>", "~~~ x", "y", "~~~", "* * *"}
	h := markdown.Highlighter()
	st := highlight.State(0)
	for _, line := range doc {
		spans, next := h.HighlightBlock(line, st)
		for _, s := range spans {
			if !themed[s.Style.String()] {
				t.Errorf("%q: %q is %s, which no theme colours", line, line[s.Start:s.End], s.Style)
			}
		}
		st = next
	}
}
