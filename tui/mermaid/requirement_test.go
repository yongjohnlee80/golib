package mermaid

import (
	"context"
	"fmt"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// requirementCases are requirement diagrams drawn in cells, each a golden: every field of a
// requirement and an element, a container's dot and the other kinds' dashed arrows, running down and
// across, and a relationship of a requirement with itself.
var requirementCases = map[string]string{
	"req-basic": `requirementDiagram
  requirement login {
    id: 1
    text: a user signs in
    risk: high
    verifymethod: test
  }
  functionalRequirement reset {
    id: 1.1
    text: a user resets a password
    risk: low
    verifymethod: demonstration
  }
  designConstraint bare
  element page {
    type: web page
    docref: docs/login.md
  }
  element probe
  login - contains -> reset
  page - satisfies -> login
  probe - verifies -> reset
  bare - traces -> reset
`,
	"req-lr": `requirementDiagram
  direction LR
  requirement a {
    id: 1
  }
  requirement b {
    id: 2
  }
  element c
  a - derives -> b
  c - refines -> b
`,
	"req-self": `requirementDiagram
  requirement a {
    id: 1
  }
  requirement b {
    id: 2
  }
  a - copies -> a
  a - contains -> b
`,
}

func parseRequirement(t *testing.T, src string) *pm.RequirementDiagram {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	rd, ok := d.(*pm.RequirementDiagram)
	if !ok {
		t.Fatalf("not a requirement diagram: %T", d)
	}
	return rd
}

// Requirement diagrams are drawn through the router, each its golden, and hold its invariants
// (checkGraph): every relationship routed, its end's mark beside its box, no label lost.
func TestARequirementDiagramDrawsInCells(t *testing.T) {
	for name, src := range requirementCases {
		t.Run(name, func(t *testing.T) {
			rd := parseRequirement(t, src)
			checkGraph(t, fmt.Sprintf("%s:\n%s", name, src), requirementGraph(rd))
			g, err := Lay(context.Background(), rd, newCells(1, 1))
			if err != nil {
				t.Fatal(err)
			}
			s := newCells(g.Size.W, g.Size.H)
			g.Paint(s, 0, 0, -1, Theme{})
			golden(t, name, s.String())
		})
	}
}

// A box holds its «type» and name, then the fields it gives, labelled, none it doesn't: a requirement's
// and an element's own, and a node with none is its head alone.
func TestARequirementsBoxHoldsItsFields(t *testing.T) {
	gr := requirementGraph(parseRequirement(t, requirementCases["req-basic"]))
	want := map[string][][]string{
		"login": {{"«Requirement»", "login"}, {"ID: 1", "Text: a user signs in", "Risk: High", "Verification: Test"}},
		"reset": {{"«Functional Requirement»", "reset"}, {"ID: 1.1", "Text: a user resets a password", "Risk: Low", "Verification: Demonstration"}},
		"bare":  {{"«Design Constraint»", "bare"}},
		"page":  {{"«Element»", "page"}, {"Type: web page", "Doc Ref: docs/login.md"}},
		"probe": {{"«Element»", "probe"}},
	}
	for _, n := range gr.nodes {
		if fmt.Sprintf("%q", n.sections) != fmt.Sprintf("%q", want[n.id]) {
			t.Errorf("%s's sections %q, want %q", n.id, n.sections, want[n.id])
		}
	}
}

// A container holds what it contains by a solid line from a dot at the container; every other kind is
// a dashed line with an arrowhead at its destination, each named «kind».
func TestARelationshipsKindIsItsLine(t *testing.T) {
	gr := requirementGraph(parseRequirement(t, requirementCases["req-basic"]))
	for _, e := range gr.edges {
		contains := e.label == "«contains»"
		if contains && (e.stroke != StrokeLight || e.tail != EndDot || e.head != EndNone) {
			t.Errorf("«contains»: stroke %d, tail %d, head %d, want solid, a dot at the container, no head", e.stroke, e.tail, e.head)
		}
		if !contains && (e.stroke != StrokeDashed || e.tail != EndNone || e.head != EndArrow) {
			t.Errorf("%s: stroke %d, tail %d, head %d, want dashed, an arrowhead at its destination", e.label, e.stroke, e.tail, e.head)
		}
		if !strings.HasPrefix(e.label, "«") || !strings.HasSuffix(e.label, "»") {
			t.Errorf("a relationship's label %q, want «kind»", e.label)
		}
	}
}

// A relationship whose end is no node is refused, ErrUnsupported, never dropped.
func TestARequirementRelationToNoNodeIsRefused(t *testing.T) {
	rd := &pm.RequirementDiagram{Dir: pm.TB, Nodes: []pm.RequirementNode{{Name: "a", Type: pm.ReqRequirement}},
		Relations: []pm.RequirementRelation{{Src: "a", Dst: "nowhere", Kind: pm.ReqTraces}}}
	if _, err := LayRequirement(context.Background(), rd, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("a relationship to no node: %v, want ErrUnsupported", err)
	}
}

// One that runs across and is too wide for its room is laid running down (LayWithin).
func TestAWideRequirementDiagramIsLaidDown(t *testing.T) {
	rd := parseRequirement(t, "requirementDiagram\n  direction LR\n  element one\n  element two\n  element three\n  element four\n"+
		"  one - traces -> two\n  two - traces -> three\n  three - traces -> four\n")
	ctx := context.Background()
	wide, err := Lay(ctx, rd, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	g, err := LayWithin(ctx, rd, newCells(1, 1), wide.Size.W-1)
	if err != nil {
		t.Fatal(err)
	}
	if g.Size.W >= wide.Size.W {
		t.Errorf("laid within %d: %d wide, not narrower than across (%d)", wide.Size.W-1, g.Size.W, wide.Size.W)
	}
	if requirementDown(parseRequirement(t, "requirementDiagram\n  element a\n")) != nil {
		t.Error("a diagram already running down has a narrower form")
	}
}
