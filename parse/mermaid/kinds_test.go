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
}

// Kinds is the table Parse dispatches from: the nine kinds, each with its header keywords, and
// each keyword's diagram parses as its kind. A kind on no row (a pie, a gantt, a keyword in
// another case) is ErrUnsupported.
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
	for _, src := range []string{"pie\n  \"a\": 1", "gantt\n  title T", "quadrantChart\n", "Flowchart LR\n  A --> B"} {
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
