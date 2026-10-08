package mermaid_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func requirementParse(t *testing.T, src string) *mermaid.RequirementDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	r, ok := d.(*mermaid.RequirementDiagram)
	if !ok || d.Kind() != mermaid.Requirement {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return r
}

// Mermaid's documented example: a requirement, an element, and the element satisfying it.
func TestRequirementDocumentedExample(t *testing.T) {
	src := `requirementDiagram

    requirement test_req {
    id: 1
    text: the test text.
    risk: high
    verifymethod: test
    }

    element test_entity {
    type: simulation
    }

    test_entity - satisfies -> test_req
`
	r := requirementParse(t, src)
	if len(r.Nodes) != 2 || len(r.Relations) != 1 || r.Dir != mermaid.TB {
		t.Fatalf("nodes %+v relations %+v dir %v", r.Nodes, r.Relations, r.Dir)
	}
	req, el := r.Nodes[0], r.Nodes[1]
	if req.Name != "test_req" || req.Type != mermaid.ReqRequirement || req.ID != "1" || req.Text != "the test text." ||
		req.Risk != mermaid.ReqRiskHigh || req.Verify != mermaid.ReqVerifyTest {
		t.Errorf("requirement %+v", req)
	}
	if el.Name != "test_entity" || el.Type != mermaid.ReqElement || el.ElemType != "simulation" || el.DocRef != "" {
		t.Errorf("element %+v", el)
	}
	if rel := r.Relations[0]; rel.Src != "test_entity" || rel.Dst != "test_req" || rel.Kind != mermaid.ReqSatisfies {
		t.Errorf("relation %+v", rel)
	}
	if got := src[req.Span[0]:req.Span[1]]; got != "requirement test_req {" {
		t.Errorf("the requirement's span is %q", got)
	}
	if rel := r.Relations[0]; src[rel.Span[0]:rel.Span[1]] != "test_entity - satisfies -> test_req" {
		t.Errorf("the relation's span is %q", src[rel.Span[0]:rel.Span[1]])
	}
}

// Every requirement type, its stereotype, and the fields read case-insensitively.
func TestRequirementTypesAndFields(t *testing.T) {
	r := requirementParse(t, `requirementDiagram
    requirement a { id: 1 }
    functionalRequirement b { risk: Low }
    interfaceRequirement c { risk: MEDIUM }
    performanceRequirement d { verifymethod: Analysis }
    physicalRequirement e { verifymethod: inspection }
    designConstraint f { verifymethod: demonstration
      text: "a quoted text: with a colon"
    }
    element g { type: "word doc"; docref: reqs/g.docx }`)
	want := []struct {
		typ        mermaid.RequirementType
		stereotype string
	}{
		{mermaid.ReqRequirement, "Requirement"}, {mermaid.ReqFunctional, "Functional Requirement"},
		{mermaid.ReqInterface, "Interface Requirement"}, {mermaid.ReqPerformance, "Performance Requirement"},
		{mermaid.ReqPhysical, "Physical Requirement"}, {mermaid.ReqDesignConstraint, "Design Constraint"},
		{mermaid.ReqElement, "Element"},
	}
	if len(r.Nodes) != len(want) {
		t.Fatalf("nodes %+v", r.Nodes)
	}
	for i, w := range want {
		if n := r.Nodes[i]; n.Type != w.typ || n.Type.Stereotype() != w.stereotype {
			t.Errorf("node %d: %v %q, want %v %q", i, n.Type, n.Type.Stereotype(), w.typ, w.stereotype)
		}
	}
	n := r.Nodes
	if n[0].ID != "1" || n[1].Risk != mermaid.ReqRiskLow || n[2].Risk != mermaid.ReqRiskMedium ||
		n[3].Verify != mermaid.ReqVerifyAnalysis || n[4].Verify != mermaid.ReqVerifyInspection ||
		n[5].Verify != mermaid.ReqVerifyDemonstration || n[5].Text != "a quoted text: with a colon" {
		t.Errorf("fields %+v", n[:6])
	}
	if n[6].ElemType != "word doc" || n[6].DocRef != "reqs/g.docx" {
		t.Errorf("element %+v", n[6])
	}
}

