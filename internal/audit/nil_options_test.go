package audit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// THE NIL-OPTION GUARD.
//
// A constructor skips a nil option: a caller building its options conditionally ([]XOption{a,
// maybeB()}) passes nil where one does not apply, and a constructor that calls it panics far from
// the line that chose it. So every loop over a variadic ...XOption parameter calls the option only
// inside `if o != nil`.
//
// It covers the tree the tui architecture epic audited and owns: tui/, gui/ and parse/markdown/.
// The rest of golib was not part of that work; widening the guard there is its own change.
//
// WHAT IT CANNOT SEE: an option called through another variable (fn := o; fn(x)), or a loop over a
// slice copied from the parameter. It is a syntax check on the loop's own value, which is how every
// constructor here is written.

// nilOptionScope is the guarded tree, as repository-relative path prefixes.
var nilOptionScope = []string{"tui/", "gui/", "parse/markdown/"}

func TestConstructorsSkipANilOption(t *testing.T) {
	root := repoRoot(t)
	var found []string
	for _, pf := range parseTree(t, root) {
		if !inNilOptionScope(pf.rel) {
			continue
		}
		for _, pos := range unguardedOptionCalls(pf.fset, pf.file) {
			found = append(found, fmt.Sprintf("%s:%d", pf.rel, pos.Line))
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("%d loops call an option without skipping nil (wrap the call in `if o != nil`):\n  %s",
			len(found), strings.Join(found, "\n  "))
	}
}

func inNilOptionScope(rel string) bool {
	for _, p := range nilOptionScope {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// unguardedOptionCalls is every call of a loop value ranging over a variadic ...XOption parameter
// that is not inside `if v != nil`, in f's functions and function literals.
func unguardedOptionCalls(fset *token.FileSet, f *ast.File) []token.Position {
	var out []token.Position
	ast.Inspect(f, func(n ast.Node) bool {
		var ft *ast.FuncType
		var body *ast.BlockStmt
		switch x := n.(type) {
		case *ast.FuncDecl:
			ft, body = x.Type, x.Body
		case *ast.FuncLit:
			ft, body = x.Type, x.Body
		default:
			return true
		}
		if body == nil {
			return true
		}
		params := variadicOptionParams(ft)
		if len(params) == 0 {
			return true
		}
		ast.Inspect(body, func(m ast.Node) bool {
			rs, ok := m.(*ast.RangeStmt)
			if !ok {
				return true
			}
			src, ok := rs.X.(*ast.Ident)
			if !ok || !params[src.Name] {
				return true
			}
			v, ok := rs.Value.(*ast.Ident)
			if !ok {
				return true
			}
			for _, c := range callsOutsideNilCheck(rs.Body, v.Name) {
				out = append(out, fset.Position(c.Pos()))
			}
			return true
		})
		return true
	})
	return out
}

// variadicOptionParams is the names of ft's ...T parameters whose T is named *Option.
func variadicOptionParams(ft *ast.FuncType) map[string]bool {
	out := map[string]bool{}
	for _, f := range ft.Params.List {
		el, ok := f.Type.(*ast.Ellipsis)
		if !ok {
			continue
		}
		var name string
		switch t := el.Elt.(type) {
		case *ast.Ident:
			name = t.Name
		case *ast.SelectorExpr:
			name = t.Sel.Name
		}
		if strings.HasSuffix(name, "Option") {
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	return out
}

// callsOutsideNilCheck is every call of v in body that no enclosing `if v != nil` guards.
func callsOutsideNilCheck(body *ast.BlockStmt, v string) []*ast.CallExpr {
	var out []*ast.CallExpr
	var walk func(n ast.Node, guarded bool)
	walk = func(n ast.Node, guarded bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.IfStmt:
				walk(x.Body, guarded || isNotNil(x.Cond, v))
				if x.Else != nil {
					walk(x.Else, guarded)
				}
				return false
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == v && !guarded {
					out = append(out, x)
				}
			}
			return true
		})
	}
	walk(body, false)
	return out
}

// isNotNil reports whether cond is `v != nil`.
func isNotNil(cond ast.Expr, v string) bool {
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return false
	}
	x, xok := be.X.(*ast.Ident)
	y, yok := be.Y.(*ast.Ident)
	return xok && yok && x.Name == v && y.Name == "nil"
}

// The guard fires on a constructor that calls its option unguarded, and stays silent on one that
// skips nil, on a loop over something that is not a variadic option, and on a guarded call nested
// deeper.
func TestTheNilOptionGuardSeesWhatItClaims(t *testing.T) {
	const src = `package x

type Option func(*T)
type T struct{}

func Unguarded(opts ...Option) { t := &T{}; for _, o := range opts { o(t) } }

func Guarded(opts ...Option) { t := &T{}; for _, o := range opts { if o != nil { o(t) } } }

func Nested(opts ...Option) {
	t := &T{}
	for _, o := range opts {
		if o != nil {
			if true { o(t) }
		}
	}
}

func NotOptions(fs ...func()) { for _, f := range fs { f() } }

func ElseIsUnguarded(opts ...Option) {
	t := &T{}
	for _, o := range opts {
		if o != nil { o(t) } else { o(t) }
	}
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, p := range unguardedOptionCalls(fset, f) {
		lines = append(lines, p.Line)
	}
	if want := []int{6, 24}; fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Errorf("flagged lines %v, want %v (Unguarded, and ElseIsUnguarded's else)", lines, want)
	}
}
