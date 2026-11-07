package mermaid

import (
	"context"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
)

// A pie draws in cells: its circle rastered in filled characters, one density a slice, its legend
// beside it — the label, the share, and the value when the header's showData asks — the title
// over all. A pie whose values sum to nothing has no circle: its legend alone.
func TestAPieDrawsInCells(t *testing.T) {
	cases := map[string]string{
		"pie-documented": "pie title NETFLIX\n  \"Time spent looking for movie\" : 70\n  \"Time spent watching it\" : 30\n",
		"pie-showdata":   "pie showData\n  \"a\" : 2.5\n  \"b\" : 7.5\n",
		"pie-five":       "pie\n  \"one\": 1\n  \"two\": 2\n  \"three\": 3\n  \"four\": 4\n  \"five\": 5\n",
		"pie-six":        "pie\n  \"one\": 1\n  \"two\": 2\n  \"three\": 3\n  \"four\": 4\n  \"five\": 5\n  \"six\": 6\n",
		"pie-slivers":    "pie\n  \"big\": 99.6\n  \"sliver a\": 0.2\n  \"sliver b\": 0.2\n",
		"pie-valueless":  "pie title Nothing\n  \"a\" : 0\n  \"b\" : 0\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// A pie is drawn: it is listed in Kinds, and Lay lays it.
func TestAPieIsDrawn(t *testing.T) {
	d, err := pm.Parse("pie\n  \"a\": 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if !Supported(d.Kind()) {
		t.Error("pie is not listed in Kinds")
	}
	if g, err := Lay(context.Background(), d, newCells(1, 1)); err != nil || g.Size.H == 0 {
		t.Errorf("pie: %v, size %+v; want it laid", err, g)
	}
}

// The fills are the circle and nothing else: every cell whose centre lies inside it is filled, and
// no cell outside it is.
func TestPieFillsItsCircleAlone(t *testing.T) {
	g, _ := paint(t, "pie\n  \"a\": 1\n  \"b\": 1\n") // no title: the circle sits at (0, 0)
	for y := range pieDiameter {
		for x := range pieDiameter {
			dx := (float64(x)+0.5)/pieDiameter*2 - 1
			dy := (float64(y)+0.5)/pieDiameter*2 - 1
			if _, filled := g.fills[cell{x, y}]; filled != (dx*dx+dy*dy <= 1) {
				t.Errorf("cell (%d, %d): filled %v, inside the circle %v", x, y, filled, dx*dx+dy*dy <= 1)
			}
		}
	}
}

// Neighbouring slices never share one density, the first against the last included: the top
// cell's own angle belongs to the first slice, the cell just left of it to the last.
func TestPieNeighbouringSlicesDiffer(t *testing.T) {
	g, _ := paint(t, "pie\n  \"a\": 1\n  \"b\": 1\n  \"c\": 1\n  \"d\": 1\n  \"e\": 1\n")
	mid := pieDiameter / 2
	top, leftOfTop := g.fills[cell{mid, 0}], g.fills[cell{mid - 1, 0}]
	if top == "" || leftOfTop == "" || top == leftOfTop {
		t.Errorf("the top cell %q and its left neighbour %q: want two slices apart", top, leftOfTop)
	}
}

// The legend tells every slice: its own glyph, its label and its share a slice, its value too when
// showData asks.
func TestPieLegendTellsEverySlice(t *testing.T) {
	g, _ := paint(t, "pie showData\n  \"a\": 2.5\n  \"b\": 7.5\n")
	if len(g.texts) != 2 {
		t.Fatalf("legend rows %d; want one a slice", len(g.texts))
	}
	if row := g.texts[0].s; !strings.HasPrefix(row, "█ a") || !strings.Contains(row, "25%") || !strings.Contains(row, "2.5") {
		t.Errorf("the first legend row %q: want its glyph, its label, its share, its value", row)
	}
	if row := g.texts[1].s; !strings.HasPrefix(row, "▓ b") || !strings.Contains(row, "75%") || !strings.Contains(row, "7.5") {
		t.Errorf("the second legend row %q: want its own glyph, not the first's", row)
	}
}

// Each legend row is keyed by its slice's own glyph; at five slices, where the cycle wraps, the
// last row's glyph still differs from the first's, and every row carries the glyph sliceChar
// would give it.
func TestPieLegendKeysItsRows(t *testing.T) {
	g, _ := paint(t, "pie\n  \"one\": 1\n  \"two\": 2\n  \"three\": 3\n  \"four\": 4\n  \"five\": 5\n")
	if len(g.texts) != 5 {
		t.Fatalf("legend rows %d; want 5", len(g.texts))
	}
	for i, r := range g.texts {
		if glyph := sliceChar(i, 5); !strings.HasPrefix(r.s, glyph) {
			t.Errorf("legend row %d %q: want its slice's glyph %q first", i, r.s, glyph)
		}
	}
	if g.texts[0].s[:3] == g.texts[4].s[:3] {
		t.Errorf("the first and last legend rows share their glyph: %q and %q", g.texts[0].s, g.texts[4].s)
	}
}

// styleCells records what Paint draws with its style: the golden cells surface, with each cell's
// style kept.
type styleCells struct {
	*cells
	st map[[2]int]style.Style
}

func (s *styleCells) SetCell(x, y int, content string, st style.Style) {
	s.cells.SetCell(x, y, content, st)
	if x >= 0 && x < s.w && y >= 0 && y < s.h {
		s.st[[2]int{x, y}] = st
	}
}

// The fills wear the theme's Box token and the legend its Label token — the token per element,
// whatever a host's palette resolves them to, as the cell page's diagramTheme wears them
// (sepia's accent measures 4.87:1, dark's 9.37:1, the foreground 6.31:1 and 11.05:1).
func TestPieWearsTheThemeTokens(t *testing.T) {
	g, _ := paint(t, "pie\n  \"a\": 1\n  \"b\": 1\n")
	th := Theme{Line: style.New().Foreground(style.TokenBorder), Box: style.New().Foreground(style.TokenAccent),
		Label: style.New().Foreground(style.TokenForeground)}
	s := &styleCells{cells: newCells(g.Size.W, g.Size.H), st: map[[2]int]style.Style{}}
	g.Paint(s, 0, 0, -1, th)
	fills, legend := 0, 0
	for c, st := range s.st {
		want := th.Label // the legend and the title are texts, wherever they sit
		_, onPie := g.fills[cell{c[0], c[1]}]
		if onPie {
			want = th.Box // the circle's fill cells, wherever the circle sits
		}
		if st != want {
			t.Errorf("cell %v wears %v, want the theme's %v token", c, st, want)
			continue
		}
		if onPie {
			fills++
		} else {
			legend++
		}
	}
	if fills == 0 || legend == 0 {
		t.Errorf("fills %d, legend %d; want both drawn", fills, legend)
	}
}

// A slice and its legend swatch wear its series' colour (Theme.Series), slice by slice: every fill
// cell of the circle is its slice's colour, each legend row's swatch its slice's, and the slices'
// colours differ. With more slices than series the colours cycle, the densities still apart.
func TestAPieSliceAndItsSwatchWearItsSeries(t *testing.T) {
	red, green, blue := style.RGB(200, 40, 40), style.RGB(40, 160, 40), style.RGB(40, 40, 200)
	th := Theme{Box: style.New().Foreground(style.TokenAccent), Label: style.New().Foreground(style.TokenForeground),
		Series: []style.ColorSpec{red, green, blue}}
	g, _ := paint(t, "pie\n  \"Dogs\" : 386\n  \"Cats\" : 85\n  \"Rats\" : 15\n")
	s := &styleCells{cells: newCells(g.Size.W, g.Size.H), st: map[[2]int]style.Style{}}
	g.Paint(s, 0, 0, -1, th)
	glyphs := map[string]int{} // a slice's glyph, its index
	for i := range 3 {
		glyphs[sliceChar(i, 3)] = i
	}
	colour := func(c cell) style.Color {
		fg, _ := s.st[[2]int{c.x, c.y}].GetForeground()
		return fg
	}
	want := []style.Color{red, green, blue}
	seen := map[int]int{}
	for c, glyph := range g.fills {
		i, ok := glyphs[glyph]
		if !ok {
			t.Fatalf("cell %v holds %q, no slice's glyph", c, glyph)
		}
		if got := colour(c); got != want[i] {
			t.Errorf("cell %v (slice %d's %q) wears %v, want its series' %v", c, i, glyph, got, want[i])
		}
		seen[i]++
	}
	// the legend's swatches: each row's first cell, its slice's glyph in its slice's colour
	rows := 0
	for _, txt := range g.texts {
		for i, label := range []string{"Dogs", "Cats", "Rats"} {
			if !strings.Contains(txt.s, label) {
				continue
			}
			sw := cell{x: txt.x, y: txt.y}
			if g.fills[sw] != sliceChar(i, 3) || colour(sw) != want[i] {
				t.Errorf("%s's swatch is %q in %v, want %q in its slice's %v", label, g.fills[sw], colour(sw), sliceChar(i, 3), want[i])
			}
			rows++
		}
	}
	if rows != 3 || seen[0] == 0 || seen[1] == 0 || seen[2] == 0 {
		t.Fatalf("legend rows %d, slice cells %v: want three of each", rows, seen)
	}

	// six slices, three colours: neighbours still differ, by colour or by density
	g, _ = paint(t, "pie\n  \"a\": 1\n  \"b\": 1\n  \"c\": 1\n  \"d\": 1\n  \"e\": 1\n  \"f\": 1\n")
	s = &styleCells{cells: newCells(g.Size.W, g.Size.H), st: map[[2]int]style.Style{}}
	g.Paint(s, 0, 0, -1, th)
	for i := range 6 {
		j := (i + 1) % 6
		if want[i%3] == want[j%3] && sliceChar(i, 6) == sliceChar(j, 6) {
			t.Errorf("slices %d and %d share their colour and their density", i, j)
		}
	}
}
