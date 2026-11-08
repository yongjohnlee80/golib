package mermaid_test

import (
	"context"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
)

// A block arrow is drawn as its box with an arrowhead on the side it points to (both sides for x and
// y), in the outline's colour, its label inside; a mind map node with an icon is drawn with its text.
func TestABlockArrowIsDrawnPointingItsWay(t *testing.T) {
	d, err := pm.Parse("block-beta\n  columns 3\n  a go<[\"next\"]>(right) b\n  l<[\"back\"]>(left) x<[\"wide\"]>(x) y<[\"tall\"]>(y)\n  dn<[\"down\"]>(down) up<[\"up\"]>(up)\n")
	if err != nil {
		t.Fatal(err)
	}
	g, err := tuimermaid.Lay(context.Background(), d, pieMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	p := newPiePainted(g.Size.W, g.Size.H)
	th := tuimermaid.TokenTheme()
	g.Paint(p, 0, 0, -1, th)
	box, _ := th.Box.GetForeground()
	at := func(x, y int) string { return p.text[y][x] }
	for _, b := range g.Boxes {
		r := b.Rect
		right, bottom, mid, cx := r.X+r.W-1, r.Y+r.H-1, r.Y+r.H/2, r.X+r.W/2
		heads := map[string][2]int{}
		switch b.ID {
		case "go":
			heads["▶"] = [2]int{right, mid}
		case "l":
			heads["◀"] = [2]int{r.X, mid}
		case "x":
			heads["◀"], heads["▶"] = [2]int{r.X, mid}, [2]int{right, mid}
		case "y":
			heads["▲"], heads["▼"] = [2]int{cx, r.Y}, [2]int{cx, bottom}
		case "dn":
			heads["▼"] = [2]int{cx, bottom}
		case "up":
			heads["▲"] = [2]int{cx, r.Y}
		}
		for head, c := range heads {
			if got := at(c[0], c[1]); got != head || p.fg[c[1]][c[0]] != box {
				t.Errorf("%s: %q in %v at %v; want %s in the outline's colour", b.ID, got, p.fg[c[1]][c[0]], c, head)
			}
		}
		if len(heads) == 0 && strings.ContainsAny(strings.Join(p.text[mid][r.X:r.X+r.W], ""), "▶◀▲▼") {
			t.Errorf("%s, a plain block, has an arrowhead", b.ID)
		}
	}
	var rows []string
	for y := range p.h {
		rows = append(rows, strings.TrimRight(strings.Join(p.text[y], ""), " "))
	}
	t.Logf("block arrows:\n%s", strings.Join(rows, "\n"))

	md, err := pm.Parse("mindmap\n  root((Plan))\n    ::icon(fa fa-book)\n    Read")
	if err != nil {
		t.Fatal(err)
	}
	mg, err := tuimermaid.Lay(context.Background(), md, pieMeasure{})
	if err != nil {
		t.Fatalf("a mind map with an icon: %v; want it drawn", err)
	}
	labels := map[string]bool{}
	for _, b := range mg.Boxes {
		labels[b.Label] = true
	}
	if !labels["Plan"] || !labels["Read"] {
		t.Errorf("the mind map's boxes %v; want Plan and Read, the icon's node with its text", labels)
	}
}
