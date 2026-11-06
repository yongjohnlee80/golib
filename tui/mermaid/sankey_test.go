package mermaid

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// sankeyCorpus reads a sankey of parse/mermaid's corpus: Mermaid's documented forms.
func sankeyCorpus(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "parse", "mermaid", "testdata", "sankey", name+".mmd"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// Sankeys drawn in cells through the router, each a golden, holding its invariants (checkGraph):
// every flow routed, its arrow at its target, no amount lost (beside its line or a footnote).
func TestASankeyDrawsInCells(t *testing.T) {
	for name, src := range map[string]string{
		"sankey-documented": sankeyCorpus(t, "documented"),
		"sankey-quoted":     sankeyCorpus(t, "quoted"),
		"sankey-chain":      "sankey-beta\na,b,10\nb,c,4\nc,d,9\n",
	} {
		t.Run(name, func(t *testing.T) {
			d, err := pm.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			checkGraph(t, fmt.Sprintf("%s:\n%s", name, src), sankeyGraph(d.(*pm.SankeyDiagram)))
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// A flow's weight is its line: thick when it carries at least half the largest flow, light under
// that; a node's box says what passes through it.
func TestASankeyFlowsWeightIsItsStroke(t *testing.T) {
	d, err := pm.Parse("sankey-beta\na,b,10\na,c,5\na,d,4.9\n")
	if err != nil {
		t.Fatal(err)
	}
	gr := sankeyGraph(d.(*pm.SankeyDiagram))
	for i, want := range []Stroke{StrokeThick, StrokeThick, StrokeLight} {
		if gr.edges[i].stroke != want {
			t.Errorf("flow %d (%s): stroke %d, want %d", i, gr.edges[i].label, gr.edges[i].stroke, want)
		}
	}
	if s := gr.nodes[0].sections; len(s) != 2 || s[0][0] != "a" || s[1][0] != "19.9" {
		t.Errorf("a's box %q, want its name and 19.9 through it", s)
	}
}
