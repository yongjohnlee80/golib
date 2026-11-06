package vegalite

import (
	"fmt"
	"strings"
	"testing"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
	"github.com/yongjohnlee80/golib/tui"
)

// plotOf is a plot of rect over categories, a band of size cells each, and a measure from lo to hi
// across the rect's length along it (its width for horizontal bars, its height for vertical ones).
func plotOf(rect tui.Rect, cats []string, size int, lo, hi float64, horizontal bool) barPlot {
	length := float64(rect.H)
	if horizontal {
		length = float64(rect.W)
	}
	return barPlot{
		rect: rect,
		band: func(c string) (int, int, bool) {
			for i, x := range cats {
				if x == c {
					return i * size, size, true
				}
			}
			return 0, 0, false
		},
		at: func(v float64) float64 { return (v - lo) / (hi - lo) * length },
	}
}

// pictured is cells as two pictures of rect: the glyphs, and each cell's colour and background, a
// letter a series (a, b, …), '.' the page.
func pictured(rect tui.Rect, cells []barCell) (glyphs, colours string) {
	g, c := make([][]string, rect.H), make([][]string, rect.H)
	for y := range rect.H {
		g[y], c[y] = make([]string, rect.W), make([]string, rect.W)
		for x := range rect.W {
			g[y][x], c[y][x] = " ", "  "
		}
	}
	letter := func(i int) string {
		if i == barPage {
			return "."
		}
		return string(rune('a' + i))
	}
	for _, cl := range cells {
		x, y := cl.x-rect.X, cl.y-rect.Y
		if x < 0 || y < 0 || x >= rect.W || y >= rect.H {
			panic(fmt.Sprintf("a cell outside the plot: %+v", cl))
		}
		g[y][x], c[y][x] = cl.glyph, letter(cl.fg)+letter(cl.bg)
	}
	var gb, cb strings.Builder
	for y := range rect.H {
		gb.WriteString(strings.TrimRight(strings.Join(g[y], ""), " ") + "\n")
		cb.WriteString(strings.TrimRight(strings.Join(c[y], " "), " ") + "\n")
	}
	return gb.String(), cb.String()
}

func across(cats []string, values ...float64) *pv.Layer {
	ly := &pv.Layer{Mark: pv.MarkBar, Horizontal: true}
	for i, v := range values {
		ly.Data = append(ly.Data, pv.Datum{Y: pv.Pos{Str: cats[i]}, X: pv.Pos{Num: v}, V1: v})
	}
	return ly
}

// A horizontal bar runs from zero to its value, its end cut to the nearest eighth: over 0 to 10 in
// 20 cells, 6.3 is 12.6 cells, 101 eighths: twelve whole cells and ▋ (five eighths).
func TestABarEndsAtItsEighth(t *testing.T) {
	cats := []string{"n", "s", "e"}
	rect := tui.Rect{X: 3, Y: 1, W: 20, H: 3}
	got, _ := pictured(rect, barCells(across(cats, 6.3, 10, 0.06), nil, plotOf(rect, cats, 1, 0, 10, true)))
	want := strings.Repeat("█", 12) + "▋\n" + strings.Repeat("█", 20) + "\n" + "▏\n"
	if got != want {
		t.Errorf("bars:\n%s\nwant:\n%s", got, want)
	}
}

// Each bar's length is its value's eighths exactly, for every value of a sweep across the range.
func TestEveryBarIsItsValuesEighths(t *testing.T) {
	rect := tui.Rect{W: 13, H: 1}
	for k := range 105 {
		v := float64(k) / 8 // 0 to 13 cells in eighths
		cells := barCells(across([]string{"x"}, v), nil, plotOf(rect, []string{"x"}, 1, 0, 13, true))
		total := 0
		for _, c := range cells {
			for n, g := range barsAcross {
				if c.glyph == g && c.fg == 0 {
					total += n
				}
			}
		}
		if total != k {
			t.Errorf("a bar of %v cells draws %d eighths; want %d", v, total, k)
		}
	}
}

// A stack piles its series in the chart's order from zero, and where two meet inside a cell the
// lower one's eighths are drawn in its colour over the upper one's: a 3.1 then 2.2 over 0 to 10
// in 20 cells is a's 6.2 cells (six whole and ▎ of a on b) then b to 10.6 (▋ of b on the page).
func TestAStackPilesInSeriesOrder(t *testing.T) {
	rect := tui.Rect{W: 20, H: 1}
	ly := &pv.Layer{Mark: pv.MarkBar, Horizontal: true, Stacked: true, Data: []pv.Datum{
		{Y: pv.Pos{Str: "x"}, Series: "a", V0: 0, V1: 3.1},
		{Y: pv.Pos{Str: "x"}, Series: "b", V0: 3.1, V1: 5.3},
	}}
	glyphs, colours := pictured(rect, barCells(ly, []string{"a", "b"}, plotOf(rect, []string{"x"}, 1, 0, 10, true)))
	if want := "██████▎███▋\n"; glyphs != want {
		t.Errorf("glyphs %q; want %q", glyphs, want)
	}
	if want := "aa aa aa aa aa aa ab bb bb bb b.\n"; colours != want {
		t.Errorf("colours %q; want %q", colours, want)
	}
}

