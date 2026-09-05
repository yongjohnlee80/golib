package dao

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A1. Dialect is exactly the six methods every engine must answer. The golden
// is the pre-change set MINUS the eleven that became capabilities or were
// deleted, so "what was removed" is a recorded fact rather than a claim, and
// re-adding any of them fails here.
func TestDialectSurfaceIsFrozen(t *testing.T) {
	ty := reflect.TypeOf((*Dialect)(nil)).Elem()
	var got []string
	for i := 0; i < ty.NumMethod(); i++ {
		m := ty.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)

	want := []string{
		"MaxBatchRows func() int",
		"MaxBindParams func() int",
		"Name func() string",
		"Placeholder func(int) string",
		"QuoteIdent func(string) string",
		"TranslateError func(error) error",
	}
	if len(got) != len(want) {
		t.Fatalf("Dialect has %d methods, want %d:\n  got:  %v\n  want: %v",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("method %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if t.Failed() {
		t.Log("Dialect is the contract every engine must satisfy. Anything some " +
			"engine would answer \"no\" to is a capability interface, not a method here.")
	}
}

// A2. No method on a shared interface asks whether something is supported.
//
// Scoped to INTERFACE METHODS on purpose: the same spelling as a free function
// is the blessed discovery form — SupportsIntrospection, SupportsRoutine-
// Introspection, postgres.SupportsSessionPinning all exist and all probe a
// type assertion. The difference is that a method makes every implementor
// answer, while a function asks the type system.
func TestNoCapabilityPredicatesOnInterfaces(t *testing.T) {
	banned := regexp.MustCompile(`^(Supports[A-Z]|Has[A-Z]|Is[A-Z]\w*Supported)|Supported$|Enabled$`)

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	var found []string
	walked := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok || !ts.Name.IsExported() {
				return true
			}
			for _, fld := range it.Methods.List {
				for _, nm := range fld.Names {
					if banned.MatchString(nm.Name) {
						found = append(found, filepath.ToSlash(rel)+": "+ts.Name.Name+"."+nm.Name)
					}
				}
			}
			return true
		})
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
		t.Errorf("%d capability predicate(s) on an exported interface:\n  %s\n"+
			"A capability is an interface carrying the operation it gates, so that "+
			"presence is the answer and there is no second source of truth to "+
			"disagree with the first.", len(found), strings.Join(found, "\n  "))
	}
}
