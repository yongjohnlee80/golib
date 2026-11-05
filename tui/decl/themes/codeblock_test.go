package themes_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	"github.com/yongjohnlee80/golib/tui/style"
)

var updateCodeBlock = flag.Bool("update", false, "rewrite testdata/codeblock's goldens")

// codeBlockGolden compares got with testdata/codeblock/<name>.golden (-update writes it).
func codeBlockGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "codeblock", name+".golden")
	if *updateCodeBlock {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test ./tui/decl/themes -run %s -update)", err, t.Name())
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// mustColor is a theme's colour value.
func mustColor(t *testing.T, value string) style.Color {
	t.Helper()
	c, err := style.ParseColor(value)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A code block reads in every theme, through the real themes: its box (the non-text floor, 3:1 on the
// page), its language's name, and its code and « » marks as text (4.5:1). Each part's contrast is
// measured from the cells on screen, one golden table of them all, and a golden of sepia's and dark's
// rows. Mono's are the terminal's own colours (not RGB, nothing to measure): it says so.
func TestACodeBlockReadsInEveryTheme(t *testing.T) {
	html := `<pre><code class=\"language-go\">short\nthis line is a good deal wider than the page it is on</code></pre>`
	var table strings.Builder
	for _, name := range themes.Names() {
		vals, err := themes.Values(name)
		if err != nil {
			t.Fatal(err)
		}
		page := mustColor(t, vals["document.base"])
		s := decltest.Run(t, 30, 8, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport tui.theme."+name+" 1.0\n"+
			"Window { palette.highlight: Theme.document.highlight; palette.base: Theme.document.base; palette.text: Theme.document.text\n"+
			" palette.mid: Theme.app.mid; palette.window: Theme.document.base; palette.windowText: Theme.document.text\n"+
			" HTMLView { id: page; focus: true; html: \""+html+"\" } }")), themes.Offer())
		s.WaitForText(t, "short")
		// The palette reaches the App after its first frame: useApp queues SetTheme, and Run paints once
		// before its loop takes the queue. So read the frame that wears it: the code in the theme's own
		// text colour. A theme whose text is the terminal's own has none to wait for.
		if r, g, b, ok := mustColor(t, vals["document.text"]).RGBValues(); ok {
			want := tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b}
			s.WaitFor(t, name+"'s palette on the code", func(string) bool {
				for _, row := range s.Backend.Snapshot() {
					for _, c := range row {
						if c.Content == "s" {
							return c.Attrs.FG == want
						}
					}
				}
				return false
			})
		}
		rows := s.Backend.Snapshot()
		var text strings.Builder
		for _, row := range rows {
			var line strings.Builder
			for _, c := range row {
				line.WriteString(c.Content)
			}
			fmt.Fprintf(&text, "%q\n", strings.TrimRight(line.String(), " "))
		}
		find := func(content string) (tui.Cell, bool) {
			for _, row := range rows {
				for _, c := range row {
					if c.Content == content {
						return c, true
					}
				}
			}
			return tui.Cell{}, false
		}
		fmt.Fprintf(&table, "%s\n", name)
		for _, part := range []struct {
			what, content string
			least         float64
		}{{"corner", "┌", 3}, {"edge", "─", 3}, {"side", "│", 3}, {"name", "g", 3}, {"code", "s", 4.5}, {"mark", "»", 4.5}} {
			c, ok := find(part.content)
			if !ok {
				t.Fatalf("%s: no %s (%q) on screen:\n%s", name, part.what, part.content, text.String())
			}
			fg := c.Attrs.FG
			if fg.Kind != tui.CellColorRGB {
				fmt.Fprintf(&table, "  %-6s the terminal's own colour\n", part.what)
				continue
			}
			ratio, ok := style.ContrastRatio(style.RGB(fg.R, fg.G, fg.B), page)
			if !ok {
				fmt.Fprintf(&table, "  %-6s %02x%02x%02x, on the terminal's own page\n", part.what, fg.R, fg.G, fg.B)
				continue
			}
			fmt.Fprintf(&table, "  %-6s %02x%02x%02x %.2f:1\n", part.what, fg.R, fg.G, fg.B, ratio)
			if ratio < part.least {
				t.Errorf("%s: the %s reads at %.2f:1 on the page, want %.1f at least", name, part.what, ratio, part.least)
			}
		}
		if name == "sepia" || name == "dark" {
			codeBlockGolden(t, name, text.String())
		}
	}
	codeBlockGolden(t, "contrast", table.String())
}
