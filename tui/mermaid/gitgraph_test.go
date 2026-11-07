package mermaid

import (
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const gitDocumented = `gitGraph
    commit id: "A"
    commit tag: "v1"
    branch develop
    checkout develop
    commit
    commit type: HIGHLIGHT
    checkout main
    merge develop
    commit type: REVERSE`

// A gitGraph draws in cells: a lane per branch, its name at the start, each commit's glyph on its
// lane in source order (later commits further along), the lines between them box-drawn with
// junctions where a branch leaves and joins, a tag over its commit and an id under it, in every
// direction.
func TestAGitGraphDrawsInCells(t *testing.T) {
	cases := map[string]string{
		"gitgraph-documented": gitDocumented,
		"gitgraph-tb":         "gitGraph TB:\n  commit id: \"A\"\n  branch b\n  commit\n  checkout main\n  merge b",
		"gitgraph-bt":         "gitGraph BT:\n  commit\n  branch b\n  commit\n  checkout main\n  commit",
		"gitgraph-cherry":     "gitGraph\n  commit id: \"A\"\n  branch b\n  commit id: \"B\"\n  checkout main\n  cherry-pick id: \"B\"",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// The documented example's cells: six commit glyphs, the merge ◉, the highlight ■ and the reverse
// ⊗ among them, each on its lane's row (develop's under main's) and each later one further right;
// both names, the tag and the id written; and the lines have a junction where develop leaves main
// and where it joins.
func TestAGitGraphsCellsAreItsGraph(t *testing.T) {
	g, screen := paint(t, gitDocumented)
	type glyph struct{ x, y int }
	var at []glyph
	var glyphs []string
	for c, s := range g.fills {
		at = append(at, glyph{c.x, c.y})
		glyphs = append(glyphs, s)
	}
	if len(g.fills) != 6 {
		t.Fatalf("%d commit glyphs, want 6:\n%s", len(g.fills), screen)
	}
	joined := strings.Join(glyphs, "")
	for _, want := range []string{"◉", "■", "⊗"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %s among the glyphs %q", want, joined)
		}
	}
	rows := map[int]int{}
	for _, a := range at {
		rows[a.y]++
	}
	if len(rows) != 2 {
		t.Errorf("glyphs on %d rows, want 2 lanes:\n%s", len(rows), screen)
	}
	for _, want := range []string{"main", "develop", "A", "v1"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%q not drawn:\n%s", want, screen)
		}
	}
	junctions := 0
	for _, d := range g.lines {
		if n := bitsOf(d); n >= 3 {
			junctions++
		}
	}
	if junctions < 2 {
		t.Errorf("%d junctions, want where develop leaves main and where it joins:\n%s", junctions, screen)
	}
	if !Supported(pm.GitGraph) {
		t.Error("gitGraph is not listed in Kinds")
	}
}

// bitsOf counts the sides a line cell joins.
func bitsOf(d dirs) int {
	n := 0
	for ; d != 0; d &= d - 1 {
		n++
	}
	return n
}

// The lanes: main first, then the branches given an order, by it, then the rest as they were made,
// as Mermaid places them; both renderers lane by it.
func TestGitLanesFollowTheOrders(t *testing.T) {
	d := mustParse(t, "gitGraph\n  commit\n  branch a order: 3\n  branch b order: 1\n  branch c\n  branch d\n").(*pm.GitGraphDiagram)
	got := GitLanes(d)
	want := []int{0, 2, 1, 3, 4} // main, a, b, c, d by branch index
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lanes %v, want %v (main, then b by order 1, a by 3, then c and d as made)", got, want)
		}
	}
}