// Every relationship kind, written either way; a quoted name; direction and styles.
func TestRequirementRelationsAndStyles(t *testing.T) {
	r := requirementParse(t, `requirementDiagram
    direction LR
    requirement "the parent" { id: 1 }
    requirement child:::hot { id: 1.1 }
    element doc { type: doc }
    "the parent" - contains -> child
    child - copies -> "the parent"
    child - derives -> "the parent"
    doc - satisfies -> child
    doc - verifies -> child
    child - refines -> "the parent"
    child <- traces - doc
    classDef hot fill:#f99
    classDef default stroke:#333
    class doc hot
    style child stroke-width:3px`)
	if r.Dir != mermaid.LR || r.Nodes[0].Name != "the parent" {
		t.Fatalf("dir %v, first node %q", r.Dir, r.Nodes[0].Name)
	}
	want := []mermaid.RequirementRelation{
		{Src: "the parent", Dst: "child", Kind: mermaid.ReqContains},
		{Src: "child", Dst: "the parent", Kind: mermaid.ReqCopies},
		{Src: "child", Dst: "the parent", Kind: mermaid.ReqDerives},
		{Src: "doc", Dst: "child", Kind: mermaid.ReqSatisfies},
		{Src: "doc", Dst: "child", Kind: mermaid.ReqVerifies},
		{Src: "child", Dst: "the parent", Kind: mermaid.ReqRefines},
		{Src: "doc", Dst: "child", Kind: mermaid.ReqTraces}, // written backward: doc traces child
	}
	if len(r.Relations) != len(want) {
		t.Fatalf("relations %+v", r.Relations)
	}
	for i, w := range want {
		if g := r.Relations[i]; g.Src != w.Src || g.Dst != w.Dst || g.Kind != w.Kind {
			t.Errorf("relation %d: %+v, want %+v", i, g, w)
		}
	}
	child, doc := r.Nodes[1], r.Nodes[2]
	if !strings.Contains(styleString(child.Style), "fill:#f99") || !strings.Contains(styleString(child.Style), "stroke-width:3px") ||
		!strings.Contains(styleString(child.Style), "stroke:#333") {
		t.Errorf("child's style %v", child.Style)
	}
	if len(doc.Classes) != 1 || doc.Classes[0] != "hot" || !strings.Contains(styleString(doc.Style), "fill:#f99") {
		t.Errorf("doc's classes %v style %v", doc.Classes, doc.Style)
	}
}

func styleString(st mermaid.Style) string {
	var b strings.Builder
	for _, p := range st {
		b.WriteString(p.Name + ":" + p.Value + ";")
	}
	return b.String()
}

// A requirement diagram with nothing in it is a diagram with nothing in it.
func TestRequirementEmpty(t *testing.T) {
	if r := requirementParse(t, "requirementDiagram\n"); len(r.Nodes) != 0 || len(r.Relations) != 0 {
		t.Errorf("an empty diagram: %+v", r)
	}
}

// What is wrong in the grammar is a SyntaxError at its line; what golib does not draw is
// ErrUnsupported.
func TestRequirementErrors(t *testing.T) {
	for src, line := range map[string]int{
		"requirementDiagram\n  requirement a {\n  id: 1":                             2, // the block opened here
		"requirementDiagram\n  requirement a { risk: extreme }":                      2,
		"requirementDiagram\n  requirement a { verifymethod: guess }":                2,
		"requirementDiagram\n  requirement a { type: x }":                            2, // an element's field
		"requirementDiagram\n  element e { id: 1 }":                                  2, // a requirement's field
		"requirementDiagram\n  requirement a { nonsense }":                           2,
		"requirementDiagram\n  requirement a { id: 1 }\n  a - satisfies -> b":        3, // b is not defined
		"requirementDiagram\n  requirement a { }\n  element b { }\n  b - knows -> a": 4,
		"requirementDiagram\n  requirement two words { }":                            2,
		"requirementDiagram\n  requirement a":                                        2,
		"requirementDiagram\n  }":                                                    2,
		"requirementDiagram\n  requirement a { }\n  element a { }":                   3,
		"requirementDiagram\n  direction sideways":                                   2,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || se.Line != line {
			t.Errorf("%q: %v, want a SyntaxError on line %d", src, err, line)
		}
	}
	for _, src := range []string{
		"requirementDiagram\n  requirement a { id: 1 }\n  click a call x()",
		"requirementDiagram\n  just words",
		"requirementDiagram\n  requirement a { text: <b>bold</b> }",
		"requirementDiagram\n  accTitle {\n  t\n  }",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
