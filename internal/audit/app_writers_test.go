package audit

import (
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// APP FIELD WRITERS. The map "The App's state, by owner" in tui/IMPLEMENTATION.md itemises every App
// field group: its owner, the files that write it, the writes another concern may make (Crosses).
// "Every write not listed is a change to this map." This guard makes that a gate. It reads the map
// itself, the one source of truth, type-checks package tui, and fails when a write to an App field
// lands in a file that no entry naming the field lists.
//
// An entry's files may write every App field the entry names in backticks, its Crosses included (a
// dotted name, `hover.keyFocus`, names its field, hover). A write is an assignment, an increment or
// a decrement, a delete or clear, or taking the address, whose target is a field of an App value,
// however it is reached (a.focus, c.app.focus.cur, a.nodes[id], &a.timing), and a keyed field in an
// App literal (NewApp's). A call of a pointer-receiver method on a field is not counted: whether it
// writes is the method's, and the Bus's Publish or the queue's drain, called where the map says
// they're used, would read as writes.
//
// Supplemental only: it proves where the writes are, not that they are right. The cells the map
// names stay the evidence for that.

// appWrite is one write to an App field: the field, the file (base name) and the line.
type appWrite struct {
	field, file string
	line        int
}

// appWritesIn type-checks package tui, with extra (base name → source) added as more of its files,
// and returns every write to an App field and the App's field names.
func appWritesIn(t *testing.T, root string, extra map[string]string) ([]appWrite, []string) {
	t.Helper()
	dir := filepath.Join(root, "tui")
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("tui: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range pkg.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	for _, name := range slices.Sorted(maps.Keys(extra)) {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), extra[name], 0)
		if err != nil {
			t.Fatalf("parse planted %s: %v", name, err)
		}
		files = append(files, f)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	tpkg, err := conf.Check("github.com/yongjohnlee80/golib/tui", fset, files, info)
	if err != nil {
		t.Fatalf("type-check tui: %v", err)
	}
	app, ok := tpkg.Scope().Lookup("App").Type().(*types.Named)
	if !ok {
		t.Fatal("tui has no App type")
	}
	st := app.Underlying().(*types.Struct)
	var fields []string
	for i := range st.NumFields() {
		fields = append(fields, st.Field(i).Name())
	}
	isApp := func(e ast.Expr) bool {
		tv := info.Types[e].Type
		if p, ok := tv.(*types.Pointer); ok {
			tv = p.Elem()
		}
		return types.Identical(tv, app)
	}
	// fieldOf is the App field e writes into: the innermost selector whose receiver is an App
	fieldOf := func(e ast.Expr) string {
		for e != nil {
			switch x := e.(type) {
			case *ast.SelectorExpr:
				if s := info.Selections[x]; s != nil && s.Kind() == types.FieldVal && isApp(x.X) {
					return x.Sel.Name
				}
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.StarExpr:
				e = x.X
			case *ast.ParenExpr:
				e = x.X
			default:
				return ""
			}
		}
		return ""
	}
	var writes []appWrite
	add := func(e ast.Expr) {
		if f := fieldOf(e); f != "" {
			p := fset.Position(e.Pos())
			writes = append(writes, appWrite{f, filepath.Base(p.Filename), p.Line})
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if x.Tok != token.DEFINE {
					for _, l := range x.Lhs {
						add(l)
					}
				}
			case *ast.IncDecStmt:
				add(x.X)
			case *ast.RangeStmt:
				if x.Tok == token.ASSIGN {
					add(x.Key)
					add(x.Value)
				}
			case *ast.UnaryExpr:
				if x.Op == token.AND {
					add(x.X)
				}
			case *ast.CompositeLit:
				if isApp(x) {
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if id, ok := kv.Key.(*ast.Ident); ok {
								p := fset.Position(kv.Pos())
								writes = append(writes, appWrite{id.Name, filepath.Base(p.Filename), p.Line})
							}
						}
					}
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "delete" || id.Name == "clear") && len(x.Args) > 0 {
					if _, builtin := info.Uses[id].(*types.Builtin); builtin {
						add(x.Args[0])
					}
				}
			}
			return true
		})
	}
	return writes, fields
}

