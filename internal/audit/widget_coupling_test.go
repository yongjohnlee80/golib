package audit

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE WIDGET COUPLING INVENTORY.
//
// tui/widget is to split into a core package and five component packages
// (editor, table, files, terminal, htmlview). The split is blocked by how much
// each group reaches into another's unexported names: after it, only exported
// API can cross a package boundary. A regex could only bound that from above
// (it counts same-named locals too), so this inventory type-checks the package
// and lists every unexported package-level name, method or field that a file of
// one group uses and a file of another group declares.
//
// Its ledger is the work list for decoupling core from the components and for
// the split itself. It is exact in both directions: a new crossing fails until
// it is recorded (so the coupling cannot grow unnoticed), and a crossing that is
// gone fails until its row is removed (so the list only shrinks). Empty is the
// destination; after the split this guard holds the same line at package
// boundaries.

const (
	widgetPkg          = modulePath + "/tui/widget"
	widgetCouplingPath = "testdata/widget_coupling.txt"
	widgetCouplingEnv  = "GOLIB_WIDGET_COUPLING_UPDATE"
)

// widgetGroups assign each file of tui/widget to a group by its name's prefix,
// the longest prefix winning; a file no prefix names is core's.
var widgetGroups = map[string]string{
	"editor": "editor", "commandline": "editor", "textbuffer": "editor", "buffer": "editor",
	"keygrammar": "editor", "jumplist": "editor", "viewmark": "editor", "edithistory": "editor",
	"editregister": "editor",
	"table":        "table", "datatable": "table",
	"filelist": "files", "fileviews": "files", "filesource": "files", "filepreview": "files",
	"filepath": "files", "tree": "files",
	"terminal": "terminal",
	"htmlview": "htmlview", "htmlimages": "htmlview", "htmldoc": "htmlview", "raster": "htmlview",
	"image": "htmlview",
}

// widgetGroupOf is the group of the file named base.
func widgetGroupOf(base string) string {
	best, group := "", "core"
	for prefix, g := range widgetGroups {
		if strings.HasPrefix(base, prefix) && len(prefix) > len(best) {
			best, group = prefix, g
		}
	}
	return group
}

// crossing is one unexported object one group uses and another declares.
type crossing struct {
	from, owner, name, kind string
}

func (c crossing) row() string { return c.from + "\t" + c.owner + "\t" + c.name + "\t" + c.kind }

// widgetCrossings type-checks tui/widget's non-test files (its dependencies come
// from the build's export data) and answers every crossing, sorted, and how many
// files it read.
func widgetCrossings(t *testing.T, root string) ([]crossing, int) {
	t.Helper()
	exports := map[string]string{}
	cmd := exec.Command("go", "list", "-deps", "-export", "-f", "{{.ImportPath}}\t{{.Export}}", "./tui/widget")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -export ./tui/widget: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if path, file, ok := strings.Cut(line, "\t"); ok && file != "" {
			exports[path] = file
		}
	}
	// the files the build compiles for this platform (build constraints applied), tests excluded
	cmd = exec.Command("go", "list", "-f", "{{range .GoFiles}}{{.}}\n{{end}}", "./tui/widget")
	cmd.Dir = root
	listed, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./tui/widget: %v", err)
	}
	fset := token.NewFileSet()
	dir := filepath.Join(root, "tui", "widget")
	var files []*ast.File
	for _, base := range strings.Fields(string(listed)) {
		name := filepath.Join(dir, base)
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	pkg, err := (&types.Config{Importer: imp}).Check(widgetPkg, fset, files, info)
	if err != nil {
		t.Fatalf("type-checking %s: %v", widgetPkg, err)
	}
	groupAt := func(pos token.Pos) string { return widgetGroupOf(filepath.Base(fset.Position(pos).Filename)) }
	// each package-level struct's fields, by the struct's name, so a field reads Type.field
	fieldOf := map[*types.Var]string{}
	for _, n := range pkg.Scope().Names() {
		if tn, ok := pkg.Scope().Lookup(n).(*types.TypeName); ok {
			if st, ok := tn.Type().Underlying().(*types.Struct); ok {
				for i := range st.NumFields() {
					fieldOf[st.Field(i)] = tn.Name()
				}
			}
		}
	}
	seen := map[crossing]bool{}
	for id, obj := range info.Uses {
		if obj.Pkg() != pkg || obj.Exported() || !obj.Pos().IsValid() {
			continue
		}
		name, kind, ok := widgetObject(obj, fieldOf)
		if !ok {
			continue // a local: never crosses a file
		}
		from, owner := groupAt(id.Pos()), groupAt(obj.Pos())
		if from != owner {
			seen[crossing{from: from, owner: owner, name: name, kind: kind}] = true
		}
	}
	list := make([]crossing, 0, len(seen))
	for c := range seen {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].row() < list[j].row() })
	return list, len(files)
}

