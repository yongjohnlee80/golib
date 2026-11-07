package mermaid_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
	tuivegalite "github.com/yongjohnlee80/golib/tui/vegalite"
)

var updatePieThemes = flag.Bool("update-pie-themes", false, "rewrite testdata/themes' pie goldens")

// piePainted keeps each painted cell's text and foreground.
type piePainted struct {
	w, h int
	text [][]string
	fg   [][]style.Color
	set  [][]bool
}

func newPiePainted(w, h int) *piePainted {
	p := &piePainted{w: w, h: h}
	for range h {
		p.text = append(p.text, strings.Split(strings.Repeat(" ", w), ""))
		p.fg = append(p.fg, make([]style.Color, w))
		p.set = append(p.set, make([]bool, w))
	}
	return p
}

func (p *piePainted) SetCell(x, y int, content string, st style.Style) {
	if x < 0 || y < 0 || x >= p.w || y >= p.h {
		return
	}
	p.text[y][x] = content
	p.fg[y][x], p.set[y][x] = st.GetForeground()
}
func (p *piePainted) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			p.SetCell(x, y, content, st)
		}
	}
}
func (p *piePainted) Sub(tui.Rect) tui.Surface { return p }
func (p *piePainted) Size() tui.Size           { return tui.Size{W: p.w, H: p.h} }
func (p *piePainted) StringWidth(s string) int { return tui.StringWidth(s) }
func (p *piePainted) Theme() *style.Theme      { th := style.DefaultTheme(); return &th }
func (p *piePainted) Caps() tui.Capabilities   { return tui.Capabilities{} }

type pieMeasure struct{}

func (pieMeasure) StringWidth(s string) int { return tui.StringWidth(s) }

// A pie reads in every built-in theme as a page paints it: the diagram theme with the page's chart
// series (tui/vegalite's ThemeFromSyntax over the theme's syntax colours), each slice's colour at
// 3:1 at least on the page and the slices' colours apart, one golden table of every theme's; and
// sepia's and dark's drawings, each cell's series marked. A theme whose code colours are the
// terminal's own has nothing to measure: it says so.
func TestAPiesSlicesReadInEveryTheme(t *testing.T) {
	d, err := pm.Parse("pie title Pets\n  \"Dogs\" : 386\n  \"Cats\" : 85\n  \"Rats\" : 15\n")
	if err != nil {
		t.Fatal(err)
	}
	g, err := tuimermaid.Lay(context.Background(), d, pieMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	var table strings.Builder
	for _, name := range themes.Names() {
		vals, err := themes.Values(name)
		if err != nil {
			t.Fatal(err)
		}
		color := func(v string) (style.Color, bool) {
			c, err := style.ParseColor(v)
			return c, err == nil
		}
		chart := tuivegalite.ThemeFromSyntax(func(r highlight.Style) style.Style {
			if c, ok := color(vals["syntax."+r.String()]); ok {
				return style.New().Foreground(c)
			}
			return style.New()
		})
		page, _ := color(vals["document.base"])
		th := tuimermaid.TokenTheme()
		th.Series = chart.Series
		p := newPiePainted(g.Size.W, g.Size.H)
		g.Paint(p, 0, 0, -1, th)

		fmt.Fprintf(&table, "%s\n", name)
		var slices []style.Color
		for i := range 3 {
			c, ok := th.Series[i%len(th.Series)].(style.Color)
			if !ok {
				fmt.Fprintf(&table, "  slice %d  the terminal's own colour (the page's tokens)\n", i)
				continue
			}
			r, gr, b, rgb := c.RGBValues()
			ratio, measured := style.ContrastRatio(c, page)
			switch {
			case !rgb:
				fmt.Fprintf(&table, "  slice %d  the terminal's own colour\n", i)
			case !measured:
				fmt.Fprintf(&table, "  slice %d  %02x%02x%02x, on the terminal's own page\n", i, r, gr, b)
			default:
				fmt.Fprintf(&table, "  slice %d  %02x%02x%02x %.2f:1\n", i, r, gr, b, ratio)
				if ratio < 3 {
					t.Errorf("%s: slice %d reads at %.2f:1 on the page, want 3 at least", name, i, ratio)
				}
			}
			for _, o := range slices {
				if o == c {
					t.Errorf("%s: slice %d shares a colour with an earlier slice", name, i)
				}
			}
			slices = append(slices, c)
		}
		if name != "sepia" && name != "dark" {
			continue
		}
		var golden strings.Builder
		for y := range p.h {
			fmt.Fprintf(&golden, "%q\n", strings.TrimRight(strings.Join(p.text[y], ""), " "))
		}
		fmt.Fprintf(&golden, "-- the slice each cell's colour is\n")
		for y := range p.h {
			var line strings.Builder
			for x := range p.w {
				mark := "."
				for i, c := range slices {
					if p.set[y][x] && p.fg[y][x] == c && strings.TrimSpace(p.text[y][x]) != "" {
						mark = fmt.Sprint(i)
					}
				}
				line.WriteString(mark)
			}
			fmt.Fprintf(&golden, "%s\n", strings.TrimRight(line.String(), "."))
		}
		pieGolden(t, "pie-"+name, golden.String())
	}
	pieGolden(t, "pie-contrast", table.String())
}

// pieGolden compares got with testdata/themes/<name>.golden (-update-pie-themes writes it).
func pieGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "themes", name+".golden")
	if *updatePieThemes {
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
		t.Fatalf("%v (go test ./tui/mermaid -run %s -update-pie-themes)", err, t.Name())
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}
