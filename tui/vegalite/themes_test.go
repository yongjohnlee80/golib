package vegalite_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	"github.com/yongjohnlee80/golib/tui/style"
	tuivegalite "github.com/yongjohnlee80/golib/tui/vegalite"
)

var updateThemes = flag.Bool("update-themes", false, "rewrite testdata/themes' goldens")

// themesGolden compares got with testdata/themes/<name>.golden (-update-themes writes it).
func themesGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "themes", name+".golden")
	if *updateThemes {
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
		t.Fatalf("%v (go test ./tui/vegalite -run %s -update-themes)", err, t.Name())
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// painted is a surface that keeps each cell's text and foreground.
type painted struct {
	w, h int
	text [][]string
	fg   [][]style.Color
	set  [][]bool
}

func newPainted(w, h int) *painted {
	p := &painted{w: w, h: h}
	for range h {
		p.text = append(p.text, strings.Split(strings.Repeat(" ", w), ""))
		p.fg = append(p.fg, make([]style.Color, w))
		p.set = append(p.set, make([]bool, w))
	}
	return p
}

func (p *painted) SetCell(x, y int, content string, st style.Style) {
	if x < 0 || y < 0 || x >= p.w || y >= p.h {
		return
	}
	p.text[y][x] = content
	p.fg[y][x], p.set[y][x] = st.GetForeground()
}
func (p *painted) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			p.SetCell(x, y, content, st)
		}
	}
}
func (p *painted) Sub(tui.Rect) tui.Surface { return p }
func (p *painted) Size() tui.Size           { return tui.Size{W: p.w, H: p.h} }
func (p *painted) StringWidth(s string) int { return tui.StringWidth(s) }
func (p *painted) Theme() *style.Theme      { th := style.DefaultTheme(); return &th }
func (p *painted) Caps() tui.Capabilities   { return tui.Capabilities{} }
func (m measure) StringWidth(s string) int  { return tui.StringWidth(s) }

type measure struct{}

// themeOf is a built-in theme's chart theme, as a page builds it: its series from the theme's
// syntax roles (tui/decl's syntax.<role> palette), and its page's base colour.
func themeOf(t *testing.T, name string) (tuivegalite.Theme, style.Color) {
	t.Helper()
	vals, err := themes.Values(name)
	if err != nil {
		t.Fatal(err)
	}
	color := func(v string) (style.Color, bool) {
		c, err := style.ParseColor(v)
		return c, err == nil
	}
	th := tuivegalite.ThemeFromSyntax(func(r highlight.Style) style.Style {
		if c, ok := color(vals["syntax."+r.String()]); ok {
			return style.New().Foreground(c)
		}
		return style.New()
	})
	page, _ := color(vals["document.base"])
	return th, page
}

// tierCharts are a chart each tier draws, each with four series: bars stacked, Braille lines, and
// the table (a bar under a line).
var tierCharts = []struct{ name, src string }{
	{"bars", `{"data":{"values":[{"c":"A","s":"w","v":3},{"c":"A","s":"x","v":2},{"c":"A","s":"y","v":4},{"c":"A","s":"z","v":1},
		{"c":"B","s":"w","v":2},{"c":"B","s":"x","v":5},{"c":"B","s":"y","v":1},{"c":"B","s":"z","v":3}]},
		"mark":"bar","encoding":{"x":{"field":"c","type":"nominal"},"y":{"field":"v","type":"quantitative"},"color":{"field":"s","type":"nominal"}}}`},
	{"braille", `{"data":{"values":[{"t":1,"s":"w","v":3},{"t":2,"s":"w","v":6},{"t":3,"s":"w","v":4},{"t":1,"s":"x","v":8},{"t":2,"s":"x","v":7},{"t":3,"s":"x","v":9},
		{"t":1,"s":"y","v":1},{"t":2,"s":"y","v":2},{"t":3,"s":"y","v":5},{"t":1,"s":"z","v":6},{"t":2,"s":"z","v":3},{"t":3,"s":"z","v":2}]},
		"mark":"line","encoding":{"x":{"field":"t","type":"quantitative"},"y":{"field":"v","type":"quantitative"},"color":{"field":"s","type":"nominal"}}}`},
	{"table", `{"data":{"values":[{"m":"Jan","v":3,"w":1},{"m":"Feb","v":5,"w":2}]},
		"layer":[{"mark":"bar","encoding":{"x":{"field":"m","type":"ordinal"},"y":{"field":"v","type":"quantitative"}}},
		{"mark":"line","encoding":{"x":{"field":"m","type":"ordinal"},"y":{"field":"w","type":"quantitative"}}}]}`},
}

