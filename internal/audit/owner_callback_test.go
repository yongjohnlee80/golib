package audit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE OWNER-CALLBACK CONTINUATION GUARD.
//
// A row view tells its owner of a cursor move, an activation, a sort, a dropped edit through a
// callback (WithListOnCursor, TableOnCursor, WithTreeOnExpand, …), and the owner may replace or
// refresh the rows right there: the callback's contract says so. A view operation that goes on
// working after such a call therefore works on rows that may be gone — golib #401 found a press
// pairing, asking a menu and starting a drag over the replacement rows, each a separate site.
//
// The rule, and this guard, are one: in the row views' methods, a call that can reach an owner
// callback is either the operation's last step (in TAIL position: nothing but a return follows it,
// in its block and every block around it) or a SURVIVAL call whose result is used: a method whose
// result is declared `live bool`, reporting through the view's pressPairer epoch whether the rows
// survived (cursorTo, changed, expandNode, …). Any other result — a yank's ok, say — says nothing
// about the rows, and is no licence to go on (golib #401 r2: Cut went on after yankFrom's copy
// callback replaced the source). A function literal is its own tail: the step a Survives call runs.
// The guard is structural; the callback-replacement tests are its behavioural counterpart.
//
// "Can reach an owner callback" is computed, not listed: a method that calls one of the view's on*
// hooks (c.onCursor, t.on.expand) reaches one, and so does every method that calls such a method.

// ownerCallbackViews are the receivers whose owners may replace the rows from a callback.
var ownerCallbackViews = map[string]bool{"List": true, "Tree": true, "TableCore": true, "DataTable": true, "TreeNode": true}

// ownerCallbackHops are the receivers that reach another view through a field: a DataTable its core,
// a TreeNode its Tree.
var ownerCallbackHops = map[string]struct{ field, typ string }{"DataTable": {"core", "TableCore"}, "TreeNode": {"owner", "Tree"}}

// ownerCallbackFiles are the files those views' methods live in.
func ownerCallbackFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, glob := range []string{"list*.go", "tree*.go", "tablecore*.go", "datatable*.go"} {
		m, err := filepath.Glob(filepath.Join(root, "tui", "widget", glob))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range m {
			if !strings.HasSuffix(f, "_test.go") {
				out = append(out, f)
			}
		}
	}
	return out
}

// ownerRecv is the receiver's type name (List for *List[T]) and its variable.
func ownerRecv(fd *ast.FuncDecl) (typ, v string) {
	if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return "", ""
	}
	e := fd.Recv.List[0].Type
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	switch x := e.(type) {
	case *ast.IndexExpr:
		e = x.X
	case *ast.IndexListExpr:
		e = x.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name, fd.Recv.List[0].Names[0].Name
	}
	return "", ""
}

// calledMethod is the method a call reaches on the receiver v, or through its hop field: "List.X"
// for l.X() in a List method, "TableCore.X" for t.core.X() in a DataTable's. ok is false for any
// other call.
func calledMethod(call *ast.CallExpr, typ, v string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	switch x := sel.X.(type) {
	case *ast.Ident:
		if x.Name == v {
			return typ + "." + sel.Sel.Name, true
		}
	case *ast.SelectorExpr:
		if hop, ok := ownerCallbackHops[typ]; ok {
			if id, ok := x.X.(*ast.Ident); ok && id.Name == v && x.Sel.Name == hop.field {
				return hop.typ + "." + sel.Sel.Name, true
			}
		}
	}
	return "", false
}

// isHook reports whether call invokes one of the receiver's owner hooks: v.onX(…) or v.on.x(…).
func isHook(call *ast.CallExpr, v string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch x := sel.X.(type) {
	case *ast.Ident:
		n := sel.Sel.Name
		return x.Name == v && len(n) > 2 && strings.HasPrefix(n, "on") && n[2] >= 'A' && n[2] <= 'Z'
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == v && x.Sel.Name == "on"
	}
	return false
}

type ownerMethod struct {
	typ, v string
	decl   *ast.FuncDecl
	pos    token.Position
}

