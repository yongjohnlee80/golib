package mermaid

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

// blockCorpus reads a block diagram of parse/mermaid's corpus: Mermaid's documented forms.
func blockCorpus(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "parse", "mermaid", "testdata", "block", name+".mmd"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// layBlockSrc parses src and lays it out in cells.
func layBlockSrc(t *testing.T, src string) (*pm.BlockDiagram, *Grid) {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bd := d.(*pm.BlockDiagram)
	g, err := LayBlock(context.Background(), bd, newCells(1, 1))
	if err != nil {
		t.Fatalf("lay: %v", err)
	}
	return bd, g
}

// blockRects is each block's box, by id.
func blockRects(g *Grid) map[string]tui.Rect {
	out := map[string]tui.Rect{}
	for _, b := range g.Boxes {
		out[b.ID] = b.Rect
	}
	return out
}

// checkBlock holds a block diagram's invariants: every block drawn once; every link with a head
// has its arrowhead beside its target, pointing into it; no link's label lost (beside its line, or
// a footnote's row naming it); no line or label on a frame's border unless crossing it.
func checkBlock(t *testing.T, what string, d *pm.BlockDiagram, g *Grid) {
	t.Helper()
	rects := blockRects(g)
	var walk func(gr *pm.BlockGroup)
	walk = func(gr *pm.BlockGroup) {
		for _, it := range gr.Items {
			switch {
			case it.Group != nil:
				walk(it.Group)
			case !it.Space:
				if _, ok := rects[it.ID]; !ok {
					t.Errorf("%s: block %q not drawn", what, it.ID)
				}
			}
		}
	}
	walk(&d.Root)
	texts := map[string]bool{}
	var all []string
	for _, tx := range g.texts {
		texts[tx.s] = true
		all = append(all, tx.s)
		for x := tx.x; x < tx.x+tx.w; x++ {
			if g.onFrame(cell{x, tx.y}) {
				t.Errorf("%s: the label %q is on a frame's border", what, tx.s)
			}
			if _, line := g.lines[cell{x, tx.y}]; line {
				t.Errorf("%s: the label %q is over a line at %d,%d", what, tx.s, x, tx.y)
			}
		}
	}
	into := map[string]func(c cell, r tui.Rect) bool{
		"▼": func(c cell, r tui.Rect) bool { return c.y == r.Y-1 && c.x > r.X && c.x < r.X+r.W-1 },
		"▲": func(c cell, r tui.Rect) bool { return c.y == r.Y+r.H && c.x > r.X && c.x < r.X+r.W-1 },
		"►": func(c cell, r tui.Rect) bool { return c.x == r.X-1 && c.y > r.Y && c.y < r.Y+r.H-1 },
		"◄": func(c cell, r tui.Rect) bool { return c.x == r.X+r.W && c.y > r.Y && c.y < r.Y+r.H-1 },
	}
	for _, e := range d.Edges {
		if to, ok := rects[e.To]; ok && e.Head == pm.Head { // a link to a group: its frame, not checked here
			found := false
			for c, mk := range g.marks {
				if in := into[mk]; in != nil && in(c, to) {
					found = true
				}
			}
			noted := false
			for _, s := range all {
				noted = noted || strings.HasPrefix(s, e.From+" → "+e.To)
			}
			if !found && !noted {
				t.Errorf("%s: no arrowhead into %q for %s --> %s", what, e.To, e.From, e.To)
			}
		}
		if e.Label != "" && !texts[e.Label] {
			noted := false
			for _, s := range all {
				noted = noted || strings.Contains(s, e.Label)
			}
			if !noted {
				t.Errorf("%s: the label %q of %s --> %s is lost", what, e.Label, e.From, e.To)
			}
		}
	}
}

// Block diagrams drawn in cells, each a golden, holding the grid's and the kind's invariants: the
// corpus's documented forms (columns, widths, nested groups, links with shapes and a label), and a
// chain of links across rows and round a block in the way.
func TestABlockDiagramDrawsInCells(t *testing.T) {
	for name, src := range map[string]string{
		"block-columns": blockCorpus(t, "columns"),
		"block-widths":  blockCorpus(t, "widths"),
		"block-nested":  blockCorpus(t, "nested"),
		"block-links":   blockCorpus(t, "links"),
		"block-chain": "block-beta\n  columns 3\n  a b c\n  d e f\n  a --> f\n  c -- \"back\" --> d\n" +
			"  b --> e\n  e -.-> a\n",
	} {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
			d, _ := layBlockSrc(t, src)
			checkBlock(t, name, d, g)
		})
	}
}

// A block sits on its grid: a row's blocks share a top and run left to right, a column's blocks
// share a left edge and width, and the next row starts under the last.
func TestABlockSitsOnItsGrid(t *testing.T) {
	_, g := layBlockSrc(t, blockCorpus(t, "columns"))
	r := blockRects(g)
	for _, row := range [][]string{{"a", "b", "c"}, {"d", "e", "f"}} {
		for i := 1; i < len(row); i++ {
			p, q := r[row[i-1]], r[row[i]]
			if p.Y != q.Y || q.X <= p.X+p.W {
				t.Errorf("%s %+v and %s %+v: not side by side in a row", row[i-1], p, row[i], q)
			}
		}
	}
	for _, col := range [][2]string{{"a", "d"}, {"b", "e"}, {"c", "f"}} {
		p, q := r[col[0]], r[col[1]]
		if p.X != q.X || p.W != q.W || q.Y <= p.Y+p.H {
			t.Errorf("%s %+v and %s %+v: not one over the other in a column", col[0], p, col[1], q)
		}
	}
}

