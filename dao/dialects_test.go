package dao

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The guards below are STRUCTURAL: they read the syntax tree of the whole
// repository rather than importing the drivers. Two reasons, and the second is
// not a preference:
//
//   - dao cannot import dao/postgres and friends — the dependency runs the
//     other way — so a test that resolved dialects by value could not see them.
//   - dao/bigquery is a SEPARATE MODULE and does not currently build
//     (grpc v1.81.1 wants http2.TrailerPrefix). Parsing does not need a build,
//     so these guards cover the one dialect that no compiler in this repo is
//     currently checking. That is exactly where an unnoticed literal would
//     survive.
//
// (The repo walk here duplicates one in internal/audit on another branch. They
// merge into a shared helper once both land; noting it rather than leaving it
// silent, since de-duplication is the sweep these guards belong to.)

func repoRootT(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// dao/ sits directly under the module root.
	root := filepath.Dir(wd)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

type parsedGo struct {
	rel  string
	file *ast.File
	fset *token.FileSet
}

func parseRepo(t *testing.T, root string) []parsedGo {
	t.Helper()
	var out []parsedGo
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		out = append(out, parsedGo{rel: filepath.ToSlash(rel), file: f, fset: fset})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("parsed no files — the walk is broken, not the tree clean")
	}
	return out
}

// declaredDialectConsts reads dao/dialects.go and returns name -> value for
// every Dialect* constant. The declarations are the source of truth; nothing
// in this file restates their values.
func declaredDialectConsts(t *testing.T, files []parsedGo) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, pf := range files {
		if pf.rel != "dao/dialects.go" {
			continue
		}
		for _, d := range pf.file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					if !strings.HasPrefix(n.Name, "Dialect") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", n.Name, err)
					}
					got[n.Name] = v
				}
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("found no Dialect* constants in dao/dialects.go — the reader is broken")
	}
	return got
}

// dialectNameReturns finds every `func (XDialect) Name() string` in the repo
// and returns the constant identifier it returns, or "" plus the literal if it
// returns one.
func dialectNameReturns(t *testing.T, files []parsedGo) (byConst map[string][]string, literals []string) {
	t.Helper()
	byConst = map[string][]string{}
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "Name" || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			recv := recvTypeName(fd.Recv.List[0].Type)
			if !strings.HasSuffix(recv, "Dialect") {
				continue
			}
			ret := soleReturn(fd)
			if ret == nil {
				continue
			}
			where := pf.rel + " " + recv
			switch e := ret.(type) {
			case *ast.BasicLit:
				if e.Kind == token.STRING {
					literals = append(literals, where+" returns the literal "+e.Value)
				}
			case *ast.Ident:
				byConst[e.Name] = append(byConst[e.Name], where)
			case *ast.SelectorExpr:
				byConst[e.Sel.Name] = append(byConst[e.Sel.Name], where)
			default:
				literals = append(literals, where+" returns an unrecognised expression")
			}
		}
	}
	return byConst, literals
}

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func soleReturn(fd *ast.FuncDecl) ast.Expr {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return nil
	}
	rs, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(rs.Results) != 1 {
		return nil
	}
	return rs.Results[0]
}

