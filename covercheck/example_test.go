package covercheck_test

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/covercheck"
)

func Example() {
	base, _ := covercheck.ParseProfile(strings.NewReader(`mode: set
example.com/project/pkg/value.go:1.1,3.2 2 1
`), covercheck.WithModulePath("example.com/project"))
	head, _ := covercheck.ParseProfile(strings.NewReader(`mode: set
example.com/project/pkg/value.go:1.1,3.2 2 0
`), covercheck.WithModulePath("example.com/project"))

	report, _ := covercheck.Analyze(base, head, []covercheck.FileChange{{
		OldPath: "pkg/value.go",
		NewPath: "pkg/value.go",
		Kind:    covercheck.ChangeModified,
		Ranges:  []covercheck.LineRange{{Start: 2, End: 2}},
	}})
	violations, _ := covercheck.Evaluate(report,
		covercheck.WithMaximumFileRegression(0),
	)

	fmt.Println(covercheck.NewResult(report, violations).Passed)
	// Output: false
}
