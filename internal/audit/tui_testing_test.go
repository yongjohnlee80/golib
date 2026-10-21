package audit

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE TUI PRODUCTION-CODE GUARD: package testing stays out of every binary that uses tui.
//
// tui once imported testing for one helper's *testing.T, so every program built on it linked the
// test framework, and the runtime reached its test double by concrete type to record constraint
// violations. Both were fixed (FailOnViolations takes an interface, violations reach a
// ViolationRecorder capability). This guard keeps them fixed: no non-test file under tui/ imports
// testing, and no tui package reaches one that does, except the packages whose purpose is testing,
// named below.

// tuiTestHelpers are the tui packages that exist to be imported by tests, and so may import testing.
var tuiTestHelpers = map[string]bool{
	modulePath + "/tui/decl/decltest": true,
}

// testingImporters is every package under tui/ with a non-test file importing testing.
func testingImporters(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "tui"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // the build reports it first
		}
		for _, spec := range f.Imports {
			if strings.Trim(spec.Path.Value, `"`) == "testing" {
				rel, err := filepath.Rel(root, filepath.Dir(path))
				if err != nil {
					return err
				}
				out[modulePath+"/"+filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking tui: %v", err)
	}
	return out
}

func TestTuiProductionCodeDoesNotLinkTesting(t *testing.T) {
	root := repoRoot(t)
	importers := testingImporters(t, root)
	// the positive control: a guard that sees no importer at all is not looking
	for helper := range tuiTestHelpers {
		if !importers[helper] {
			t.Fatalf("%s imports testing, and the scan did not see it; the guard is not looking", helper)
		}
	}
	graph := moduleImportGraph(t, root)
	var problems []string
	for pkg := range graph {
		if !strings.HasPrefix(pkg, modulePath+"/tui") || tuiTestHelpers[pkg] {
			continue
		}
		if importers[pkg] {
			problems = append(problems, pkg+" imports testing")
		}
		// a package reaching a test helper links testing through it
		seen := map[string]bool{pkg: true}
		queue := []string{pkg}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, imp := range graph[cur] {
				if tuiTestHelpers[imp] {
					problems = append(problems, pkg+" reaches "+imp+" (through "+cur+")")
				}
				if !seen[imp] {
					seen[imp] = true
					queue = append(queue, imp)
				}
			}
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Errorf("%s: a tui binary would link package testing; take an interface, or move the helper into a test-only package", p)
	}
}
