// Package facadegen writes tui/widget's facade over the packages its widgets moved into
// (tui/widget/core, tui/widget/terminal, …): a forward in package widget of every name such a
// package exports, so a program importing tui/widget compiles unchanged while the widgets move. A type is forwarded as an alias (its methods come with it), a
// func as a func that calls core's (a func stays a func, not a var), and a const or var as one
// equal to core's. Doc comments are copied, so go doc reads the same through either path.
//
// tui/widget runs it by go generate (facade.go); its test fails when a generated file differs from
// what Generate writes now, so a name a package exports later cannot go unforwarded.
package facadegen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Package is one package the facade forwards: its folder under tui/widget, its import path and its
// name.
type Package struct{ Dir, Path, Name string }

// Packages are the packages tui/widget forwards, in the order their facades are written.
var Packages = []Package{
	{"core", "github.com/yongjohnlee80/golib/tui/widget/core", "core"},
	{"terminal", "github.com/yongjohnlee80/golib/tui/widget/terminal", "terminal"},
	{"editor", "github.com/yongjohnlee80/golib/tui/widget/editor", "editor"},
	{"files", "github.com/yongjohnlee80/golib/tui/widget/files", "files"},
}

// Generate reads p's non-test Go files (dir, relative to tui/widget or absolute) and returns its
// facade's source for package widget, and a test's source (package widget_test) holding each
// forwarded var DeepEqual to the package's.
func Generate(dir string, p Package) (facade, varsTest []byte, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	g := &gen{fset: fset, imports: map[string]string{}, pkg: p}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ParseComments)
		if err != nil {
			return nil, nil, err
		}
		if err := g.file(f); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", n, err)
		}
	}
	return g.facade()
}

type gen struct {
	pkg     Package
	fset    *token.FileSet
	imports map[string]string // a name the forwards use -> its import path
	types   []string
	funcs   []string
	consts  []string
	vars    []string
	varName []string
	file0   map[string]string // the current file's imports, by name
	tp      map[string]bool   // the current declaration's type parameters
}

func (g *gen) file(f *ast.File) error {
	g.file0 = map[string]string{}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		g.file0[name] = path
	}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv != nil || !x.Name.IsExported() {
				continue
			}
			if err := g.fn(x); err != nil {
				return err
			}
		case *ast.GenDecl:
			for _, s := range x.Specs {
				doc := x.Doc
				if len(x.Specs) > 1 || x.Lparen.IsValid() {
					doc = nil // a group's doc is about the group; each spec keeps its own
				}
				switch y := s.(type) {
				case *ast.TypeSpec:
					if y.Doc != nil {
						doc = y.Doc
					}
					if y.Name.IsExported() {
						if err := g.typ(y, doc); err != nil {
							return err
						}
					}
				case *ast.ValueSpec:
					if y.Doc != nil {
						doc = y.Doc
					}
					for _, n := range y.Names {
						if !n.IsExported() {
							continue
						}
						line := g.comment(doc) + n.Name + " = " + g.pkg.Name + "." + n.Name
						if x.Tok == token.CONST {
							g.consts = append(g.consts, line)
						} else {
							g.vars = append(g.vars, line)
							g.varName = append(g.varName, n.Name)
						}
						doc = nil // one doc for a spec of several names
					}
				}
			}
		}
	}
	return nil
}

// comment is doc as comment lines, each tab-free, ending in a newline; "" for none.
func (g *gen) comment(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range doc.List {
		b.WriteString(c.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// expr prints e as the forward writes it, recording the imports it names; a lowercase name of
// core's own would not compile in package widget, so it is an error.
func (g *gen) expr(e ast.Expr) (string, error) {
	var bad error
	names := map[*ast.Ident]bool{} // a func type's parameter names, a struct's field names: not types
	ast.Inspect(e, func(n ast.Node) bool {
		if f, ok := n.(*ast.Field); ok {
			for _, id := range f.Names {
				names[id] = true
			}
		}
		return true
	})
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				if path, ok := g.file0[id.Name]; ok {
					if prev, seen := g.imports[id.Name]; seen && prev != path {
						bad = fmt.Errorf("%s names both %s and %s", id.Name, prev, path)
					}
					g.imports[id.Name] = path
				}
			}
			return false
		case *ast.Ident:
			if !x.IsExported() && !predeclared[x.Name] && !names[x] && !g.tp[x.Name] {
				bad = fmt.Errorf("an exported declaration names %s, which core does not export", x.Name)
			}
		}
		return true
	})
	if bad != nil {
		return "", bad
	}
	var b bytes.Buffer
	if err := printer.Fprint(&b, g.fset, e); err != nil {
		return "", err
	}
	return b.String(), nil
}

var predeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true, "int16": true,
	"int32": true, "int64": true, "rune": true, "string": true, "uint": true, "uint8": true,
	"uint16": true, "uint32": true, "uint64": true, "uintptr": true, "nil": true, "true": true,
	"false": true, "iota": true, "_": true,
}

// tparams is a declaration's type parameters as declared, and as arguments.
func (g *gen) tparams(fl *ast.FieldList) (decl, args string, err error) {
	g.tp = map[string]bool{}
	if fl == nil || len(fl.List) == 0 {
		return "", "", nil
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			g.tp[n.Name] = true
		}
	}
	var ds, as []string
	for _, f := range fl.List {
		c, err := g.expr(f.Type)
		if err != nil {
			return "", "", err
		}
		var ns []string
		for _, n := range f.Names {
			ns = append(ns, n.Name)
			as = append(as, n.Name)
		}
		ds = append(ds, strings.Join(ns, ", ")+" "+c)
	}
	return "[" + strings.Join(ds, ", ") + "]", "[" + strings.Join(as, ", ") + "]", nil
}

