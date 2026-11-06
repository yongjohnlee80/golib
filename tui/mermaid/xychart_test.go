package mermaid

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// xyCorpus reads a chart of parse/mermaid's XY chart corpus: Mermaid's documented forms.
func xyCorpus(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "parse", "mermaid", "testdata", "xychart", name+".mmd"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// XY charts drawn in cells, each a golden: the documented chart (a bar and a line over twelve
// months), one of named series over a numeric x range with negative values (its key, its range from
// the data), a narrow one of three points, and a wide one of twenty.
func TestAnXYChartDrawsInCells(t *testing.T) {
	wide := "xychart-beta\n  x-axis [a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15, a16, a17, a18, a19, a20]\n" +
		"  bar [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0]\n"
	for name, src := range map[string]string{
		"xychart-documented": xyCorpus(t, "documented"),
		"xychart-ranged":     xyCorpus(t, "ranged"),
		"xychart-narrow":     "xychart-beta\n  title Three\n  x-axis [x, y, z]\n  bar [3, 1, 2]\n",
		"xychart-wide":       wide,
	} {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// Each point of a series stands over its own category: in the documented chart July's value is the
// range's top, so its bar fills the plot's first row and the line's mark is on it, in the column of
// July's label. No other column has a whole cell there (June's 10500 ends in an eighth on that row,
// as it should: ▁, not █ or a mark).
func TestAnXYPointStandsOverItsCategory(t *testing.T) {
	d, err := pm.Parse(xyCorpus(t, "documented"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := LayXYChart(context.Background(), d.(*pm.XYChartDiagram), newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	var jul text
	for _, tx := range g.texts {
		if tx.s == "jul" {
			jul = tx
		}
	}
	if jul.s == "" {
		t.Fatal("no jul label")
	}
	topRow := -1 << 30
	for c := range g.fills {
		if topRow == -1<<30 || c.y < topRow {
			topRow = c.y
		}
	}
	whole := map[string]bool{"█": true}
	for _, gl := range xyLineGlyphs {
		whole[gl] = true
	}
	var cols []int
	for c, f := range g.fills {
		if c.y == topRow && whole[f] {
			cols = append(cols, c.x)
			if c.x < jul.x-1 || c.x > jul.x+jul.w {
				t.Errorf("a %q reaches the top row at column %d, not over jul (columns %d to %d)", f, c.x, jul.x, jul.x+jul.w-1)
			}
		}
	}
	if len(cols) == 0 {
		t.Fatal("no whole cell on the plot's top row")
	}
}

// A horizontal chart is drawn: Drawn accepts it, and its goldens, the corpus's and one of two bar
// series and a line, read as the vertical ones turned (categories down the left, values along the
// bottom).
func TestAHorizontalXYChartDrawsInCells(t *testing.T) {
	d, err := pm.Parse(xyCorpus(t, "horizontal"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Drawn(d); err != nil {
		t.Errorf("Drawn(horizontal) = %v, want it drawn", err)
	}
	for name, src := range map[string]string{
		"xychart-horizontal": xyCorpus(t, "horizontal"),
		"xychart-horizontal-mixed": "xychart-beta horizontal\n  title Mixed\n  x-axis Team [north, south, east]\n  y-axis Score 0 --> 10\n" +
			"  bar \"Q1\" [4, 7.5, 2]\n  bar \"Q2\" [6, 3, 9]\n  line \"Target\" [5, 5, 5]\n",
	} {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// Laid across, each point stands beside its own category: the documented chart turned, July (the
// range's top) is the only bar reaching the plot's right edge, on the row of July's label.
func TestAHorizontalXYPointStandsBesideItsCategory(t *testing.T) {
	src := strings.Replace(xyCorpus(t, "documented"), "xychart-beta", "xychart-beta horizontal", 1)
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	g, err := LayXYChart(context.Background(), d.(*pm.XYChartDiagram), newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	var jul text
	for _, tx := range g.texts {
		if tx.s == "jul" {
			jul = tx
		}
	}
	if jul.s == "" {
		t.Fatal("no jul label")
	}
	right := -1
	for c, f := range g.fills {
		if f == "█" && c.x > right {
			right = c.x
		}
	}
	var rows []int
	for c, f := range g.fills {
		if f == "█" && c.x == right {
			rows = append(rows, c.y)
		}
	}
	if len(rows) != 1 || rows[0] != jul.y {
		t.Errorf("the bars reaching the right edge (column %d) are on rows %v; want jul's alone (%d)", right, rows, jul.y)
	}
}

// The key names every series when there is more than one, an unnamed one by its kind and place.
func TestAnXYChartsKeyNamesEverySeries(t *testing.T) {
	_, got := paint(t, "xychart-beta\n  bar [1, 2]\n  line \"trend\" [2, 1]\n")
	for _, want := range []string{"█ bar 1", "● trend"} {
		if !strings.Contains(got, want) {
			t.Errorf("the key lacks %q:\n%s", want, got)
		}
	}
}

// A value past the declared range is drawn clamped to it: a bar or a point past the top stands at
// the plot's top, not up into the title's row, and one under the bottom at the floor. The chart is
// drawn cell for cell as the one whose values sit at the range's ends.
func TestAnXYValuePastItsRangeIsClamped(t *testing.T) {
	const head = "xychart-beta\n  title Clamped\n  x-axis [a, b, c]\n  y-axis 0 --> 10\n"
	_, past := paint(t, head+"  bar [-5, 5, 15]\n  line [-5, 5, 15]\n")
	_, ends := paint(t, head+"  bar [0, 5, 10]\n  line [0, 5, 10]\n")
	if past != ends {
		t.Errorf("values past the range drew otherwise than the range's ends:\n%s\nwant:\n%s", past, ends)
	}
}

// A horizontal chart clamps the same way: a bar or a point past the range's top stops at the plot's
// right edge, one under its bottom at the value axis, drawn cell for cell as the range's ends.
func TestAHorizontalXYValuePastItsRangeIsClamped(t *testing.T) {
	const head = "xychart-beta horizontal\n  title Clamped\n  x-axis [a, b, c]\n  y-axis 0 --> 10\n"
	_, past := paint(t, head+"  bar [-5, 5, 15]\n  line [-5, 5, 15]\n")
	_, ends := paint(t, head+"  bar [0, 5, 10]\n  line [0, 5, 10]\n")
	if past != ends {
		t.Errorf("values past the range drew otherwise than the range's ends:\n%s\nwant:\n%s", past, ends)
	}
}
