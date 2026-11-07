package mermaid_test

import (
	"context"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
)

// isRule reports whether s is a box-drawing character: an outline's, not a label's.
func isRule(s string) bool {
	r := []rune(s)
	return len(r) == 1 && (r[0] >= 0x2500 && r[0] <= 0x257f)
}

// tinted lays src out and paints it with Tint giving the box labelled "One" red, the box selected
// the one labelled sel ("" none): the painted cells and the boxes by label.
func tinted(t *testing.T, src, sel string, tint bool) (*piePainted, map[string]tuimermaid.Box) {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	g, err := tuimermaid.Lay(context.Background(), d, pieMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	boxes, selected := map[string]tuimermaid.Box{}, -1
	for i, b := range g.Boxes {
		boxes[b.Label] = b
		if b.Label == sel {
			selected = i
		}
	}
	th := tuimermaid.TokenTheme()
	if tint {
		one := boxes["One"].ID
		th.Tint = func(id string) (style.ColorSpec, bool) { return style.RGB(255, 0, 0), id == one }
	}
	p := newPiePainted(g.Size.W, g.Size.H)
	g.Paint(p, 0, 0, selected, th)
	return p, boxes
}

// outline is the foregrounds of box b's outline cells, and of its label's.
func outline(p *piePainted, b tuimermaid.Box) (rules, labels []style.Color) {
	for y := b.Rect.Y; y < b.Rect.Y+b.Rect.H; y++ {
		for x := b.Rect.X; x < b.Rect.X+b.Rect.W; x++ {
			switch s := p.text[y][x]; {
			case isRule(s):
				rules = append(rules, p.fg[y][x])
			case s != " " && s != "":
				labels = append(labels, p.fg[y][x])
			}
		}
	}
	return rules, labels
}

// A box the theme tints draws its outline in its own colour, its label as every label; a box it
// doesn't tint keeps the Box style; the selected box keeps Selected, tinted or not; and a theme with
// no Tint paints every outline in the Box style.
func TestABoxTheThemeTintsWearsItsOwnColour(t *testing.T) {
	const src = "mindmap\n  root[Hub]\n    a[One]\n    b[Two]\n"
	red := style.RGB(255, 0, 0)
	accent, _ := tuimermaid.TokenTheme().Box.GetForeground()
	label, _ := tuimermaid.TokenTheme().Label.GetForeground()
	all := func(cs []style.Color, want style.Color) bool {
		for _, c := range cs {
			if c != want {
				return false
			}
		}
		return len(cs) > 0
	}

	p, boxes := tinted(t, src, "", true)
	rules, labels := outline(p, boxes["One"])
	if !all(rules, red) || !all(labels, label) {
		t.Fatalf("One, tinted: outline %v, label %v; want the outline red, the label as every label", rules, labels)
	}
	for _, other := range []string{"Hub", "Two"} {
		if rules, _ := outline(p, boxes[other]); !all(rules, accent) {
			t.Errorf("%s, not tinted: outline %v; want the Box style's %v", other, rules, accent)
		}
	}

	p, boxes = tinted(t, src, "One", true)
	if rules, _ := outline(p, boxes["One"]); len(rules) == 0 || all(rules, red) {
		t.Errorf("One, tinted and selected: outline %v; want Selected's, not the tint", rules)
	}

	p, boxes = tinted(t, src, "", false)
	if rules, _ := outline(p, boxes["One"]); !all(rules, accent) {
		t.Errorf("One, with no Tint: outline %v; want the Box style's %v", rules, accent)
	}
}