// Vertical bars rise up their band's columns from the plot's bottom, in eighths (▁ … ▇ atop █).
func TestAVerticalBarRisesInEighths(t *testing.T) {
	rect := tui.Rect{W: 6, H: 4}
	ly := &pv.Layer{Mark: pv.MarkBar, Data: []pv.Datum{
		{X: pv.Pos{Str: "p"}, V1: 2.5}, // 2.5 cells: two rows and ▄
		{X: pv.Pos{Str: "q"}, V1: 4},
	}}
	got, _ := pictured(rect, barCells(ly, nil, plotOf(rect, []string{"p", "q"}, 3, 0, 4, false)))
	want := "   ██\n▄▄ ██\n██ ██\n██ ██\n"
	if got != want {
		t.Errorf("bars:\n%s\nwant:\n%s", got, want)
	}
}

// Series not stacked overlap on their band, as Vega-Lite overlays them: the longest drawn first and
// the shorter in front, so each shows its own length: b's 1 in front of a's 2, whatever the data's
// order.
func TestSeriesNotStackedOverlapShorterInFront(t *testing.T) {
	rect := tui.Rect{W: 4, H: 2}
	ly := &pv.Layer{Mark: pv.MarkBar, Data: []pv.Datum{
		{X: pv.Pos{Str: "p"}, Series: "b", V1: 1},
		{X: pv.Pos{Str: "p"}, Series: "a", V1: 2},
	}}
	_, colours := pictured(rect, barCells(ly, []string{"a", "b"}, plotOf(rect, []string{"p"}, 4, 0, 2, false)))
	if want := "aa aa aa\nbb bb bb\n"; colours != want {
		t.Errorf("colours %q; want %q", colours, want)
	}
}

// A negative value runs back from zero: over −5 to 5 in 20 cells, −2.5 fills the five cells left of
// zero's column, 1.25 the two and a half after it (▌). One that starts inside a cell, −2.25, draws
// that cell's first half as the page over the bar (▌ in the page's colour on the bar's).
func TestANegativeBarRunsBackFromZero(t *testing.T) {
	rect := tui.Rect{W: 20, H: 3}
	ly := &pv.Layer{Mark: pv.MarkBar, Horizontal: true, Data: []pv.Datum{
		{Y: pv.Pos{Str: "m"}, V0: 0, V1: -2.5},
		{Y: pv.Pos{Str: "p"}, V0: 0, V1: 1.25},
		{Y: pv.Pos{Str: "h"}, V0: 0, V1: -2.25},
	}}
	got, colours := pictured(rect, barCells(ly, nil, plotOf(rect, []string{"m", "p", "h"}, 1, -5, 5, true)))
	want := "     █████\n          ██▌\n     ▌████\n"
	if got != want {
		t.Errorf("bars:\n%s\nwant:\n%s", got, want)
	}
	if row := strings.Split(colours, "\n")[2]; !strings.HasSuffix(row, ".a aa aa aa aa") {
		t.Errorf("the half cell's colours: %q; want the page over the bar (.a)", row)
	}
}

// Horizontal bars carry their values on the right, on each band's first row: a bar's own, a stack's
// total, overlaid bars' each in series order; as many decimals as a value has (up to two), thousands
// grouped, a minus sign.
func TestBarsCarryTheirValues(t *testing.T) {
	band := plotOf(tui.Rect{W: 20, H: 4}, []string{"x", "y"}, 2, 0, 10, true).band
	plain := across([]string{"x", "y"}, 6.25, 1234)
	if got := fmt.Sprint(barValues(plain, band)); got != "[{0 6.25} {2 1,234}]" {
		t.Errorf("plain: %s", got)
	}
	stack := &pv.Layer{Mark: pv.MarkBar, Horizontal: true, Stacked: true, Data: []pv.Datum{
		{Y: pv.Pos{Str: "x"}, Series: "a", V0: 0, V1: 3.1}, {Y: pv.Pos{Str: "x"}, Series: "b", V0: 3.1, V1: 5.3},
		{Y: pv.Pos{Str: "y"}, Series: "a", V0: 0, V1: -2}, {Y: pv.Pos{Str: "y"}, Series: "b", V0: -2, V1: -2.5},
	}}
	if got := fmt.Sprint(barValues(stack, band)); got != "[{0 5.3} {2 −2.5}]" {
		t.Errorf("stacked: %s", got)
	}
	over := &pv.Layer{Mark: pv.MarkBar, Horizontal: true, Data: []pv.Datum{
		{Y: pv.Pos{Str: "x"}, Series: "a", V1: 1}, {Y: pv.Pos{Str: "x"}, Series: "b", V1: 2.5},
	}}
	if got := fmt.Sprint(barValues(over, band)); got != "[{0 1 / 2.5}]" {
		t.Errorf("overlaid: %s", got)
	}
	stack.Normalized = true
	if got := barValues(stack, band); got != nil {
		t.Errorf("normalized: %v; want none", got)
	}
}
