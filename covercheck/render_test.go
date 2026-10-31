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

// TestTheReportSaysAFileHadNoCodeChanged: a modified file whose change touched none of its
// statements is marked "no code changed" in the text and Markdown reports, so a reader sees why the
// per-file limit did not hold it; a file whose code changed is not.
func TestTheReportSaysAFileHadNoCodeChanged(t *testing.T) {
	t.Parallel()
	cov := Comparison{Base: Stats{Covered: 2, Total: 2}, Head: Stats{Covered: 1, Total: 2}, Delta: floatPointer(-50), State: EntityExisting}
	result := NewResult(Report{Mode: ModeSet, Files: []FileReport{
		{Path: "pkg/comments.go", Kind: ChangeModified, Coverage: cov},
		{Path: "pkg/code.go", Kind: ChangeModified, Coverage: cov, ChangedBlocks: Stats{Covered: 0, Total: 1}},
	}}, nil)
	var text, markdown bytes.Buffer
	if err := WriteText(&text, result); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarkdown(&markdown, result); err != nil {
		t.Fatal(err)
	}
	line := func(out, path string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, path) {
				return l
			}
		}
		return ""
	}
	for name, out := range map[string]string{"text": text.String(), "markdown": markdown.String()} {
		if l := line(out, "comments.go"); !strings.Contains(l, "no code changed") {
			t.Errorf("%s: comments.go's row %q does not say no code changed", name, l)
		}
		if l := line(out, "code.go\t"); name == "text" && strings.Contains(l, "no code changed") {
			t.Errorf("%s: code.go's row %q says no code changed", name, l)
		}
		if l := line(out, "`pkg/code.go`"); name == "markdown" && strings.Contains(l, "no code changed") {
			t.Errorf("%s: code.go's row %q says no code changed", name, l)
		}
	}
}