// A chart reads in every theme, its series in the page's code colours: each series' cells, as a
// tier paints them, at 3:1 at least on the page (the non-text floor), one golden table of every
// theme's; and sepia's and dark's drawings of every tier, each with its cells' series. A theme whose
// code colours are the terminal's own (not RGB) has nothing to measure: it says so.
func TestAChartsSeriesReadInEveryTheme(t *testing.T) {
	var table strings.Builder
	for _, name := range themes.Names() {
		th, page := themeOf(t, name)
		fmt.Fprintf(&table, "%s\n", name)
		var golden strings.Builder
		var colours []string
		for i, spec := range th.Series {
			if c, ok := spec.(style.Color); ok {
				if r, g, b, rgb := c.RGBValues(); rgb {
					colours = append(colours, fmt.Sprintf("%d %02x%02x%02x", i, r, g, b))
				}
			}
		}
		if len(colours) == 0 { // no code colours to give: the page's tokens, the terminal's own
			fmt.Fprintf(&table, "  the terminal's own colours (the page's tokens)\n")
		}
		fmt.Fprintf(&golden, "series: %s\n", strings.Join(colours, ", "))
		for _, c := range tierCharts {
			d, err := tuivegalite.Lay(context.Background(), c.src, measure{}, 60)
			if err != nil {
				t.Fatalf("%s %s: %v", name, c.name, err)
			}
			p := newPainted(d.W, d.H)
			d.Paint(p, 0, 0, th)
			// each series' colour as painted: the first cell wearing it
			for i, spec := range th.Series {
				want, ok := spec.(style.Color)
				if !ok || c.name == "table" {
					continue
				}
				found := false
				for y := range p.h {
					for x := range p.w {
						found = found || p.set[y][x] && p.fg[y][x] == want && strings.TrimSpace(p.text[y][x]) != ""
					}
				}
				if !found || i >= 4 {
					continue // a series past the chart's four
				}
				r, g, b, rgb := want.RGBValues()
				ratio, measured := style.ContrastRatio(want, page)
				switch {
				case !rgb:
					fmt.Fprintf(&table, "  %-7s series %d  the terminal's own colour\n", c.name, i)
				case !measured:
					fmt.Fprintf(&table, "  %-7s series %d  %02x%02x%02x, on the terminal's own page\n", c.name, i, r, g, b)
				default:
					fmt.Fprintf(&table, "  %-7s series %d  %02x%02x%02x %.2f:1\n", c.name, i, r, g, b, ratio)
					if ratio < 3 {
						t.Errorf("%s: %s's series %d reads at %.2f:1 on the page, want 3 at least", name, c.name, i, ratio)
					}
				}
			}
			if name != "sepia" && name != "dark" {
				continue
			}
			fmt.Fprintf(&golden, "== %s\n", c.name)
			for y := range p.h {
				fmt.Fprintf(&golden, "%q\n", strings.TrimRight(strings.Join(p.text[y], ""), " "))
			}
			fmt.Fprintf(&golden, "-- series per cell\n")
			for y := range p.h {
				var line strings.Builder
				for x := range p.w {
					mark := "."
					for i, spec := range th.Series {
						if col, ok := spec.(style.Color); ok && p.set[y][x] && p.fg[y][x] == col && strings.TrimSpace(p.text[y][x]) != "" {
							mark = fmt.Sprint(i)
						}
					}
					line.WriteString(mark)
				}
				fmt.Fprintf(&golden, "%s\n", strings.TrimRight(line.String(), "."))
			}
		}
		if name == "sepia" || name == "dark" {
			themesGolden(t, name, golden.String())
		}
	}
	themesGolden(t, "contrast", table.String())
}
