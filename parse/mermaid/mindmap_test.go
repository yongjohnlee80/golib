package mermaid_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// mindmapDoc is Mermaid's documented mind map, its icon taken out.
const mindmapDoc = `mindmap
  root((mindmap))
    Origins
      Long history
      Popularisation
        British popular psychology author Tony Buzan
    Research
      On effectiveness<br/>and features
      On Automatic creation
        Uses
            Creative techniques
            Strategic planning
            Argument mapping
    Tools
      Pen and paper
      Mermaid
`

func mindmapParse(t *testing.T, src string) *mermaid.MindmapDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	m, ok := d.(*mermaid.MindmapDiagram)
	if !ok || d.Kind() != mermaid.Mindmap {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return m
}

// The documented mind map is a tree by indentation: each node under the nearest line above it
// indented less, the root first.
func TestMindmapIsATreeByIndentation(t *testing.T) {
	m := mindmapParse(t, mindmapDoc)
	if len(m.Nodes) != 15 {
		t.Fatalf("%d nodes, want 15", len(m.Nodes))
	}
	parent := func(label string) string {
		for _, n := range m.Nodes {
			if n.Label == label {
				if n.Parent < 0 {
					return ""
				}
				return m.Nodes[n.Parent].Label
			}
		}
		t.Fatalf("no node %q", label)
		return ""
	}
	for child, want := range map[string]string{
		"mindmap": "", "Origins": "mindmap", "Long history": "Origins", "Popularisation": "Origins",
		"British popular psychology author Tony Buzan": "Popularisation",
		"On effectiveness\nand features":               "Research", "Uses": "On Automatic creation",
		"Argument mapping": "Uses", "Tools": "mindmap", "Mermaid": "Tools",
	} {
		if got := parent(child); got != want {
			t.Errorf("%q's parent is %q, want %q", child, got, want)
		}
	}
	if r := m.Nodes[0]; r.ID != "root" || r.Shape != mermaid.MindCircle || r.Parent != -1 {
		t.Errorf("the root %+v", r)
	}
	if s := m.Nodes[1].Span; mindmapDoc[s[0]:s[1]] != "Origins" {
		t.Errorf("Origins' span reads %q", mindmapDoc[s[0]:s[1]])
	}
}

// Every shape reads with its id and label; text with a space before a bracket is text.
func TestMindmapShapes(t *testing.T) {
	m := mindmapParse(t, "mindmap\n  r\n    a[square]\n    b(rounded)\n    c((circle))\n    d))bang((\n    e)cloud(\n    f{{hexagon}}\n    Ideas (more)\n    [no id]\n")
	want := []struct {
		id, label string
		shape     mermaid.MindShape
	}{
		{"r", "r", mermaid.MindDefault}, {"a", "square", mermaid.MindSquare}, {"b", "rounded", mermaid.MindRounded},
		{"c", "circle", mermaid.MindCircle}, {"d", "bang", mermaid.MindBang}, {"e", "cloud", mermaid.MindCloud},
		{"f", "hexagon", mermaid.MindHexagon}, {"Ideas (more)", "Ideas (more)", mermaid.MindDefault},
		{"no id", "no id", mermaid.MindSquare},
	}
	if len(m.Nodes) != len(want) {
		t.Fatalf("%d nodes, want %d", len(m.Nodes), len(want))
	}
	for i, w := range want {
		if n := m.Nodes[i]; n.ID != w.id || n.Label != w.label || n.Shape != w.shape {
			t.Errorf("node %d: %+v, want %+v", i, n, w)
		}
	}
}

// An empty mind map has no nodes; a second root and an unclosed shape are SyntaxErrors at their
// line; icons and classes are ErrUnsupported.
func TestMindmapErrors(t *testing.T) {
	if m := mindmapParse(t, "mindmap\n"); len(m.Nodes) != 0 {
		t.Errorf("an empty mind map has %d nodes", len(m.Nodes))
	}
	for src, line := range map[string]int{
		"mindmap\n  a\n  b":        3,
		"mindmap\n  a\n    b[open": 3,
		"mindmap\n  a\n    b((x)":  3,
		"mindmap junk\n  a":        1,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || se.Line != line {
			t.Errorf("%q: %v, want a SyntaxError on line %d", src, err, line)
		}
	}
	for _, src := range []string{
		"mindmap\n  a\n    b\n    ::icon(fa fa-book)",
		"mindmap\n  a\n    b:::urgent",
		"mindmap\n  a\n    b[<b>bold</b>]",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
