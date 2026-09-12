package covercheck

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderersRepresentResult(t *testing.T) {
	t.Parallel()
	report := Report{
		Mode: ModeSet,
		Total: Comparison{
			Base:  Stats{Covered: 1, Total: 2},
			Head:  Stats{Covered: 2, Total: 2},
			Delta: floatPointer(50),
			State: EntityExisting,
		},
		ChangedBlocks: Stats{Covered: 1, Total: 1},
		Files: []FileReport{{
			Path:          "pkg/a|b.go",
			Kind:          ChangeModified,
			Coverage:      Comparison{Base: Stats{Covered: 1, Total: 2}, Head: Stats{Covered: 2, Total: 2}, Delta: floatPointer(50), State: EntityExisting},
			ChangedBlocks: Stats{Covered: 1, Total: 1},
		}},
	}
	result := NewResult(report, nil)
	var textOutput, markdownOutput, jsonOutput bytes.Buffer
	if err := WriteText(&textOutput, result); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarkdown(&markdownOutput, result); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(&jsonOutput, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOutput.String(), "+50.00pp") || !strings.Contains(markdownOutput.String(), "a\\|b.go") {
		t.Fatalf("rendered outputs missing values:\ntext=%s\nmarkdown=%s", textOutput.String(), markdownOutput.String())
	}
	var decoded Result
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Passed || decoded.Report.Total.Head != report.Total.Head {
		t.Fatalf("decoded result = %#v", decoded)
	}
}

func floatPointer(value float64) *float64 { return &value }
