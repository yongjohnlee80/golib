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

// A code block reads in a sepia page and a dark one, through the real themes: its box in the theme's
// border, its name and its code as text over the page, its « » marks as text. A golden per theme, its
// rows and each part's colours and contrast with the page; the code and the marks must read.
func TestACodeBlockReadsInEveryTheme(t *testing.T) {
	for _, name := range []string{"sepia", "dark"} {
		t.Run(name, func(t *testing.T) {
			vals, err := themes.Values(name)
			if err != nil {
				t.Fatal(err)
			}
			page, err := style.ParseColor(vals["document.base"])
			if err != nil {
				t.Fatal(err)
			}
			html := `<pre><code class=\"language-go\">short\nthis line is a good deal wider than the page it is on</code></pre>`
			s := decltest.Run(t, 30, 8, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport tui.theme."+name+" 1.0\n"+
				"Window { palette.highlight: Theme.document.highlight; palette.base: Theme.document.base; palette.text: Theme.document.text\n"+
				" palette.mid: Theme.app.mid; palette.window: Theme.document.base; palette.windowText: Theme.document.text\n"+
				" HTMLView { id: page; focus: true; html: \""+html+"\" } }")), themes.Offer())
			s.WaitForText(t, "short")
			rows := s.Backend.Snapshot()
			var b strings.Builder
			for _, row := range rows {
				var line strings.Builder
				for _, c := range row {
					line.WriteString(c.Content)
				}
				fmt.Fprintf(&b, "%q\n", strings.TrimRight(line.String(), " "))
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
			for _, part := range []struct {
				what, content string
				least         float64 // the contrast it must have with the page, 0: as it is ruled, reported
			}{{"corner", "┌", 0}, {"side", "│", 0}, {"name", "g", 3}, {"code", "s", 4.5}, {"mark", "»", 4.5}} {
				c, ok := find(part.content)
				if !ok {
					t.Fatalf("no %s (%q) on screen:\n%s", part.what, part.content, b.String())
				}
				fg := c.Attrs.FG
				if fg.Kind != tui.CellColorRGB {
					t.Fatalf("the %s's colour isn't RGB: %+v", part.what, fg)
				}
				ratio, _ := style.ContrastRatio(style.RGB(fg.R, fg.G, fg.B), page)
				fmt.Fprintf(&b, "%-6s fg %02x%02x%02x, %.2f:1 on the page\n", part.what, fg.R, fg.G, fg.B, ratio)
				if ratio < part.least {
					t.Errorf("the %s reads at %.2f:1 on %s's page, want %.1f at least", part.what, ratio, name, part.least)
				}
			}
			path := filepath.Join("testdata", "codeblock", name+".golden")
			if *updateCodeBlock {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (go test ./tui/decl/themes -run %s -update)", err, t.Name())
			}
			if b.String() != string(want) {
				t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, b.String(), want)
			}
		})
	}
}
