package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
)

// module_test.go covers the import layer.
//
// The claim it holds is that `import` MEANS something. A module system whose
// import line parses, resolves to nothing and changes nothing when omitted is
// not a module system — it is a comment with a keyword, and every test here
// exists to keep that from being true by accident.

func qml(t *testing.T, src string) parse.SpecTree {
	t.Helper()
	spec, err := parse.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return spec
}

// TestAModulesNamesAreUnreachableUntilItIsImported is the whole point.
func TestAModulesNamesAreUnreachableUntilItIsImported(t *testing.T) {
	// Without the import: refused, and told what to add.
	tr := decl.New(newReactor())
	err := tr.Mount(qml(t, `Flex { direction: tui.Vertical }`))
	if !errors.Is(err, decl.ErrNotImported) {
		t.Fatalf("err = %v, want ErrNotImported", err)
	}
	for _, want := range []string{"tui", "import"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic = %q, want it to mention %q", err, want)
		}
	}
	// It must NOT read as an unbound name: the name is fine, the import is missing.
	if errors.Is(err, decl.ErrNotInjected) {
		t.Errorf("err = %v, want it to blame the import rather than the name", err)
	}

	// With the import: the same document mounts.
	tr2 := decl.New(newReactor())
	if err := tr2.Mount(qml(t, "import tui 1.0\nFlex { direction: tui.Vertical }")); err != nil {
		t.Fatalf("the imported form was refused: %v", err)
	}
}

// TestImportingSomethingNoHostProvidesIsRefusedAtTheImport.
//
// The position matters as much as the refusal. An undefined module reported at
// each of the twenty places that use it buries the one line to fix.
func TestImportingSomethingNoHostProvidesIsRefusedAtTheImport(t *testing.T) {
	tr := decl.New(newReactor())
	err := tr.Mount(qml(t, "import QtQuick 2.15\nFlex { }"))
	if !errors.Is(err, decl.ErrUndefinedModule) {
		t.Fatalf("err = %v, want ErrUndefinedModule", err)
	}
	var se decl.SchemaError
	if !errors.As(err, &se) {
		t.Fatalf("err %T, want decl.SchemaError", err)
	}
	if se.Pos.Line != 1 {
		t.Errorf("Pos = %s, want the IMPORT's line", se.Pos)
	}
	// And it says what IS available, so the author is not left guessing.
	if !strings.Contains(err.Error(), "tui") {
		t.Errorf("diagnostic = %q, want it to list the modules the host provides", err)
	}
}

// TestAnImportIsCheckedBeforeAnythingIsBuilt.
//
// A bad import must leave the tree exactly as it was — the same rule every
// other check in this engine follows, and the reason a watcher can hold the
// last good screen.
func TestAnImportIsCheckedBeforeAnythingIsBuilt(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	if err := tr.Mount(qml(t, "import nosuch 1.0\nA { B { } C { } }")); err == nil {
		t.Fatal("an undefined module mounted")
	}
	if len(r.trace) != 0 {
		t.Errorf("the adapter was called %d times for a document that never imported: %v",
			len(r.trace), r.trace)
	}
	if tr.Len() != 0 {
		t.Errorf("%d nodes survive a refused import", tr.Len())
	}
	// Not latched: the corrected document mounts.
	if err := tr.Mount(qml(t, "import tui 1.0\nA { }")); err != nil {
		t.Fatalf("the tree was latched by a bad import: %v", err)
	}
}

// TestAnAliasReplacesTheModuleNameRatherThanAddingToIt.
//
// QML's rule, and the one an implementation is most likely to get wrong by
// being generous: binding BOTH spellings would let a document mount here that a
// real QML runtime refuses, which is the worst kind of compatibility.
func TestAnAliasReplacesTheModuleNameRatherThanAddingToIt(t *testing.T) {
	tr := decl.New(newReactor())
	if err := tr.Mount(qml(t, "import tui 1.0 as T\nFlex { direction: T.Vertical }")); err != nil {
		t.Fatalf("the aliased name was refused: %v", err)
	}

	tr2 := decl.New(newReactor())
	err := tr2.Mount(qml(t, "import tui 1.0 as T\nFlex { direction: tui.Vertical }"))
	if !errors.Is(err, decl.ErrNotImported) {
		t.Fatalf("err = %v, want the original spelling to stop resolving under an alias", err)
	}
}