// ownerMap reads the App's map in doc: for each App field, the files of every entry that names it.
func ownerMap(t *testing.T, doc string, fields []string) map[string]map[string]bool {
	t.Helper()
	start := strings.Index(doc, "\n### 6.1 ")
	if start < 0 {
		t.Fatal("IMPLEMENTATION.md has no §6.1")
	}
	sec := doc[start+1:]
	if end := regexp.MustCompile(`\n#{2,3} `).FindStringIndex(sec[4:]); end != nil {
		sec = sec[:end[0]+4]
	}
	isField := map[string]bool{}
	for _, f := range fields {
		isField[f] = true
	}
	tick := regexp.MustCompile("`([^`]+)`")
	owners := map[string]map[string]bool{}
	entries := 0
	for _, entry := range strings.Split(sec, "\n- **")[1:] {
		entries++
		var names []string
		files := map[string]bool{}
		for _, m := range tick.FindAllStringSubmatch(entry, -1) {
			tok := m[1]
			switch {
			case strings.HasSuffix(tok, "_test.go"):
			case strings.HasSuffix(tok, ".go"):
				files[tok] = true
			default:
				if name, _, _ := strings.Cut(tok, "."); isField[name] {
					names = append(names, name)
				}
			}
		}
		for _, n := range names {
			if owners[n] == nil {
				owners[n] = map[string]bool{}
			}
			for f := range files {
				owners[n][f] = true
			}
		}
	}
	if entries < 10 {
		t.Fatalf("§6.1 parsed into %d entries; the map itemises more (the parse is broken)", entries)
	}
	return owners
}

// strayWrites are the writes owners doesn't list: field, then file, sorted.
func strayWrites(owners map[string]map[string]bool, writes []appWrite) []appWrite {
	var out []appWrite
	for _, w := range writes {
		if !owners[w.field][w.file] {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].field != out[j].field {
			return out[i].field < out[j].field
		}
		return out[i].file < out[j].file || out[i].file == out[j].file && out[i].line < out[j].line
	})
	return out
}

func readImplementation(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "tui", "IMPLEMENTATION.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every App field is in an entry of the map, and every write to one lands in a file an entry naming
// it lists.
func TestAppFieldWritersAreTheMapsOwners(t *testing.T) {
	root := repoRoot(t)
	writes, fields := appWritesIn(t, root, nil)
	if len(writes) < 100 {
		t.Fatalf("the scan found %d writes to App fields; tui makes hundreds (the scan is not looking)", len(writes))
	}
	t.Logf("%d writes to %d App fields", len(writes), len(fields))
	owners := ownerMap(t, readImplementation(t, root), fields)
	for _, f := range fields {
		if owners[f] == nil {
			t.Errorf("App.%s is in no §6.1 entry: add it to its group, with its owner and writer files", f)
		}
	}
	for _, w := range strayWrites(owners, writes) {
		if owners[w.field] == nil {
			continue // said above
		}
		t.Errorf("App.%s is written in %s:%d, a file no §6.1 entry naming it lists (listed: %s): move the write to an owner, or change the map in the same change",
			w.field, w.file, w.line, strings.Join(slices.Sorted(maps.Keys(owners[w.field])), ", "))
	}
}

// The detector: a write planted in a file the map doesn't list is reported, naming the field and the
// file; with the file listed in the field's entry, it isn't.
func TestTheAppWriterGuardReportsAnUnlistedWrite(t *testing.T) {
	root := repoRoot(t)
	planted := map[string]string{"planted_write.go": "package tui\n\nfunc (a *App) plantedWrite() { a.focus = focusState{} }\n"}
	writes, fields := appWritesIn(t, root, planted)
	doc := readImplementation(t, root)
	isPlanted := func(w appWrite) bool { return w.field == "focus" && w.file == "planted_write.go" }
	if !slices.ContainsFunc(strayWrites(ownerMap(t, doc, fields), writes), isPlanted) {
		t.Fatal("a write to App.focus in an unlisted file was not reported")
	}
	listed := strings.Replace(doc, "Written in `focus.go`", "Written in `planted_write.go`, `focus.go`", 1)
	if listed == doc {
		t.Fatal("the Focus entry's writer list was not found to list the planted file in")
	}
	if slices.ContainsFunc(strayWrites(ownerMap(t, listed, fields), writes), isPlanted) {
		t.Error("a write to App.focus in a file the Focus entry lists was reported")
	}
}
