package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const requirementExample = `requirementDiagram
    requirement test_req {
    id: 1
    text: the test text.
    risk: high
    verifymethod: test
    }
    functionalRequirement test_req2 {
    id: 1.1
    text: the second test text.
    }
    element test_entity {
    type: simulation
    docRef: reqs/test_entity
    }
    test_entity - satisfies -> test_req2
    test_req - contains -> test_req2
    test_req <- copies - test_entity`

// requirementTexts counts the texts a Laid paints.
func requirementTexts(l *Laid) int {
	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 2000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	n := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			n++
		}
	}
	return n
}

// A requirement diagram lays out as boxes that do not overlap, each its «type», its name and its
// fields, and its relationships as edges: contains solid from a dot at the container, the rest
// dotted to an open arrow, each named «kind».
func TestARequirementDiagramIsLaidOutAndPainted(t *testing.T) {
	l, err := lay(t, requirementExample, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 3 || len(l.edges) != 3 {
		t.Fatalf("%d boxes and %d edges, want 3 and 3", len(l.nodes), len(l.edges))
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if overlaps(a.box, b.box) {
				t.Errorf("%q's box %+v overlaps %q's %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
		// every label inside its box
		for _, p := range a.parts {
			for _, lb := range p.labels {
				if lb.box.X < a.box.X || lb.box.X+lb.box.W > a.box.X+a.box.W+0.01 || lb.box.Y < a.box.Y || lb.box.Y+lb.box.H > a.box.Y+a.box.H+0.01 {
					t.Errorf("%q's %q at %+v is outside its box %+v", a.label.lines, lb.lines, lb.box, a.box)
				}
			}
		}
	}
	req := l.nodes[0]
	if req.label.lines[0] != "test_req" || req.parts[0].labels[0].lines[0] != "«Requirement»" || len(req.parts) != 2 ||
		len(req.parts[1].labels) != 4 || req.parts[1].labels[2].lines[0] != "Risk: High" || req.parts[1].labels[3].lines[0] != "Verification: Test" {
		t.Errorf("test_req's box: %+v", req.parts)
	}
	if el := l.nodes[2]; el.parts[0].labels[0].lines[0] != "«Element»" || el.parts[1].labels[1].lines[0] != "Doc Ref: reqs/test_entity" {
		t.Errorf("test_entity's box: %+v", el.parts)
	}
	sat, con, cop := l.edges[0], l.edges[1], l.edges[2]
	if sat.line != pm.Dotted || sat.head != endOpen || sat.tail != endNone || sat.label.lines[0] != "«satisfies»" {
		t.Errorf("satisfies: %+v", sat)
	}
	if con.line != pm.Solid || con.head != endNone || con.tail != endDot || con.label.lines[0] != "«contains»" {
		t.Errorf("contains: %+v", con)
	}
	if cop.line != pm.Dotted || cop.head != endOpen || cop.label.lines[0] != "«copies»" {
		t.Errorf("copies: %+v", cop)
	}
	// copies is written backward: test_entity copies test_req, so it ends at test_req
	if last := cop.pts[len(cop.pts)-1]; !overlaps(gui.Rect{X: last.X - 1, Y: last.Y - 1, W: 2, H: 2}, req.box) {
		t.Errorf("copies ends at %v, not at test_req's box %+v", last, req.box)
	}
	// each box: its type, its name and its fields (4, 2 and 2); each edge its name
	if got, want := requirementTexts(l), (2+4)+(2+2)+(2+2)+3; got != want {
		t.Errorf("%d texts painted, want %d", got, want)
	}
}

// A box with no fields has no divider; a requirement diagram wider than the width asked is
// scaled down to it.
func TestARequirementDiagramFitsAndABareBoxHasNoDivider(t *testing.T) {
	l, err := lay(t, "requirementDiagram\n  requirement bare { }", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 1 || len(l.nodes[0].parts) != 1 || l.nodes[0].parts[0].y != 0 {
		t.Errorf("a bare requirement's parts: %+v", l.nodes[0].parts)
	}
	l, err = lay(t, requirementExample, 200)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 200 || l.fit >= 1 {
		t.Errorf("size %+v fit %v: want 200 wide, scaled down", l.Size(), l.fit)
	}
}

// An empty requirement diagram lays out and paints.
func TestAnEmptyRequirementDiagramLaysOut(t *testing.T) {
	l, err := lay(t, "requirementDiagram", 400)
	if err != nil {
		t.Fatal(err)
	}
	if s := l.Size(); s.W < 0 || s.H < 0 {
		t.Errorf("size %+v", s)
	}
	if n := requirementTexts(l); n != 0 {
		t.Errorf("%d texts painted for nothing", n)
	}
}
