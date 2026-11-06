package mermaid

import (
	"errors"
	"reflect"
	"testing"
)

// kindSamples are a small diagram of each kind, by the header keyword that starts it.
var kindSamples = map[string]string{
	"flowchart":          "flowchart LR\n  A --> B",
	"graph":              "graph TD\n  A --> B",
	"sequenceDiagram":    "sequenceDiagram\n  A->>B: hi",
	"stateDiagram":       "stateDiagram\n  [*] --> Still\n  Still --> [*]",
	"stateDiagram-v2":    "stateDiagram-v2\n  [*] --> Still\n  Still --> [*]",
	"classDiagram":       "classDiagram\n  class A\n  A --> B",
	"classDiagram-v2":    "classDiagram-v2\n  class A\n  A --> B",
	"erDiagram":          "erDiagram\n  CUSTOMER ||--o{ ORDER : places",
	"journey":            "journey\n  section A\n  Make tea: 5: Me",
	"mindmap":            "mindmap\n  r\n    a",
	"timeline":           "timeline\n  2004 : Facebook",
	"requirementDiagram": "requirementDiagram\n  requirement a {\n  id: 1\n  text: the text\n  }",
	"gantt":              "gantt\n  dateFormat YYYY-MM-DD\n  a :2026-01-01, 3d",
	"pie":                "pie\n  \"a\": 1",
	"quadrantChart":      "quadrantChart\n  A: [0.5, 0.5]",
	"xychart-beta":       "xychart-beta\n  bar [1, 2]",
	"xychart":            "xychart\n  line [1, 2]",
}

// Kinds is the table Parse dispatches from: the thirteen kinds, each with its header keywords, and
// each keyword's diagram parses as its kind. A kind on no row (a gitGraph, a keyword in another
// case) is ErrUnsupported.
func TestKindsAreWhatParseReads(t *testing.T) {
	want := []KindInfo{
		{Flowchart, []string{"flowchart", "graph"}},
		{Sequence, []string{"sequenceDiagram"}},
		{State, []string{"stateDiagram", "stateDiagram-v2"}},
		{Class, []string{"classDiagram", "classDiagram-v2"}},
		{ER, []string{"erDiagram"}},
		{Journey, []string{"journey"}},
		{Mindmap, []string{"mindmap"}},
		{Timeline, []string{"timeline"}},
		{Requirement, []string{"requirementDiagram"}},
		{Gantt, []string{"gantt"}},
		{Pie, []string{"pie"}},
		{Quadrant, []string{"quadrantChart"}},
		{XYChart, []string{"xychart-beta", "xychart"}},
	}
	got := Kinds()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds() = %v\nwant %v", got, want)
	}
	for _, k := range got {
		for _, kw := range k.Keywords {
			d, err := Parse(kindSamples[kw])
			if err != nil || d.Kind() != k.Kind {
				t.Errorf("%s: %v, %v; want a %v", kw, d, err, k.Kind)
			}
		}
	}
	for _, src := range []string{"gitGraph\n  commit\n", "Flowchart LR\n  A --> B"} {
		if _, err := Parse(src); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: %v; want ErrUnsupported", src, err)
		}
	}
	// a copy: changing what Kinds answered changes no later answer
	got[0].Keywords[0] = "changed"
	if Kinds()[0].Keywords[0] != "flowchart" {
		t.Error("Kinds answered its table's own slice")
	}
}

// A kind registers itself, and a clash panics before it changes anything: a second kind with a
// registered value, name or keyword fails every test run in init, never ships. The registry is
// unchanged after each refusal.
func TestRegisteringAClashPanics(t *testing.T) {
	before := Kinds()
	for _, c := range []struct {
		what     string
		k        Kind
		name     string
		keywords []string
	}{
		{"a registered value", Flowchart, "another", []string{"another"}},
		{"a registered name", 200, "sequence", []string{"another"}},
		{"a registered keyword", 201, "another", []string{"graph"}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: register did not panic", c.what)
				}
			}()
			register(c.k, c.name, c.keywords, nil)
		}()
	}
	if after := Kinds(); !reflect.DeepEqual(after, before) {
		t.Errorf("the registry changed after the refusals:\n%v\nwas\n%v", after, before)
	}
	if Kind(200).String() != "200" || Flowchart.String() != "flowchart" {
		t.Errorf("String: %q %q", Kind(200).String(), Flowchart.String())
	}
}