// A block's width is the columns it spans: in `a b:2 / c:2 d` b spans the second and third columns,
// so its right edge is d's; c spans the first two, so it starts at a and ends before d.
func TestABlockSpansItsWidth(t *testing.T) {
	_, g := layBlockSrc(t, blockCorpus(t, "widths"))
	r := blockRects(g)
	a, b, c, d := r["a"], r["b"], r["c"], r["d"]
	if b.X+b.W != d.X+d.W {
		t.Errorf("b %+v doesn't end where d %+v does", b, d)
	}
	if c.X != a.X || c.X+c.W >= d.X {
		t.Errorf("c %+v doesn't start at a %+v and end before d %+v", c, a, d)
	}
	if b.W <= d.W || c.W <= a.W {
		t.Errorf("a spanning block isn't wider than one column: b %+v d %+v, c %+v a %+v", b, d, c, a)
	}
}

// A group is a frame round its own blocks: group1's frame holds h, i, j and k, group2's l to r,
// and neither holds a, g or the other's blocks.
func TestABlockGroupFramesItsBlocks(t *testing.T) {
	_, g := layBlockSrc(t, blockCorpus(t, "nested"))
	r := blockRects(g)
	if len(g.frames) != 2 {
		t.Fatalf("%d frames; want 2", len(g.frames))
	}
	within := func(in, f tui.Rect) bool {
		return in.X > f.X && in.Y > f.Y && in.X+in.W < f.X+f.W && in.Y+in.H < f.Y+f.H
	}
	for i, ids := range [][]string{{"h", "i", "j", "k"}, {"l", "m", "n", "o", "p", "q", "r"}} {
		f := g.frames[i].rect
		for _, id := range ids {
			if !within(r[id], f) {
				t.Errorf("%s %+v is outside its group's frame %+v", id, r[id], f)
			}
		}
		for id, rr := range r {
			if !strings.Contains(strings.Join(ids, ""), id) && within(rr, f) {
				t.Errorf("%s %+v is inside group %d's frame %+v", id, rr, i+1, f)
			}
		}
	}
}

// A link goes round the block in its way: c --> e, with d between them, draws no line in d, and its
// head points into e from beside it; the label sits beside the line.
func TestABlockLinkGoesRoundABlock(t *testing.T) {
	src := blockCorpus(t, "links")
	d, g := layBlockSrc(t, src)
	checkGrid(t, "links", g)
	checkBlock(t, "links", d, g)
	r := blockRects(g)
	e := r["e"]
	heads := 0
	for c, mk := range g.marks {
		if strings.ContainsAny(mk, "▼▲►◄") && (c.x == e.X-1 || c.x == e.X+e.W || c.y == e.Y-1 || c.y == e.Y+e.H) &&
			c.x >= e.X-1 && c.x <= e.X+e.W && c.y >= e.Y-1 && c.y <= e.Y+e.H {
			heads++
		}
	}
	if heads != 1 {
		t.Errorf("%d arrowheads beside e; want 1", heads)
	}
}

// The invariants hold for random block diagrams: one to four columns, blocks one or two wide with
// labels of mixed widths, spaces, a nested group, and links between any two blocks, labelled or
// not, of every stroke.
func TestRandomBlockDiagramsHoldTheirInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	labels := []string{"", "x", "a node", "a longer label", "日本語"}
	links := []string{"-->", "---", "-.->", "==>", "<-->", "--o", "--x"}
	for trial := range 120 {
		var b strings.Builder
		b.WriteString("block-beta\n")
		cols := 1 + rng.IntN(4)
		fmt.Fprintf(&b, "  columns %d\n", cols)
		var ids []string
		item := func(indent string) {
			if rng.IntN(6) == 0 {
				b.WriteString(indent + "space\n")
				return
			}
			id := fmt.Sprint("n", len(ids))
			ids = append(ids, id)
			b.WriteString(indent + id)
			if l := labels[rng.IntN(len(labels))]; l != "" {
				fmt.Fprintf(&b, "[\"%s\"]", l)
			}
			if w := 1 + rng.IntN(2); w > 1 {
				fmt.Fprintf(&b, ":%d", w)
			}
			b.WriteString("\n")
		}
		for range 1 + rng.IntN(6) {
			item("  ")
		}
		if rng.IntN(2) == 0 {
			fmt.Fprintf(&b, "  block:g%d\n", trial)
			for range 1 + rng.IntN(4) {
				item("    ")
			}
			b.WriteString("  end\n")
		}
		for range rng.IntN(5) {
			if len(ids) < 2 {
				break
			}
			from, to := ids[rng.IntN(len(ids))], ids[rng.IntN(len(ids))]
			if from == to {
				continue
			}
			l := links[rng.IntN(len(links))]
			if rng.IntN(3) == 0 {
				fmt.Fprintf(&b, "  %s -- \"%s\" --> %s\n", from, labels[1+rng.IntN(len(labels)-1)], to)
			} else {
				fmt.Fprintf(&b, "  %s %s %s\n", from, l, to)
			}
		}
		src := b.String()
		d, err := pm.Parse(src)
		if err != nil {
			t.Fatalf("trial %d: %v\n%s", trial, err, src)
		}
		g, err := LayBlock(context.Background(), d.(*pm.BlockDiagram), newCells(1, 1))
		if err != nil {
			t.Fatalf("trial %d: %v\n%s", trial, err, src)
		}
		what := fmt.Sprintf("trial %d:\n%s", trial, src)
		checkGrid(t, what, g)
		checkBlock(t, what, d.(*pm.BlockDiagram), g)
	}
}
