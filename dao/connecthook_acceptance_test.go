package dao

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests hold the connect-hook capability to the shape it was accepted
// in. Each one guards a decision that is invisible in the code itself — the
// absence of something. An absence needs a test, because nothing else will
// notice when it stops being absent.

// repoFiles returns every non-test .go file in the repository, including the
// nested dao/bigquery module, with paths relative to the repo root.
func repoFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil // a file that does not parse cannot be hiding a call
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = f
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repo: %v", err)
	}
	if len(out) < 50 {
		t.Fatalf("walked only %d source files; the walk is broken and every "+
			"absence assertion below would pass vacuously", len(out))
	}
	return out
}

// A1. The capability is two plain types. The withdrawn design — a
// ConnectHooker interface probed at a call site, plus a SupportsConnectHook
// helper — must not come back unnoticed, so its names are asserted absent
// rather than merely not written.
func TestAcceptance_DaoExportsNoCapabilityProbe(t *testing.T) {
	banned := regexp.MustCompile(`^(ConnectHooker|SupportsConnectHook)$`)

	var found []string
	for rel, f := range repoFiles(t) {
		if filepath.Dir(rel) != "dao" {
			continue
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.GenDecl:
				for _, sp := range decl.Specs {
					if ts, ok := sp.(*ast.TypeSpec); ok && banned.MatchString(ts.Name.Name) {
						found = append(found, rel+": type "+ts.Name.Name)
					}
				}
			case *ast.FuncDecl:
				if decl.Recv == nil && banned.MatchString(decl.Name.Name) {
					found = append(found, rel+": func "+decl.Name.Name)
				}
			}
		}
	}
	if len(found) > 0 {
		t.Errorf("dao exports the withdrawn capability-probe surface:\n  %s\n"+
			"The hook is named by the driver package that supports it "+
			"(postgres.WithConnectHook, mysql/sqlite.OpenHooked), so support is a "+
			"compile-time fact and needs no runtime probe.", strings.Join(found, "\n  "))
	}
}

// A3(i). modernc.org/sqlite ships RegisterConnectionHook, which looks exactly
// like this feature and is a trap: it appends to a process-global slice on a
// package singleton, so a hook registered for one dao.Open would fire for
// EVERY sqlite connection anywhere in the process — including other libraries'
// — and it can never be removed. golib must never call it.
func TestAcceptance_GolibNeverCallsSqliteRegisterConnectionHook(t *testing.T) {
	var found []string
	for rel, f := range repoFiles(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "RegisterConnectionHook" {
				found = append(found, rel)
			}
			return true
		})
	}
	if len(found) > 0 {
		t.Errorf("golib calls RegisterConnectionHook in:\n  %s\n"+
			"That registration is process-global and permanent. The connector "+
			"seam in dao/internal/stdsql is per-DataConn and is the supported "+
			"way to run a hook.", strings.Join(found, "\n  "))
	}
}

// A4. bigquery does not support connect hooks, and the guarantee is a COMPILE
// ERROR rather than a runtime error — so there is no behaviour to assert, only
// the absence of the symbols that would make it compile.
func TestAcceptance_BigQueryDeclaresNoHookSurface(t *testing.T) {
	banned := map[string]bool{"WithConnectHook": true, "OpenHooked": true}

	var found []string
	sawPackage := false
	for rel, f := range repoFiles(t) {
		if !strings.HasPrefix(rel, "dao/bigquery/") {
			continue
		}
		sawPackage = true
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && banned[fd.Name.Name] {
				found = append(found, rel+": func "+fd.Name.Name)
			}
		}
	}
	// Vacuity guard: this test is an absence assertion over a directory. If
	// the directory were not walked at all it would pass while checking
	// nothing.
	if !sawPackage {
		t.Fatal("no dao/bigquery source was walked; the assertion below is vacuous")
	}
	if len(found) > 0 {
		t.Errorf("dao/bigquery declares a connect-hook surface:\n  %s\n"+
			"bigquery has no connection pool to hook; the guarantee is that "+
			"calling one of these does not compile.", strings.Join(found, "\n  "))
	}
}