// TestDialectNames_EveryDialectReturnsAConstant is the forward direction: no
// dialect may hand back a bare string.
func TestDialectNames_EveryDialectReturnsAConstant(t *testing.T) {
	files := parseRepo(t, repoRootT(t))
	consts := declaredDialectConsts(t, files)
	byConst, literals := dialectNameReturns(t, files)

	if len(byConst) == 0 && len(literals) == 0 {
		t.Fatal("found no dialect Name() methods at all — the finder is broken")
	}
	sort.Strings(literals)
	if len(literals) > 0 {
		t.Errorf("%d dialect Name() method(s) return a literal instead of a dao.Dialect* "+
			"constant:\n  %s", len(literals), strings.Join(literals, "\n  "))
	}
	var unknown []string
	for name, sites := range byConst {
		if _, ok := consts[name]; !ok {
			unknown = append(unknown, name+" (returned by "+strings.Join(sites, ", ")+")")
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Errorf("%d dialect Name() method(s) return an identifier that is not a declared "+
			"Dialect* constant:\n  %s", len(unknown), strings.Join(unknown, "\n  "))
	}
	t.Logf("%d dialect Name() methods, all returning declared constants", len(byConst))
}

// TestDialectNames_EveryConstantIsUsed is the reverse direction. Without it a
// constant could be declared, never returned by any dialect, and the forward
// check would stay green — dead surface with a doc comment, which golib.md
// forbids.
func TestDialectNames_EveryConstantIsUsed(t *testing.T) {
	files := parseRepo(t, repoRootT(t))
	consts := declaredDialectConsts(t, files)
	byConst, _ := dialectNameReturns(t, files)

	var unused []string
	for name := range consts {
		if len(byConst[name]) == 0 {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("%d declared Dialect* constant(s) are returned by no dialect in this repo — "+
			"either a dialect is missing or the constant is dead surface:\n  %s",
			len(unused), strings.Join(unused, ", "))
	}
	// And no constant may be claimed by two different dialects.
	var shared []string
	for name, sites := range byConst {
		if len(sites) > 1 {
			shared = append(shared, name+": "+strings.Join(sites, ", "))
		}
	}
	sort.Strings(shared)
	if len(shared) > 0 {
		t.Errorf("%d constant(s) are returned by more than one dialect — two engines cannot "+
			"share a name:\n  %s", len(shared), strings.Join(shared, "\n  "))
	}
	t.Logf("%d declared constants, each returned by exactly one dialect", len(consts))
}

// TestDialectNames_EngineDialectsIsDerived checks EngineDialects() against the
// DECLARATIONS rather than against a second hand-written list. Generic is the
// one exclusion and it is named, not silently dropped.
func TestDialectNames_EngineDialectsIsDerived(t *testing.T) {
	files := parseRepo(t, repoRootT(t))
	consts := declaredDialectConsts(t, files)

	want := map[string]bool{}
	for name, val := range consts {
		if name == "DialectGeneric" {
			continue
		}
		want[val] = true
	}
	got := map[string]bool{}
	for _, v := range EngineDialects() {
		if got[v] {
			t.Errorf("EngineDialects() lists %q twice", v)
		}
		got[v] = true
	}
	for v := range want {
		if !got[v] {
			t.Errorf("EngineDialects() omits %q, which is a declared non-generic dialect "+
				"constant — a new engine must be added to the list, not just declared", v)
		}
	}
	for v := range got {
		if !want[v] {
			t.Errorf("EngineDialects() lists %q, which is not a declared non-generic dialect "+
				"constant", v)
		}
	}
	if !want[DialectPostgres] {
		t.Fatal("the derivation produced no postgres entry — the reader is broken, not the list")
	}
	t.Logf("EngineDialects() = %v, derived from %d declared constants minus generic",
		EngineDialects(), len(consts))
}

// TestDialectNames_NoStrayLiterals is the de-duplication guard: an engine name
// written as a string anywhere else in non-test code is the duplication this
// change removes, and it would otherwise creep straight back.
//
// One exception, and it is NAMED rather than skipped: sql.Open's first
// argument is the THIRD-PARTY DRIVER's database/sql registration name, owned
// by go-sql-driver/mysql and modernc.org/sqlite. Its equality with two of our
// dialect names is a coincidence, not a contract, so it must NOT be replaced
// by a dao constant.
func TestDialectNames_NoStrayLiterals(t *testing.T) {
	files := parseRepo(t, repoRootT(t))
	consts := declaredDialectConsts(t, files)
	isName := map[string]bool{}
	for _, v := range consts {
		isName[v] = true
	}

	// Positions of sql.Open's first argument — the named exception.
	allowed := map[token.Pos]bool{}
	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Open" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "sql" {
				allowed[call.Args[0].Pos()] = true
			}
			return true
		})
	}

	var stray []string
	for _, pf := range files {
		if pf.rel == "dao/dialects.go" {
			continue // the declarations themselves
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || allowed[lit.Pos()] {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || !isName[v] {
				return true
			}
			stray = append(stray, pf.rel+":"+strconv.Itoa(pf.fset.Position(lit.Pos()).Line)+
				" "+lit.Value)
			return true
		})
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Errorf("%d engine-name literal(s) outside dao/dialects.go — use the constant, or if "+
			"this is a third-party driver registration name, add it to the named exception "+
			"in this test with the reason:\n  %s", len(stray), strings.Join(stray, "\n  "))
	}
	// The exception must actually be exercised, or it is untested cover for
	// whatever drifts into it later.
	if len(allowed) == 0 {
		t.Error("the sql.Open exception matched nothing — either the drivers stopped calling " +
			"sql.Open, or the matcher is broken and is exempting nothing")
	}
	t.Logf("no stray engine-name literals; %d sql.Open registration argument(s) exempted",
		len(allowed))
}