func (g *gen) typ(s *ast.TypeSpec, doc *ast.CommentGroup) error {
	decl, args, err := g.tparams(s.TypeParams)
	if err != nil {
		return err
	}
	g.types = append(g.types, g.comment(doc)+"type "+s.Name.Name+decl+" = "+g.pkg.Name+"."+s.Name.Name+args)
	return nil
}

func (g *gen) fn(d *ast.FuncDecl) error {
	decl, args, err := g.tparams(d.Type.TypeParams)
	if err != nil {
		return err
	}
	var params, call []string
	i := 0
	for _, f := range d.Type.Params.List {
		t, err := g.expr(f.Type)
		if err != nil {
			return err
		}
		_, variadic := f.Type.(*ast.Ellipsis)
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		var ns []string
		for range n {
			name := fmt.Sprintf("a%d", i)
			i++
			ns = append(ns, name)
			if variadic {
				call = append(call, name+"...")
			} else {
				call = append(call, name)
			}
		}
		params = append(params, strings.Join(ns, ", ")+" "+t)
	}
	var results []string
	if d.Type.Results != nil {
		for _, f := range d.Type.Results.List {
			t, err := g.expr(f.Type)
			if err != nil {
				return err
			}
			n := max(len(f.Names), 1)
			for range n {
				results = append(results, t)
			}
		}
	}
	res := ""
	switch len(results) {
	case 0:
	case 1:
		res = " " + results[0]
	default:
		res = " (" + strings.Join(results, ", ") + ")"
	}
	body := g.pkg.Name + "." + d.Name.Name + args + "(" + strings.Join(call, ", ") + ")"
	if len(results) > 0 {
		body = "return " + body
	}
	g.funcs = append(g.funcs, g.comment(d.Doc)+"func "+d.Name.Name+decl+"("+strings.Join(params, ", ")+")"+res+" {\n\t"+body+"\n}")
	return nil
}

func (g *gen) facade() (facade, varsTest []byte, err error) {
	var b strings.Builder
	b.WriteString("// Code generated by tui/widget/internal/facadegen; DO NOT EDIT.\n\npackage widget\n\nimport (\n")
	b.WriteString("\t\"" + g.pkg.Path + "\"\n")
	var names []string
	for n := range g.imports {
		if g.imports[n] != g.pkg.Path {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		path := g.imports[n]
		if path[strings.LastIndex(path, "/")+1:] == n {
			b.WriteString("\t\"" + path + "\"\n")
		} else {
			b.WriteString("\t" + n + " \"" + path + "\"\n")
		}
	}
	b.WriteString(")\n\n")
	for _, t := range g.types {
		b.WriteString(t + "\n\n")
	}
	if len(g.consts) > 0 {
		b.WriteString("const (\n")
		for _, c := range g.consts {
			b.WriteString(indent(c) + "\n")
		}
		b.WriteString(")\n\n")
	}
	if len(g.vars) > 0 {
		b.WriteString("// The forwarded vars are " + g.pkg.Name + "'s values, copied: read them, never assign one, since\n// " + g.pkg.Name + "'s widgets read " + g.pkg.Name + "'s.\nvar (\n")
		for _, v := range g.vars {
			b.WriteString(indent(v) + "\n")
		}
		b.WriteString(")\n\n")
	}
	for _, f := range g.funcs {
		b.WriteString(f + "\n\n")
	}
	facade, err = format.Source([]byte(b.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("the facade does not format: %w", err)
	}
	var t strings.Builder
	title := strings.ToUpper(g.pkg.Name[:1]) + g.pkg.Name[1:]
	t.WriteString("// Code generated by tui/widget/internal/facadegen; DO NOT EDIT.\n\npackage widget_test\n\nimport (\n\t\"reflect\"\n\t\"testing\"\n\n\t\"github.com/yongjohnlee80/golib/tui/widget\"\n\t\"" + g.pkg.Path + "\"\n)\n\n")
	t.WriteString("// TestTheFacadeForwards" + title + "Vars holds each var tui/widget forwards from " + g.pkg.Name + " equal to\n// " + g.pkg.Name + "'s, so a later change there cannot leave a stale copy behind.\nfunc TestTheFacadeForwards" + title + "Vars(t *testing.T) {\n\tfor name, v := range map[string][2]any{\n")
	for _, n := range g.varName {
		t.WriteString("\t\t" + strconv.Quote(n) + ": {widget." + n + ", " + g.pkg.Name + "." + n + "},\n")
	}
	t.WriteString("\t} {\n\t\tif !reflect.DeepEqual(v[0], v[1]) {\n\t\t\tt.Errorf(\"widget.%s is not " + g.pkg.Name + ".%s\", name, name)\n\t\t}\n\t}\n}\n")
	varsTest, err = format.Source([]byte(t.String()))
	if err != nil {
		return nil, nil, fmt.Errorf("the vars test does not format: %w", err)
	}
	return facade, varsTest, nil
}

// indent is s, a forward and its doc, one tab in, for a const or var block.
func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "\t" + l
	}
	return strings.Join(lines, "\n")
}