func TestOwnerCallbacksEndTheOperationOrAreChecked(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	methods := map[string]ownerMethod{}
	for _, path := range ownerCallbackFiles(t, root) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			typ, v := ownerRecv(fd)
			if !ownerCallbackViews[typ] {
				continue
			}
			methods[typ+"."+fd.Name.Name] = ownerMethod{typ, v, fd, fset.Position(fd.Pos())}
		}
	}
	if len(methods) < 50 {
		t.Fatalf("found %d view methods; the file globs no longer match the views", len(methods))
	}

	// reaches: the methods that can reach an owner hook, to a fixpoint
	reaches := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, m := range methods {
			if reaches[name] {
				continue
			}
			ast.Inspect(m.decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || reaches[name] {
					return !reaches[name]
				}
				if callee, ok := calledMethod(call, m.typ, m.v); isHook(call, m.v) || ok && reaches[callee] {
					reaches[name] = true
					changed = true
				}
				return true
			})
		}
	}
	for _, must := range []string{"List.moveCursor", "TableCore.SetCursor", "TableCore.Changed", "Tree.noteSelection"} {
		if !reaches[must] {
			t.Errorf("%s is not seen to reach an owner callback: the guard no longer models the views", must)
		}
	}

	// survival: the methods whose result is declared live, the epoch's answer
	survival := map[string]bool{}
	for name, m := range methods {
		if r := m.decl.Type.Results; r != nil {
			for _, f := range r.List {
				for _, n := range f.Names {
					survival[name] = survival[name] || n.Name == "live"
				}
			}
		}
	}
	if !survival["TableCore.cursorTo"] || !survival["TableCore.changed"] {
		t.Error("cursorTo and changed are not seen as survival calls: the guard no longer models the views")
	}

	var bad []string
	for name, m := range methods {
		var walk func(stmts []ast.Stmt, tail bool)
		// check flags, in a statement not in tail position, a call that can reach an owner callback
		// and is not a survival call: its result, used or not, does not say the rows survived. Calls in
		// a nested function literal are that literal's own (walked with it).
		check := func(s ast.Stmt, tail bool) {
			if tail {
				return
			}
			var inspect func(n ast.Node) bool
			inspect = func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncLit, *ast.BlockStmt:
					return false
				case *ast.CallExpr:
					if callee, ok := calledMethod(x, m.typ, m.v); ok && reaches[callee] && !survival[callee] {
						bad = append(bad, fmt.Sprintf("%s:%d %s calls %s and goes on: end the operation there, or make it a survival call (live bool) and check it",
							rel(root, m.pos.Filename), fset.Position(x.Pos()).Line, name, callee))
					}
				}
				return true
			}
			switch x := s.(type) {
			case *ast.IfStmt: // its init and condition only: the bodies are walked as blocks
				if x.Init != nil {
					ast.Inspect(x.Init, inspect)
				}
				ast.Inspect(x.Cond, inspect)
			case *ast.SwitchStmt:
				if x.Init != nil {
					ast.Inspect(x.Init, inspect)
				}
				if x.Tag != nil {
					ast.Inspect(x.Tag, inspect)
				}
			case *ast.ForStmt, *ast.RangeStmt, *ast.TypeSwitchStmt, *ast.BlockStmt, *ast.ReturnStmt:
			default:
				ast.Inspect(s, inspect)
			}
		}
		walk = func(stmts []ast.Stmt, tail bool) {
			for i, s := range stmts {
				rest := stmts[i+1:]
				onlyReturns := len(rest) > 0
				for _, r := range rest {
					if _, ok := r.(*ast.ReturnStmt); !ok {
						onlyReturns = false
					}
				}
				st := onlyReturns || len(rest) == 0 && tail
				check(s, st)
				switch x := s.(type) {
				case *ast.BlockStmt:
					walk(x.List, st)
				case *ast.IfStmt:
					walk(x.Body.List, st)
					for e := x.Else; e != nil; {
						switch el := e.(type) {
						case *ast.BlockStmt:
							walk(el.List, st)
							e = nil
						case *ast.IfStmt:
							walk(el.Body.List, st)
							e = el.Else
						}
					}
				case *ast.SwitchStmt:
					for _, c := range x.Body.List {
						walk(c.(*ast.CaseClause).Body, st)
					}
				case *ast.TypeSwitchStmt:
					for _, c := range x.Body.List {
						walk(c.(*ast.CaseClause).Body, st)
					}
				case *ast.ForStmt:
					walk(x.Body.List, false)
				case *ast.RangeStmt:
					walk(x.Body.List, false)
				}
			}
		}
		walk(m.decl.Body.List, true)
		// a function literal is its own tail: the step a Survives call runs
		ast.Inspect(m.decl.Body, func(n ast.Node) bool {
			if fl, ok := n.(*ast.FuncLit); ok {
				walk(fl.Body.List, true)
			}
			return true
		})
	}
	sort.Strings(bad)
	bad = dedupe(bad)
	if len(bad) > 0 {
		t.Errorf("%d owner-callback continuation(s):\n\t%s", len(bad), strings.Join(bad, "\n\t"))
	}
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

func dedupe(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}
