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
	"github.com/yongjohnlee80/golib/tui/style"
)

// vocabulary is every key a theme promises, by group: what the package doc
// lists, and what a layout may bind to whichever theme it imports.
var vocabulary = map[string][]string{
	"app": {"window", "windowText", "button", "buttonText", "highlight", "highlightedText",
		"base", "text", "mid", "light", "inactive.highlight", "inactive.highlightedText", "backdrop"},
	"menu":     {"window", "windowText", "highlight", "highlightedText", "accent"},
	"document": {"window", "windowText", "highlight", "highlightedText", "base", "text", "selection", "selectedText", "cursor", "lineNumber"},
	"status":   {"window", "windowText"},
	"syntax": {"keyword", "controlFlow", "dataType", "attribute", "function", "string", "specialChar",
		"decVal", "float", "baseN", "constant", "comment", "alert", "import", "operator"},
}

func TestTheTenThemesAreOffered(t *testing.T) {
	want := []string{"catppuccin_frappe", "dark", "kanagawa", "light", "mono", "nord", "retro", "sepia",
		"shades_of_purple", "tokyo_night"}
	if got := themes.Names(); !slices.Equal(got, want) {
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

// Values: every theme's dotted names are the whole vocabulary, each a colour
// style.ParseColor reads; a nested group keeps its path; an unknown theme is
// refused, naming the ones there are.
func TestValuesAreEveryThemesColoursByName(t *testing.T) {
	for _, name := range themes.Names() {
		v, err := themes.Values(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for group, keys := range vocabulary {
			for _, key := range keys {
				s, ok := v[group+"."+key]
				if !ok {
					t.Errorf("%s: no %s.%s", name, group, key)
					continue
				}
				if _, err := style.ParseColor(s); err != nil {
					t.Errorf("%s: %s.%s = %q: %v", name, group, key, s, err)
				}
			}
		}
	}
	mono, _ := themes.Values("mono")
	if got := mono["app.inactive.highlight"]; got != "brightblack" {
		t.Errorf(`mono app.inactive.highlight = %q, want "brightblack"`, got)
	}
	if _, err := themes.Values("solarized"); err == nil || !strings.Contains(err.Error(), "catppuccin_frappe, dark, kanagawa, light, mono, nord, retro, sepia, shades_of_purple, tokyo_night") {
		t.Errorf(`Values("solarized") error = %v, want the themes named`, err)
	}
}

// MenuLabels lists every theme once, the first five in their order, each access key unique: two
// themes sharing one would make the second unreachable by its letter.
func TestMenuLabelsListEveryThemeWithAUniqueKey(t *testing.T) {
	labels := themes.MenuLabels()
	var names []string
	keys := map[rune]string{}
	for _, l := range labels {
		names = append(names, l.Name)
		i := strings.Index(l.Text, "&")
		if i < 0 || i+1 >= len(l.Text) {
			t.Errorf("%s: %q has no access key", l.Name, l.Text)
			continue
		}
		k := []rune(strings.ToLower(l.Text[i+1:]))[0]
		if prev, dup := keys[k]; dup {
			t.Errorf("%s and %s share the access key %q", prev, l.Name, k)
		}
		keys[k] = l.Name
	}
	if !slices.Equal(names[:5], []string{"dark", "light", "mono", "retro", "sepia"}) {
		t.Errorf("first five = %v", names[:5])
	}
	sorted := slices.Sorted(slices.Values(names))
	if !slices.Equal(sorted, themes.Names()) {
		t.Errorf("MenuLabels names %v, want each of %v once", names, themes.Names())
	}
}
