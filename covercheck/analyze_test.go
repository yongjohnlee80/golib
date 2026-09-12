package covercheck

import (
	"strings"
	"testing"
)

func TestAnalyzeAndEvaluate(t *testing.T) {
	t.Parallel()
	base := mustProfile(t, `mode: set
example.com/p/pkg/a.go:1.1,3.2 2 1
example.com/p/pkg/a.go:5.1,7.2 2 1
example.com/p/pkg/old.go:1.1,2.2 1 1
`)
	head := mustProfile(t, `mode: set
example.com/p/pkg/a.go:1.1,3.2 2 1
example.com/p/pkg/a.go:5.1,7.2 2 0
example.com/p/pkg/new.go:1.1,2.2 1 1
`)
	report, err := Analyze(base, head, []FileChange{
		{OldPath: "pkg/a.go", NewPath: "pkg/a.go", Kind: ChangeModified, Ranges: []LineRange{{Start: 6, End: 6}}},
		{OldPath: "pkg/old.go", NewPath: "pkg/new.go", Kind: ChangeRenamed, Ranges: []LineRange{{Start: 1, End: 1}}},
		{NewPath: "pkg/missing.go", Kind: ChangeAdded, Ranges: []LineRange{{Start: 1, End: 2}}, Executability: ExecutabilityPresent},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := report.ChangedBlocks; got != (Stats{Covered: 1, Total: 3}) {
		t.Fatalf("changed = %#v, want 1/3", got)
	}
	if got := report.Files[0].ChangedBlocks; got != (Stats{Covered: 0, Total: 2}) {
		t.Fatalf("a.go changed = %#v, want 0/2", got)
	}
	if report.Files[1].MissingAtHead != true {
		t.Fatalf("missing.go was not reported missing: %#v", report.Files[1])
	}
	violations, err := Evaluate(report,
		WithMinimumChanged(80),
		WithMaximumFileRegression(0),
		WithRequiredChangedFiles(),
		ForFiles(`new\.go$`, WithOverrideMinimumChanged(100)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 3 {
		for _, violation := range violations {
			t.Logf("%s %s %s", violation.Scope, violation.Path, violation.Rule)
		}
		t.Fatalf("violations = %d, want 3", len(violations))
	}
}

func TestAnalyzeDoesNotMarkDeclarationOnlyFileMissing(t *testing.T) {
	t.Parallel()
	base := mustProfileWithMode(t, "set")
	head := mustProfileWithMode(t, "set")
	report, err := Analyze(base, head, []FileChange{{
		NewPath:       "doc.go",
		Kind:          ChangeAdded,
		Executability: ExecutabilityAbsent,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Files[0].MissingAtHead {
		t.Fatalf("declaration-only file reported missing: %#v", report.Files[0])
	}
}

func TestAnalyzeRejectsDifferentModes(t *testing.T) {
	t.Parallel()
	base := mustProfileWithMode(t, "set")
	head := mustProfileWithMode(t, "atomic")
	if _, err := Analyze(base, head, nil); err == nil {
		t.Fatal("Analyze succeeded with different modes")
	}
}

func TestStatsPercentNoStatements(t *testing.T) {
	t.Parallel()
	if _, ok := (Stats{}).Percent(); ok {
		t.Fatal("zero statements were reported as measured")
	}
}

func TestChangedBlocksHonorExclusiveEndLine(t *testing.T) {
	t.Parallel()
	blocks := []Block{{
		Start:      Position{Line: 2, Column: 1},
		End:        Position{Line: 5, Column: 1},
		Statements: 3,
		Count:      1,
	}}
	if got := intersectStats(blocks, []LineRange{{Start: 5, End: 5}}); got != (Stats{}) {
		t.Fatalf("exclusive end line intersected: %#v", got)
	}
	if got := intersectStats(blocks, []LineRange{{Start: 4, End: 4}}); got != (Stats{Covered: 3, Total: 3}) {
		t.Fatalf("last included line = %#v, want 3/3", got)
	}
}

func mustProfile(t *testing.T, input string) *Profile {
	t.Helper()
	profile, err := ParseProfile(strings.NewReader(input), WithModulePath("example.com/p"))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func mustProfileWithMode(t *testing.T, mode string) *Profile {
	t.Helper()
	return mustProfile(t, "mode: "+mode+"\nexample.com/p/a.go:1.1,2.2 1 1\n")
}