// TestAVersionTheHostDoesNotAnswerToIsRefused, and one it does is accepted,
// and an import that names none takes what it is given.
func TestImportVersionsAreComparedOnlyWhenStated(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		wantOK bool
	}{
		{"the version the host answers to", "import tui 1.0\nFlex { direction: tui.Vertical }", true},
		{"no version at all", "import tui\nFlex { direction: tui.Vertical }", true},
		{"a version the host does not answer to", "import tui 2.0\nFlex { direction: tui.Vertical }", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := decl.New(newReactor()).Mount(qml(t, c.src))
			if c.wantOK && err != nil {
				t.Fatalf("mount: %v", err)
			}
			if !c.wantOK {
				if !errors.Is(err, decl.ErrUndefinedModule) {
					t.Fatalf("err = %v, want ErrUndefinedModule", err)
				}
				if !strings.Contains(err.Error(), "2.0") {
					t.Errorf("diagnostic = %q, want it to name the version asked for", err)
				}
			}
		})
	}
}

// TestTwoImportsCannotBindTheSameName.
func TestTwoImportsCannotBindTheSameName(t *testing.T) {
	tr := decl.New(newReactor())
	if err := tr.DeclareModule(decl.Module{Name: "other", Version: "1.0"}); err != nil {
		t.Fatalf("DeclareModule: %v", err)
	}
	err := tr.Mount(qml(t, "import tui 1.0 as T\nimport other 1.0 as T\nFlex { }"))
	if !errors.Is(err, decl.ErrDuplicateImport) {
		t.Fatalf("err = %v, want ErrDuplicateImport", err)
	}
}

// TestAnInjectedObjectNeedsNoImport.
//
// The other side of the line, and the reason the two kinds are separate. A host
// installing a context property is not publishing a module, and requiring an
// import for it would be inventing a rule QML does not have.
func TestAnInjectedObjectNeedsNoImport(t *testing.T) {
	tr := decl.New(newReactor())
	if err := tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := tr.Mount(qml(t, `Text { text: Theme.surface }`)); err != nil {
		t.Errorf("an injected object was made to require an import: %v", err)
	}
}

// TestAReloadWhoseImportIsWrongKeepsTheLastGoodDocument.
func TestAReloadWhoseImportIsWrongKeepsTheLastGoodDocument(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, "import tui 1.0\nFlex { direction: tui.Vertical Text { id: a text: \"one\" } }")

	if _, err := tr.Reconcile(qml(t, "import nosuch 1.0\nFlex { Text { id: a text: \"two\" } }")); !errors.Is(err, decl.ErrUndefinedModule) {
		t.Fatalf("err = %v, want ErrUndefinedModule", err)
	}
	if len(rec.trace) != 0 {
		t.Errorf("a reload with a bad import touched the tree: %v", rec.trace)
	}
	// And the live tree still resolves through the imports it MOUNTED with, so
	// the next good reload does not have to re-state them to keep working.
	if _, err := tr.Reconcile(qml(t, "import tui 1.0\nFlex { direction: tui.Vertical Text { id: a text: \"two\" } }")); err != nil {
		t.Fatalf("the tree was latched by a bad import: %v", err)
	}
}

// TestDeclareModuleIsRefusedOncePlanningHasBegun.
func TestDeclareModuleIsRefusedOncePlanningHasBegun(t *testing.T) {
	tr := decl.New(newReactor())
	if err := tr.Mount(qml(t, `Text { text: "hi" }`)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := tr.DeclareModule(decl.Module{Name: "late"}); !errors.Is(err, decl.ErrPhase) {
		t.Errorf("err = %v, want ErrPhase", err)
	}
}
