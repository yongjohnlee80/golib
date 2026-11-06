package mermaid

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// quadrantCorpus reads a chart of parse/mermaid's quadrant corpus: Mermaid's documented forms.
func quadrantCorpus(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "parse", "mermaid", "testdata", "quadrant", name+".mmd"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// quadrantCases are quadrant charts drawn in cells, each a golden: Mermaid's documented chart and
// its styled one (the styles read, not drawn), one with an axis label alone and points at the
// corners (narrow), and one with long labels and crowded points (wide, labels cut or footnoted).
func quadrantCases(t *testing.T) map[string]string {
	return map[string]string{
		"quadrant-documented": quadrantCorpus(t, "documented"),
		"quadrant-styled":     quadrantCorpus(t, "styled"),
		"quadrant-bare":       quadrantCorpus(t, "bare"),
		"quadrant-wide": "quadrantChart\n  title A crowded corner\n  x-axis Little effort required of the team --> A great deal of effort required\n" +
			"  y-axis Low --> High\n  quadrant-1 Major projects worth planning for\n  quadrant-2 Quick wins\n" +
			"  A point with a very long label indeed, longer than its room: [0.8, 0.9]\n  B: [0.85, 0.9]\n  C: [0.9, 0.9]\n" +
			"  D: [0.1, 0.1]\n  Middle: [0.5, 0.5]\n",
	}
}

// A quadrant chart draws in cells: its box split in four, each quadrant's label, each point a ●
// with its label beside it or a footnote, the axes' labels beside and under the box.
func TestAQuadrantChartDrawsInCells(t *testing.T) {
	for name, src := range quadrantCases(t) {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// Every quadrant holds as many ● as the points its numbers put there: left of the cross for x below
// 0.5, right for above, above the cross for y above 0.5, below for below, and a point at 0.5 on the
// cross. And no point's label is lost: it is beside its point, or a footnote.
func TestAQuadrantPointLiesInItsQuadrant(t *testing.T) {
	for name, src := range quadrantCases(t) {
		d, err := pm.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		q := d.(*pm.QuadrantDiagram)
		g, err := LayQuadrant(context.Background(), q, newCells(1, 1))
		if err != nil {
			t.Fatal(err)
		}
		// the cross: of the three columns (rows) a line runs the box's whole height (width) down,
		// the middle one
		crossX, crossY := middleOfLongest(g, func(c cell) int { return c.x }), middleOfLongest(g, func(c cell) int { return c.y })
		side := func(v float64) int { // -1 below 0.5, 0 at it, 1 above
			switch {
			case v < 0.5:
				return -1
			case v > 0.5:
				return 1
			}
			return 0
		}
		sign := func(v int) int {
			switch {
			case v < 0:
				return -1
			case v > 0:
				return 1
			}
			return 0
		}
		want, got := map[[2]int]int{}, map[[2]int]int{}
		for _, p := range q.Points {
			want[[2]int{side(p.X), side(p.Y)}]++
		}
		for c, mk := range g.marks {
			if mk == quadrantPoint {
				got[[2]int{sign(c.x - crossX), sign(crossY - c.y)}]++ // rows grow downward
			}
		}
		for k, n := range want {
			if got[k] != n && !overlap(q.Points) {
				t.Errorf("%s: %d points belong at (x %+d, y %+d) of the cross, %d ● are there (cross at column %d, row %d)", name, n, k[0], k[1], got[k], crossX, crossY)
			}
		}
		var all strings.Builder
		for _, tx := range g.texts {
			all.WriteString(tx.s + "\n")
		}
		for _, p := range q.Points {
			shown, _, _ := capLabel(p.Label, RoleLabel, newCells(1, 1))
			if !strings.Contains(all.String(), shown) {
				t.Errorf("%s: %q is neither beside its point nor a footnote:\n%s", name, p.Label, all.String())
			}
		}
	}
}

// middleOfLongest is, of the lines' columns (or rows, by key), the middle one of those with the most
// cells: the box's two sides and its cross run its whole height, the cross between them.
func middleOfLongest(g *Grid, key func(cell) int) int {
	count := map[int]int{}
	for c := range g.lines {
		count[key(c)]++
	}
	most := 0
	for _, n := range count {
		most = max(most, n)
	}
	var ks []int
	for k, n := range count {
		if n == most {
			ks = append(ks, k)
		}
	}
	slices.Sort(ks)
	return ks[len(ks)/2]
}

// overlap reports whether two points fall in one cell, which a count of marks can't tell apart.
func overlap(ps []pm.QuadrantPoint) bool {
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			if ps[i].X == ps[j].X && ps[i].Y == ps[j].Y {
				return true
			}
		}
	}
	return false
}

// A quadrant chart is drawn: listed in Kinds, and Lay lays it.
func TestAQuadrantChartIsDrawn(t *testing.T) {
	d, err := pm.Parse("quadrantChart\n  A: [0.5, 0.5]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !Supported(d.Kind()) {
		t.Error("quadrantChart is not listed in Kinds")
	}
	if g, err := Lay(context.Background(), d, newCells(1, 1)); err != nil || g.Size.H == 0 {
		t.Errorf("quadrantChart: %v, size %+v; want it laid", err, g)
	}
}
