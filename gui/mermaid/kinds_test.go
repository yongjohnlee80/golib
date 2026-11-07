package mermaid

import (
	"reflect"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Kinds are the kinds Lay draws: every kind the parser reads has a layer, and a small diagram of
// each, by each of its keywords, lays out.
func TestKindsAreWhatLayDraws(t *testing.T) {
	got := Kinds()
	if !reflect.DeepEqual(got, pm.Kinds()) {
		t.Fatalf("Kinds() = %v\nwant every kind the parser reads: %v", got, pm.Kinds())
	}
	samples := map[pm.Kind]string{
		pm.Flowchart: "flowchart LR\n  A --> B", pm.Sequence: "sequenceDiagram\n  A->>B: hi",
		pm.State: "stateDiagram-v2\n  [*] --> Still\n  Still --> [*]", pm.Class: "classDiagram\n  class A\n  A --> B",
		pm.ER: "erDiagram\n  CUSTOMER ||--o{ ORDER : places", pm.Journey: "journey\n  section A\n  Make tea: 5: Me",
		pm.Mindmap: "mindmap\n  r\n    a", pm.Timeline: "timeline\n  2004 : Facebook",
		pm.Requirement: "requirementDiagram\n  requirement a {\n  id: 1\n  text: the text\n  }",
		pm.Gantt:       "gantt\n  a :2026-01-01, 3d",
		pm.Pie:         "pie\n  \"a\": 1",
		pm.Quadrant:    "quadrantChart\n  A: [0.5, 0.5]",
		pm.XYChart:     "xychart-beta\n  bar [1, 2]",
		pm.Sankey:      "sankey-beta\na,b,1",
		pm.Block:       "block-beta\n  a b\n  a --> b",
		pm.GitGraph:    "gitGraph\n  commit\n  branch b\n  commit\n  checkout main\n  merge b",
	}
	for _, k := range got {
		if _, err := lay(t, samples[k.Kind], 600); err != nil {
			t.Errorf("a %v: %v", k.Kind, err)
		}
	}
}
