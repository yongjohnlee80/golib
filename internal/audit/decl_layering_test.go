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

// THE DECL LAYERING GUARD.
//
// The declarative engine is meant to be usable by more than one UI toolkit. The
// whole design rests on it: a second toolkit ships its own adapter and reuses
// the engine unchanged. That claim is cheap to make in a doc comment and cheap
// to break by accident, because reaching for a toolkit type is exactly what one
// needs when a layout question gets hard.
//
// So it is a MECHANISM here rather than an intention there. The engine must not
// import the terminal UI packages, directly or through anything else it
// imports. If it ever needs to, that is a design decision someone should have
// to argue for by deleting this test.
//
// The walk is over this module's OWN packages only. A third-party or standard
// library package cannot import a package in this module, so restricting the
// graph to internal edges loses no path that could exist.

const (
	declPkg    = "github.com/yongjohnlee80/golib/decl"
	modulePath = "github.com/yongjohnlee80/golib"
	forbidden  = "github.com/yongjohnlee80/golib/tui"
)

func TestDeclDoesNotImportTheTUI(t *testing.T) {
	root := repoRoot(t)
	graph := moduleImportGraph(t, root)

	if _, ok := graph[declPkg]; !ok {
		t.Fatalf("no imports collected for %s; the guard is not looking at anything", declPkg)
	}

	// Breadth-first from decl, recording the path so a failure names the route
	// rather than only the destination.
	type step struct {
		pkg  string
		path []string
	}
	seen := map[string]bool{declPkg: true}
	queue := []step{{pkg: declPkg, path: []string{declPkg}}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, imp := range graph[cur.pkg] {
			if imp == forbidden || strings.HasPrefix(imp, forbidden+"/") {
				t.Errorf("the engine reaches the terminal UI:\n  %s\n\n"+
					"decl must stay toolkit-neutral; a toolkit type belongs in an adapter.",
					strings.Join(append(cur.path, imp), "\n    -> "))
				continue
			}
			if seen[imp] {
				continue
			}
			seen[imp] = true
			queue = append(queue, step{pkg: imp, path: append(append([]string{}, cur.path...), imp)})
		}
	}
}

// TestDeclLayeringGuardCanSeeTheForbiddenPackages is the positive control.
//
// A guard that walks an empty graph passes for the wrong reason, and this one
// would: if the package scan silently collected nothing, the test above would
// report success. So assert that the thing being forbidden is actually present
// and reachable in the graph from somewhere.
func TestDeclLayeringGuardCanSeeTheForbiddenPackages(t *testing.T) {
	root := repoRoot(t)
	graph := moduleImportGraph(t, root)

	var tuiPkgs, importers int
	for pkg, imports := range graph {
		if pkg == forbidden || strings.HasPrefix(pkg, forbidden+"/") {
			tuiPkgs++
		}
		for _, imp := range imports {
			if imp == forbidden || strings.HasPrefix(imp, forbidden+"/") {
				importers++
				break
			}
		}
	}
	if tuiPkgs == 0 {
		t.Error("the graph contains no tui packages at all; the guard is blind")
	}
	if importers == 0 {
		t.Error("nothing in the graph imports tui; the guard would pass even if decl did")
	}
}

// moduleImportGraph maps each package in this module to the packages it imports
// that are ALSO in this module. Test files are excluded: a test may legitimately
// import anything, and it is not part of what a consumer links.
func moduleImportGraph(t *testing.T, root string) map[string][]string {
	t.Helper()
	graph := map[string]map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := modulePath
		if rel != "." {
			pkg = modulePath + "/" + filepath.ToSlash(rel)
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// A file that will not parse is a real problem, but it is not this
			// guard's problem to report; the build catches it first.
			return nil
		}
		if graph[pkg] == nil {
			graph[pkg] = map[string]bool{}
		}
		for _, spec := range f.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			if imp == modulePath || strings.HasPrefix(imp, modulePath+"/") {
				graph[pkg][imp] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}

	out := make(map[string][]string, len(graph))
	for pkg, imports := range graph {
		list := make([]string, 0, len(imports))
		for imp := range imports {
			list = append(list, imp)
		}
		sort.Strings(list)
		out[pkg] = list
	}
	return out
}

// TestParseSubPackagesDependOnlyDownwards.
//
// parse/qml, parse/js and parse/sql all depend on parse; parse depends on NONE
// of them. The direction is what makes parse the place for things every format
// shares — a Scanner, a Position, a SyntaxError — without the shared layer
// acquiring an opinion about any particular grammar.
//
// It is asserted rather than stated because a split like this reverses by
// accident: one convenience helper in parse that reaches for a QML type, and
// the layering is gone with nothing to notice it. The sub-packages may not
// import each other either, except qml -> js, which is a real dependency: a
// QML property value IS a JavaScript expression.
func TestParseSubPackagesDependOnlyDownwards(t *testing.T) {
	const root = "github.com/yongjohnlee80/golib/parse"
	cases := []struct {
		pkg       string
		forbidden []string
	}{
		{"parse", []string{root + "/qml", root + "/js", root + "/sql"}},
		{"parse/js", []string{root + "/qml", root + "/sql"}},
		{"parse/sql", []string{root + "/qml", root + "/js"}},
		{"parse/qml", []string{root + "/sql"}},
	}
	for _, c := range cases {
		t.Run(c.pkg, func(t *testing.T) {
			graph := moduleImportGraph(t, repoRoot(t))
			full := "github.com/yongjohnlee80/golib/" + c.pkg
			imports, ok := graph[full]
			if !ok {
				t.Fatalf("no imports collected for %s; the guard is not looking at anything", full)
			}
			for _, imp := range imports {
				for _, bad := range c.forbidden {
					if imp == bad {
						t.Errorf("%s imports %s; the parse layering only points downwards",
							c.pkg, imp)
					}
				}
			}
		})
	}
}