// widgetObject names an object a package boundary would hide, with its kind: a
// package-level name, or a method or field of a named type (named Type.name); ok is false for a
// local, a parameter or a label.
func widgetObject(obj types.Object, fieldOf map[*types.Var]string) (name, kind string, ok bool) {
	switch o := obj.(type) {
	case *types.Func:
		if sig, _ := o.Type().(*types.Signature); sig != nil && sig.Recv() != nil {
			return widgetRecvName(sig.Recv().Type()) + "." + o.Name(), "method", true
		}
		return o.Name(), "func", o.Parent() == o.Pkg().Scope()
	case *types.Var:
		if o.IsField() {
			if owner, ok := fieldOf[o.Origin()]; ok {
				return owner + "." + o.Name(), "field", true
			}
			return o.Name(), "field", true // a field of an unnamed struct
		}
		return o.Name(), "var", o.Parent() == o.Pkg().Scope()
	case *types.Const:
		return o.Name(), "const", o.Parent() == o.Pkg().Scope()
	case *types.TypeName:
		return o.Name(), "type", o.Parent() == o.Pkg().Scope()
	}
	return "", "", false
}

// widgetRecvName is a receiver's type name, without pointer or type arguments.
func widgetRecvName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.Obj().Name()
	}
	return t.String()
}

const widgetCouplingHeader = `# tui/widget's coupling: each unexported name, method or field one group of
# files uses and another group declares (widget_coupling_test.go assigns the
# groups by file name).
#
#   using group <TAB> owning group <TAB> name <TAB> kind
#
# The work list for splitting tui/widget into core and its components: a row
# goes when its use is decoupled (an interface core owns, a callback, an export).
# This list may only SHRINK. Empty is the destination.
#
# Regenerate: GOLIB_WIDGET_COUPLING_UPDATE=1 go test -run TestWidgetCoupling ./internal/audit/
`

// TestWidgetCouplingMatchesTheInventory fails in both directions: a crossing not
// recorded, or a recorded one that is gone.
func TestWidgetCouplingMatchesTheInventory(t *testing.T) {
	root := repoRoot(t)
	found, nfiles := widgetCrossings(t, root)
	if nfiles < 100 || len(found) == 0 {
		t.Fatalf("read %d files and found %d crossings: the inventory is not looking at tui/widget", nfiles, len(found))
	}
	ledger := filepath.Join(root, "internal", "audit", widgetCouplingPath)
	if os.Getenv(widgetCouplingEnv) == "1" {
		var b strings.Builder
		b.WriteString(widgetCouplingHeader)
		for _, c := range found {
			b.WriteString(c.row() + "\n")
		}
		if err := os.WriteFile(ledger, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("%s regenerated with %d rows: review the diff and run again without %s", widgetCouplingPath, len(found), widgetCouplingEnv)
	}
	b, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("%v: generate it with %s=1", err, widgetCouplingEnv)
	}
	recorded := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			recorded[line] = true
		}
	}
	var added []string
	for _, c := range found {
		if !recorded[c.row()] {
			added = append(added, c.row())
		}
		delete(recorded, c.row())
	}
	gone := make([]string, 0, len(recorded))
	for row := range recorded {
		gone = append(gone, row)
	}
	sort.Strings(gone)
	if len(added) > 0 {
		t.Errorf("%d new crossing(s) of tui/widget's groups, each an unexported name another group declares:\n  %s\n"+
			"Use the owning group's exported API, or move the code; the coupling may not grow.",
			len(added), strings.Join(added, "\n  "))
	}
	if len(gone) > 0 {
		t.Errorf("%d recorded crossing(s) are gone: remove them from %s (the list only shrinks):\n  %s",
			len(gone), widgetCouplingPath, strings.Join(gone, "\n  "))
	}
	summary := map[[2]string]int{}
	for _, c := range found {
		summary[[2]string{c.from, c.owner}]++
	}
	keys := make([][2]string, 0, len(summary))
	for k := range summary {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		t.Logf("%-9s uses %3d of %s's", k[0], summary[k], k[1])
	}
}

// TestWidgetGroupsAssignByLongestPrefix is the grouping's control: a file whose
// name extends a shorter group's prefix goes to the longer one, and a file no
// prefix names is core's.
func TestWidgetGroupsAssignByLongestPrefix(t *testing.T) {
	for base, want := range map[string]string{
		"editorcore.go": "editor", "datatable.go": "table", "tablecore_render.go": "table",
		"filepreview.go": "files", "htmlview.go": "htmlview", "terminal_vt.go": "terminal",
		"menu.go": "core", "textinput.go": "core", "tabs.go": "core",
	} {
		if got := widgetGroupOf(base); got != want {
			t.Errorf("widgetGroupOf(%q) = %s, want %s", base, got, want)
		}
	}
}
