package decl_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// ReconcileWithSources declares the sources a new document reads, then reconciles against the
// enlarged set (a host part added at runtime). The set a schema is checked against still changes
// only before planning begins, so a refusal of either step leaves no name it added behind.

const oneText = `Flex { Text { id: old; text: App.old } }`
const twoTexts = `Flex { Text { id: old; text: App.old } Text { id: fresh; text: App.fresh } }`

// A reconcile that reads a source never declared is refused; declared by ReconcileWithSources, it
// binds, and the new source moves as any other.
func TestAReconcileDeclaresTheSourcesItReads(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, oneText, map[string]string{"App.old": "before"}, nil)
	if _, err := tr.Reconcile(mustSpec(t, twoTexts)); err == nil {
		t.Fatal("a reconcile bound a source never declared")
	}
	if _, err := tr.ReconcileWithSources(mustSpec(t, twoTexts), map[string]qml.SpecValue{"App.fresh": sv("newcomer")}); err != nil {
		t.Fatalf("ReconcileWithSources: %v", err)
	}
	if in, ok := tr.Lookup("App.fresh"); !ok || in.Kind != decl.KindSource {
		t.Fatalf("App.fresh is not declared a source: %+v %v", in, ok)
	}
	if v, ok := tr.Source("App.fresh"); !ok || v.Raw != "newcomer" {
		t.Fatalf("App.fresh holds %+v (%v), want newcomer", v, ok)
	}
	if _, err := tr.SetSource("App.fresh", sv("moved")); err != nil {
		t.Fatalf("the new source does not move: %v", err)
	}
	if _, err := tr.SetSource("App.old", sv("after")); err != nil {
		t.Fatalf("the old source no longer moves: %v", err)
	}
}

// A declaration refused part-way (App.parent: a reserved segment, sorted after App.good) takes the
// names declared before it away again, and the tree is unchanged.
func TestARefusedDeclarationDeclaresNothing(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, oneText, map[string]string{"App.old": "before"}, nil)
	_, err := tr.ReconcileWithSources(mustSpec(t, twoTexts),
		map[string]qml.SpecValue{"App.good": sv("g"), "App.parent": sv("p"), "App.fresh": sv("f")})
	if !errors.Is(err, decl.ErrReservedName) {
		t.Fatalf("a reserved name: %v, want ErrReservedName", err)
	}
	for _, name := range []string{"App.fresh", "App.good", "App.parent"} {
		if _, ok := tr.Lookup(name); ok {
			t.Errorf("a refused declaration left %s declared", name)
		}
	}
	if tr.Failed() {
		t.Fatal("a refused declaration latched the tree")
	}
}

// A reconcile refused after its sources were declared (the document reads one more, never declared)
// takes them away again; a name already declared is refused, not redeclared.
func TestARefusedReconcileDeclaresNothing(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, oneText, map[string]string{"App.old": "before"}, nil)
	missing := `Flex { Text { id: old; text: App.old } Text { id: fresh; text: App.fresh } Text { id: miss; text: App.missing } }`
	if _, err := tr.ReconcileWithSources(mustSpec(t, missing), map[string]qml.SpecValue{"App.fresh": sv("f")}); err == nil {
		t.Fatal("a reconcile reading a source still undeclared was accepted")
	}
	if _, ok := tr.Lookup("App.fresh"); ok {
		t.Fatal("a refused reconcile left App.fresh declared")
	}
	if _, err := tr.SetSource("App.fresh", sv("x")); !errors.Is(err, decl.ErrNoSuchSource) {
		t.Fatalf("setting App.fresh after the refusal: %v, want ErrNoSuchSource", err)
	}
	_, err := tr.ReconcileWithSources(mustSpec(t, twoTexts), map[string]qml.SpecValue{"App.old": sv("again"), "App.fresh": sv("f")})
	if !errors.Is(err, decl.ErrDuplicateDecl) {
		t.Fatalf("a name declared twice: %v, want ErrDuplicateDecl", err)
	}
	if _, ok := tr.Lookup("App.fresh"); ok {
		t.Fatal("a refused duplicate left App.fresh declared")
	}
	if v, _ := tr.Source("App.old"); v.Raw != "before" {
		t.Fatalf("a refused duplicate changed App.old to %q", v.Raw)
	}
}
