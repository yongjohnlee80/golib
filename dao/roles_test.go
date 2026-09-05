package dao

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The roles in roles.go must partition DAO: every DAO method in exactly one
// role, every role part of the union, and the total method set frozen against
// a golden file. These three tests are what let roles.go state the rule as a
// fact rather than a hope.

// daoMethodCount and roleCount are vacuity guards. Every assertion below is a
// set comparison, and set comparisons pass loudly when both sides are empty —
// an empty role table would "prove" the partition. Pinning both totals means a
// test that observes nothing fails instead of reporting success.
const (
	daoMethodCount = 24
	roleCount      = 8
)

// roleTypes lists the roles by the instantiation used for reflection. The
// type arguments are arbitrary but must agree with daoType below, or the
// signatures will not compare equal.
func roleTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		"Named":        reflect.TypeOf((*Named)(nil)).Elem(),
		"Filterer":     reflect.TypeOf((*Filterer[any, string, int])(nil)).Elem(),
		"Setter":       reflect.TypeOf((*Setter[any, string, int])(nil)).Elem(),
		"ResultShaper": reflect.TypeOf((*ResultShaper[any, string, int])(nil)).Elem(),
		"TxBinder":     reflect.TypeOf((*TxBinder[any, string, int])(nil)).Elem(),
		"Reader":       reflect.TypeOf((*Reader[any, string])(nil)).Elem(),
		"Writer":       reflect.TypeOf((*Writer[int])(nil)).Elem(),
		"Batcher":      reflect.TypeOf((*Batcher[any, string])(nil)).Elem(),
	}
}

func daoType() reflect.Type { return reflect.TypeOf((*DAO[any, string, int])(nil)).Elem() }

// methodLines renders an interface's methods as sorted "Name Signature" lines.
// The signature is included so that CHANGING a method is caught, not only
// adding or removing one.
func methodLines(t reflect.Type) []string {
	out := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		out = append(out, fmt.Sprintf("%s %s", m.Name, m.Type))
	}
	sort.Strings(out)
	return out
}

func TestDAOMethodSetIsFrozen(t *testing.T) {
	got := methodLines(daoType())
	if len(got) != daoMethodCount {
		t.Fatalf("DAO has %d methods, expected %d — update daoMethodCount deliberately, "+
			"and only after confirming the change is intended", len(got), daoMethodCount)
	}

	golden := filepath.Join("testdata", "dao_method_set.txt")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v", golden, err)
	}
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")

	if len(wantLines) != len(got) {
		t.Fatalf("golden has %d lines, DAO has %d methods", len(wantLines), len(got))
	}
	for i := range got {
		if got[i] != wantLines[i] {
			t.Errorf("method set drifted at line %d:\n  got:  %s\n  want: %s", i+1, got[i], wantLines[i])
		}
	}
	if t.Failed() {
		t.Log("DAO is implemented outside this repo. Adding or changing a method here " +
			"breaks every implementor at compile time; the convention is to add a new " +
			"optional interface and probe for it instead.")
	}
}

func TestRolesPartitionDAO(t *testing.T) {
	roles := roleTypes()
	if len(roles) != roleCount {
		t.Fatalf("roleTypes lists %d roles, expected %d", len(roles), roleCount)
	}

	// owner[method] = the role that declares it. A second writer is an overlap.
	owner := map[string]string{}
	for name, rt := range roles {
		lines := methodLines(rt)
		if len(lines) == 0 {
			t.Errorf("role %s declares no methods", name)
		}
		for _, l := range lines {
			if prev, dup := owner[l]; dup {
				t.Errorf("method %q is in two roles: %s and %s", l, prev, name)
				continue
			}
			owner[l] = name
		}
	}

	daoMethods := methodLines(daoType())
	inDAO := map[string]bool{}
	for _, l := range daoMethods {
		inDAO[l] = true
	}

	// Direction 1: every DAO method is owned by some role.
	for _, l := range daoMethods {
		if _, ok := owner[l]; !ok {
			t.Errorf("DAO method %q belongs to no role — add it to a role, or declare a new one", l)
		}
	}
	// Direction 2: every role method is actually on DAO. Catches a role that
	// drifted out of the union, or one whose instantiation disagrees.
	for l, role := range owner {
		if !inDAO[l] {
			t.Errorf("role %s declares %q, which is not on DAO", role, l)
		}
	}

	if len(owner) != daoMethodCount {
		t.Errorf("roles cover %d distinct methods, DAO has %d", len(owner), daoMethodCount)
	}
}

// TestEveryDeclaredRoleIsEmbedded reads the source rather than the types,
// because reflection cannot see a role that was declared and then forgotten:
// an unembedded role still compiles and still has methods.
func TestEveryDeclaredRoleIsEmbedded(t *testing.T) {
	fset := token.NewFileSet()

	rolesFile, err := parser.ParseFile(fset, "roles.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse roles.go: %v", err)
	}
	declared := map[string]bool{}
	for _, d := range rolesFile.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, isIface := ts.Type.(*ast.InterfaceType); !isIface {
				continue
			}
			if ts.Name.IsExported() {
				declared[ts.Name.Name] = true
			}
		}
	}

	daoFile, err := parser.ParseFile(fset, "dao.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse dao.go: %v", err)
	}
	embedded := map[string]bool{}
	ast.Inspect(daoFile, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "DAO" {
			return true
		}
		it, ok := ts.Type.(*ast.InterfaceType)
		if !ok {
			return true
		}
		for _, f := range it.Methods.List {
			if len(f.Names) > 0 {
				t.Errorf("DAO declares method %s directly; every method must live in a role", f.Names[0].Name)
				continue
			}
			if name := embeddedName(f.Type); name != "" {
				embedded[name] = true
			}
		}
		return false
	})

	if len(declared) != roleCount {
		t.Errorf("roles.go declares %d exported interfaces, expected %d: %v",
			len(declared), roleCount, sortedKeys(declared))
	}
	for name := range declared {
		if !embedded[name] {
			t.Errorf("role %s is declared in roles.go but not embedded in DAO", name)
		}
	}
	for name := range embedded {
		if !declared[name] {
			t.Errorf("DAO embeds %s, which is not a role declared in roles.go", name)
		}
	}
}

// embeddedName returns the type name of an embedded interface, handling the
// plain (Named), single-argument and multi-argument generic forms.
func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The concrete implementation satisfies every role individually, not only
// their union. Without these a role could name a method with a signature no
// implementation actually has, and only the union witness would catch it.
var (
	_ Named                          = (*queryDAO[any, string, string, int])(nil)
	_ Filterer[any, string, int]     = (*queryDAO[any, string, string, int])(nil)
	_ Setter[any, string, int]       = (*queryDAO[any, string, string, int])(nil)
	_ ResultShaper[any, string, int] = (*queryDAO[any, string, string, int])(nil)
	_ TxBinder[any, string, int]     = (*queryDAO[any, string, string, int])(nil)
	_ Reader[any, string]            = (*queryDAO[any, string, string, int])(nil)
	_ Writer[int]                    = (*queryDAO[any, string, string, int])(nil)
	_ Batcher[any, string]           = (*queryDAO[any, string, string, int])(nil)
)
