package mermaid_test

import (
	"context"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
)

// fmTitle is a front matter block titled Weekly plan, with a config block passed over.
const fmTitle = "---\ntitle: Weekly plan\nconfig:\n  theme: forest\n---\n"

// painted lays src out and paints it with the token theme.
func painted(t *testing.T, src string) (*piePainted, *tuimermaid.Grid) {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatalf("%.30q: %v", src, err)
	}
	g, err := tuimermaid.Lay(context.Background(), d, pieMeasure{})
	if err != nil {
		t.Fatalf("%.30q: %v", src, err)
	}
	p := newPiePainted(g.Size.W, g.Size.H)
	g.Paint(p, 0, 0, -1, tuimermaid.TokenTheme())
	return p, g
}

func row(p *piePainted, y int) string { return strings.TrimRight(strings.Join(p.text[y], ""), " ") }

// A kind that draws no title of its own (a flowchart, a sankey) draws its front matter's title on
// its top row in the label's colour, a blank row under it, and the diagram as it is drawn without
// one, two rows down; a kind that draws its own (a pie) draws the front matter's title once.
// The label's colour (the page's text) reads on the page in sepia and dark.
func TestAFrontMatterTitleIsDrawnAbove(t *testing.T) {
	label, _ := tuimermaid.TokenTheme().Label.GetForeground()
	for name, body := range map[string]string{
		"flowchart": "flowchart LR\n  A[Start] --> B[Finish]",
		"sankey":    "sankey-beta\n\nWater,Tea,10\nWater,Coffee,4\n",
	} {
		p, g := painted(t, fmTitle+body)
		plain, g0 := painted(t, body)
		if !strings.HasPrefix(row(p, 0), "Weekly plan") || row(p, 1) != "" {
			t.Errorf("%s: rows 0 and 1 %q, %q; want the title, then a blank row", name, row(p, 0), row(p, 1))
		}
		if fg, _ := p.fg[0][0], p.set[0][0]; fg != label {
			t.Errorf("%s: the title's colour %v; want the label's %v", name, fg, label)
		}
		if g.Size.H != g0.Size.H+2 {
			t.Errorf("%s: %d rows; want the untitled %d and two", name, g.Size.H, g0.Size.H)
		}
		for y := range g0.Size.H {
			if got, want := strings.TrimRight(strings.Join(p.text[y+2][:g0.Size.W], ""), " "), row(plain, y); got != want {
				t.Fatalf("%s: row %d under the title %q; want the untitled drawing's row %d %q", name, y+2, got, y, want)
			}
		}
		t.Logf("%s:\n%s", name, strings.Join(func() []string {
			var out []string
			for y := range p.h {
				out = append(out, row(p, y))
			}
			return out
		}(), "\n"))
	}
	p, _ := painted(t, fmTitle+"pie\n  \"Dogs\" : 3\n  \"Cats\" : 2")
	n := 0
	for y := range p.h {
		n += strings.Count(row(p, y), "Weekly plan")
	}
	if n != 1 {
		t.Errorf("a pie with a front matter title draws it %d times; want once, its own", n)
	}
	for _, theme := range []string{"sepia", "dark"} {
		vals, err := themes.Values(theme)
		if err != nil {
			t.Fatal(err)
		}
		text, e1 := style.ParseColor(vals["document.text"])
		page, e2 := style.ParseColor(vals["document.base"])
		if e1 != nil || e2 != nil {
			t.Fatalf("%s: the page's colours: %v, %v", theme, e1, e2)
		}
		if r, ok := style.ContrastRatio(text, page); !ok || r < 4.5 {
			t.Errorf("%s: the title's colour reads at %.2f:1 on the page; want 4.5 at least", theme, r)
		}
	}
}
