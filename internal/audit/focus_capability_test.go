package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE FOCUS CAPABILITY GUARD.
//
// Implementing tui.Focusable says a component is FOCUSABLE BY DESIGN — a
// control at all. AcceptsFocus says whether it takes focus NOW. A component that
// is never a focus stop does not implement Focusable; its absence says so, and
// tui.App.HoldsFocusable reads it (forceActiveFocus refuses a subtree with
// nothing focusable by design).
//
// A constant `AcceptsFocus() bool { return false }` answers the first question
// with the second: it claims the capability and denies it forever, so the
// component reads as a control that is merely unavailable. Eight golib widgets
// did exactly that, and HoldsFocusable counted them as focusable.
// This guard refuses the shape anywhere in the module, test files aside —
// a probe standing in for a disabled control may legitimately start at false.
//
// WHAT IT CANNOT SEE: a body that is false by construction but not the literal
// `return false` — `return x.never`, a constant named something else. Those
// are for review; the doc comment on tui.Focusable states the rule.
func TestNoConstantFalseAcceptsFocus(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	walked := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "AcceptsFocus" || fn.Body == nil {
				continue
			}
			if len(fn.Body.List) != 1 {
				continue
			}
			ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			if id, ok := ret.Results[0].(*ast.Ident); ok && id.Name == "false" {
				rel, _ := filepath.Rel(root, path)
				found = append(found, filepath.ToSlash(rel)+":"+
					itoaPos(fset.Position(fn.Pos()).Line)+" "+recvName(fn))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	if walked < 100 {
		t.Fatalf("walked only %d files; this absence assertion would pass vacuously", walked)
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("%d constant-false AcceptsFocus:\n  %s\n"+
			"A component that is never a focus stop does not implement tui.Focusable at "+
			"all; AcceptsFocus answers only whether it takes focus NOW. When being a "+
			"control is a per-instance choice, implement tui.FocusDesigner.",
			len(found), strings.Join(found, "\n  "))
	}
}

// recvName is a method's receiver type, for the report.
func recvName(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return "?"
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return "?"
}

func itoaPos(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
