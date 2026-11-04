package mermaid

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

var update = flag.Bool("update", false, "write the golden snapshots")

// cells is a tui.Surface over a plain grid of clusters, for snapshots: a wide cluster takes its
// cell and leaves the next empty, as a terminal draws it.
type cells struct {
	w, h int
	at   [][]string
}

func newCells(w, h int) *cells {
	c := &cells{w: w, h: h, at: make([][]string, h)}
	for y := range c.at {
		c.at[y] = make([]string, w)
		for x := range c.at[y] {
			c.at[y][x] = " "
		}
	}
	return c
}

func (c *cells) SetCell(x, y int, content string, _ style.Style) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || content == "" {
		return
	}
	c.at[y][x] = content
	if tui.StringWidth(content) == 2 && x+1 < c.w {
		c.at[y][x+1] = ""
	}
}
func (c *cells) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c.SetCell(x, y, content, st)
		}
	}
}
func (c *cells) Sub(tui.Rect) tui.Surface { return c }
func (c *cells) Size() tui.Size           { return tui.Size{W: c.w, H: c.h} }
func (c *cells) StringWidth(s string) int { return tui.StringWidth(s) }
func (c *cells) Theme() *style.Theme      { return nil }
func (c *cells) Caps() tui.Capabilities   { return tui.Capabilities{} }
func (c *cells) String() string {
	var b strings.Builder
	for _, row := range c.at {
		b.WriteString(strings.TrimRight(strings.Join(row, ""), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

var _ tui.Surface = (*cells)(nil)

// paint lays src out and paints it into a surface its own size.
func paint(t *testing.T, src string) (*Grid, string) {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := newCells(1, 1)
	g, err := Lay(context.Background(), d, m)
	if err != nil {
		t.Fatalf("lay: %v", err)
	}
	s := newCells(g.Size.W, g.Size.H)
	g.Paint(s, 0, 0, -1, Theme{})
	return g, s.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (go test ./tui/mermaid -run %s -update)", path, err, t.Name())
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// Every direction mask has its own box-drawing character: no two share one, and none is blank.
func TestEveryLineJunctionHasItsCharacter(t *testing.T) {
	seen := map[string]dirs{}
	for d := dirs(1); d < 16; d++ {
		r := lineRune[d]
		if r == "" || r == " " {
			t.Errorf("dirs %04b has no character", d)
			continue
		}
		if o, ok := seen[r]; ok {
			t.Errorf("dirs %04b and %04b both draw %q", d, o, r)
		}
		seen[r] = d
	}
}

// A mind map draws as a two-sided tree: the root in the middle, a child each side, a parent's
// children on one shared spine, each shape its own outline.
func TestAMindmapDrawsAsATwoSidedTree(t *testing.T) {
	cases := map[string]string{
		"mindmap-two-sides": "mindmap\n  root((AutoDoc))\n    left[Search]\n    right(Agents)\n",
		"mindmap-spine":     "mindmap\n  root))Hub((\n    a[One]\n      a1(Leaf A)\n      a2(Leaf B)\n      a3(Leaf C)\n    b{{Two}}\n",
		"mindmap-plain":     "mindmap\n  Root\n    child one\n    child two\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, got := paint(t, src)
			golden(t, name, got)
		})
	}
}

// Boxes never overlap or touch, and no line runs through a box, whatever the tree: random trees of
// mixed shapes, depths and label widths, wide ones among them.
func TestNoBoxesOverlapAndNoLineCrossesOne(t *testing.T) {
	labels := []string{"a", "node", "a longer label", "日本語", "🙂 emoji", "x"}
	shapes := []pm.MindShape{pm.MindDefault, pm.MindSquare, pm.MindRounded, pm.MindHexagon, pm.MindBang, pm.MindCircle}
	rng := rand.New(rand.NewPCG(7, 11))
	for trial := range 200 {
		n := 1 + rng.IntN(40)
		d := &pm.MindmapDiagram{}
		for i := range n {
			parent := -1
			if i > 0 {
				parent = rng.IntN(i)
			}
			d.Nodes = append(d.Nodes, pm.MindNode{ID: fmt.Sprint(i), Label: labels[rng.IntN(len(labels))],
				Shape: shapes[rng.IntN(len(shapes))], Parent: parent})
		}
		g, err := LayMindmap(context.Background(), d, newCells(1, 1))
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		for i, a := range g.Boxes {
			if a.Rect.X < 0 || a.Rect.Y < 0 || a.Rect.X+a.Rect.W > g.Size.W || a.Rect.Y+a.Rect.H > g.Size.H {
				t.Fatalf("trial %d: box %d %+v outside the grid %+v", trial, i, a.Rect, g.Size)
			}
			for j := i + 1; j < len(g.Boxes); j++ {
				b := g.Boxes[j]
				if a.Rect.X < b.Rect.X+b.Rect.W+1 && b.Rect.X < a.Rect.X+a.Rect.W+1 &&
					a.Rect.Y < b.Rect.Y+b.Rect.H+1 && b.Rect.Y < a.Rect.Y+a.Rect.H+1 {
					t.Fatalf("trial %d: boxes %d %+v and %d %+v overlap or touch", trial, i, a.Rect, j, b.Rect)
				}
			}
			for c := range g.lines {
				if c.x >= a.Rect.X && c.x < a.Rect.X+a.Rect.W && c.y >= a.Rect.Y && c.y < a.Rect.Y+a.Rect.H {
					t.Fatalf("trial %d: a line runs through box %d %+v at %v", trial, i, a.Rect, c)
				}
			}
		}
	}
}

// A wide label is measured in cells, so its box closes: the outline is the label's width plus
// four, and the right border sits on that column.
func TestAWideLabelsBoxCloses(t *testing.T) {
	g, got := paint(t, "mindmap\n  root[日本語のノード]\n")
	b := g.Boxes[0]
	if want := tui.StringWidth("日本語のノード") + 4; b.Rect.W != want {
		t.Fatalf("the box is %d wide, want %d", b.Rect.W, want)
	}
	rows := strings.Split(got, "\n")
	if !strings.HasPrefix(rows[0], "┌") || !strings.HasSuffix(rows[0], "┐") ||
		!strings.HasPrefix(rows[1], "│ 日本語のノード │") || !strings.HasPrefix(rows[2], "└") {
		t.Fatalf("the box isn't closed:\n%s", got)
	}
}

// A kind not in Kinds is ErrUnsupported, for the host to show its own way.
func TestAKindNotDrawnIsUnsupported(t *testing.T) {
	d, err := pm.Parse("flowchart LR\n  a --> b\n")
	if err != nil {
		t.Fatal(err)
	}
	if Supported(d.Kind()) {
		t.Fatal("a flowchart is reported drawn")
	}
	if _, err := Lay(context.Background(), d, newCells(1, 1)); err != ErrUnsupported {
		t.Fatalf("Lay of a flowchart: %v, want ErrUnsupported", err)
	}
}
