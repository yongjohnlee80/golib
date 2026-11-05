package mermaid

import (
	"context"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A pie draws in cells: its circle rastered in filled characters, one density a slice, its legend
// beside it — the label, the share, and the value when the header's showData asks — the title
// over all. A pie whose values sum to nothing has no circle: its legend alone.
func TestAPieDrawsInCells(t *testing.T) {
	cases := map[string]string{
		"pie-documented": "pie title NETFLIX\n  \"Time spent looking for movie\" : 70\n  \"Time spent watching it\" : 30\n",
		"pie-showdata":   "pie showData\n  \"a\" : 2.5\n  \"b\" : 7.5\n",
		"pie-five":       "pie\n  \"one\": 1\n  \"two\": 2\n  \"three\": 3\n  \"four\": 4\n  \"five\": 5\n",
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

// The legend tells every slice: its label and its share a slice, its value too when showData asks.
func TestPieLegendTellsEverySlice(t *testing.T) {
	g, _ := paint(t, "pie showData\n  \"a\": 2.5\n  \"b\": 7.5\n")
	var rows []string
	for _, t2 := range g.texts {
		rows = append(rows, t2.s)
	}
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"a", "b", "25%", "75%", "2.5", "7.5"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the legend holds no %q: %q", want, joined)
		}
	}
	if len(rows) != 2 {
		t.Errorf("legend rows %d; want one a slice", len(rows))
	}
}
