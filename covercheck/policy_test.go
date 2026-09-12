package covercheck

import (
	"errors"
	"testing"
)

func TestEvaluateAllThresholdScopes(t *testing.T) {
	t.Parallel()
	negativeTen := -10.0
	report := Report{
		Total:         Comparison{Head: Stats{Covered: 7, Total: 10}, Delta: &negativeTen, State: EntityExisting},
		ChangedBlocks: Stats{Covered: 1, Total: 2},
		Packages:      []PackageReport{{Path: "pkg", Coverage: Comparison{Head: Stats{Covered: 7, Total: 10}, State: EntityExisting}}},
		Files: []FileReport{{
			Path:          "pkg/a.go",
			Kind:          ChangeModified,
			Coverage:      Comparison{Head: Stats{Covered: 7, Total: 10}, Delta: &negativeTen, State: EntityExisting},
			ChangedBlocks: Stats{Covered: 1, Total: 2},
		}},
	}
	violations, err := Evaluate(report,
		WithMinimumTotal(80),
		WithMinimumPackage(80),
		WithMinimumFile(80),
		WithMinimumChanged(80),
		WithMaximumTotalRegression(1),
		WithMaximumFileRegression(1),
		ForFiles(`a\.go$`, WithOverrideMinimumChanged(90)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 7 {
		for _, violation := range violations {
			t.Logf("%s/%s %s", violation.Scope, violation.Path, violation.Rule)
		}
		t.Fatalf("violations = %d, want 7", len(violations))
	}
}

func TestEvaluateExclusionRemovesChangedBlocks(t *testing.T) {
	t.Parallel()
	report := Report{
		ChangedBlocks: Stats{Covered: 1, Total: 2},
		Files: []FileReport{
			{Path: "included.go", Kind: ChangeModified, ChangedBlocks: Stats{Covered: 1, Total: 1}},
			{Path: "generated.go", Kind: ChangeModified, ChangedBlocks: Stats{Covered: 0, Total: 1}},
		},
	}
	violations, err := Evaluate(report, WithMinimumChanged(100), ExcludeFiles(`generated\.go$`))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v, want none", violations)
	}
}

func TestEvaluateRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()
	_, err := Evaluate(Report{}, WithMinimumTotal(101))
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("error = %v, want ErrInvalidPolicy", err)
	}
}
