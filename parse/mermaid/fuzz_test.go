package mermaid_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// FuzzParse: any source parses as a diagram or is refused, and never panics or runs away.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "flowchart", "flowchart TD\nA --> B", "graph LR\nA[box] -->|label| B((round))\nB -.-> C{rhombus}",
		"sequenceDiagram\nA->>B: hi\nB-->>A: back", "classDiagram\nclass A { +f() int }\nA <|-- B",
		"stateDiagram-v2\n[*] --> S\nS --> [*]", "pie title P\n\"a\": 1\n\"b\": 2",
		"journey\ntitle T\nsection S\nA task: 5: Me", "mindmap\n  root((r))\n    a\n      b",
		"gantt\ndateFormat YYYY-MM-DD\nsection S\nTask :a1, 2026-01-01, 3d", "flowchart TD\nA --> B --> C --> A",
		"erDiagram\nA ||--o{ B : has", "flowchart TD\nsubgraph S\nA\nend",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		d, err := mermaid.Parse(src)
		if err == nil && d == nil {
			t.Fatal("no error and no diagram")
		}
	})
}
