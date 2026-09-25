package decl_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// component_test.go covers component types: a QML file that defines a type,
// loaded when a document imports its module, and expanded where it is used.
// Every claim is read off the adapter's trace — what was BUILT — because an
// expansion that parsed and built nothing would pass any test of the parse.

func componentTree(t *testing.T, files map[string]string) (*decl.Tree, *recorder, *counted) {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, src := range files {
		fsys["ui/"+name] = &fstest.MapFile{Data: []byte(src)}
	}
	a := newReactor()
	tr := decl.New(a)
	c := &counted{}
	load := decl.ComponentFiles(fsys, "ui")
	if err := tr.OfferModule("app.ui", "", func() (decl.ModuleContents, error) {
		c.calls++
		return load()
	}); err != nil {
		t.Fatal(err)
	}
	return tr, a.recorder, c
}

func traced(rec *recorder) string { return strings.Join(rec.trace, "\n") }

// TestAComponentFileIsATypeNamedForItsFile, expanded with Qt's rules: the use
// site's property replaces the component's, and its children follow.
func TestAComponentFileIsATypeNamedForItsFile(t *testing.T) {
	tr, rec, c := componentTree(t, map[string]string{
		"Card.qml": "Box {\n title: \"default\"\n Text { text: \"inside\" }\n}",
	})
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { id: card; title: \"mine\"\n  Text { text: \"added\" } }\n}"))
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	trace := traced(rec)
	for _, want := range []string{"create 3 Text", "create 4 Text", "create 2 Box", "title=string(mine)",
		"text=string(inside)", "text=string(added)"} {
		if !strings.Contains(trace, want) {
			t.Errorf("trace lacks %q:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, "default") {
		t.Errorf("the component's title was applied as well as the use site's:\n%s", trace)
	}
	if id, ok := tr.NodeByID("card"); !ok || id != 2 {
		t.Errorf("the use site's id names node %d (%v), want the Box, 2", id, ok)
	}
	if c.calls != 1 {
		t.Errorf("loads = %d, want 1", c.calls)
	}
}

// TestAComponentIsNotLoadedUntilImported, and is not a type without the import.
func TestAComponentIsNotLoadedUntilImported(t *testing.T) {
	tr, rec, c := componentTree(t, map[string]string{"Card.qml": "Box { }"})
	// The fake adapter builds any type name, so Card mounts — as a raw Card,
	// which is the point: nothing was expanded.
	if err := tr.Mount(qmlDoc(t, "Flex {\n Card { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if strings.Contains(traced(rec), "Box") {
		t.Fatalf("Card expanded without an import:\n%s", traced(rec))
	}
	if c.calls != 0 {
		t.Fatalf("loads = %d without an import, want 0", c.calls)
	}
}

// TestComponentHandlersBothRun: the component's and the use site's, in that
// order — a use site adds to what the component does, it does not undo it.
func TestComponentHandlersBothRun(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Go.qml": "Button { onClicked: first() }"})
	r := rec
	for _, n := range []string{"first", "second"} {
		n := n
		if err := tr.Inject(n, decl.Handle(func(_ []qml.SpecValue) error {
			r.trace = append(r.trace, "run "+n)
			return nil
		})); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Go { id: go; onClicked: second() }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	id, _ := tr.NodeByID("go")
	rec.trace = nil
	if err := rec.emitters[id]["clicked"](); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if got := strings.Join(rec.trace, ","); got != "run first,run second" {
		t.Fatalf("ran %q, want the component's handler then the use site's", got)
	}
}

// TestAComponentResolvesNamesInTheDocumentUsingIt: the theme it reads is the
// one the LAYOUT imported, which is what keeps a theme switch to one line.
func TestAComponentResolvesNamesInTheDocumentUsingIt(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Card.qml": "Box { title: Theme.name }"})
	for _, th := range []struct{ module, name string }{{"theme.a", "alpha"}, {"theme.b", "beta"}} {
		if err := tr.OfferModule(th.module, "", decl.ValueModule([]byte(`Theme { name: "`+th.name+`" }`))); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nimport theme.b\nFlex {\n Card { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if !strings.Contains(traced(rec), "title=string(beta)") {
		t.Fatalf("the component did not read the layout's theme:\n%s", traced(rec))
	}
}

func TestAComponentFileIsRefusedForWhatItMayNotContain(t *testing.T) {
	for name, c := range map[string]struct{ src, want string }{
		"an import": {"import tui 1.0\nBox { }", "imports nothing"},
		"an id":     {"Box {\n Text { id: label }\n}", "declares no ids"},
		"bad QML":   {"Box {", "ui/Bad.qml:"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _, _ := componentTree(t, map[string]string{"Bad.qml": c.src})
			err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Bad { }\n}"))
			if !errors.Is(err, decl.ErrModuleLoad) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrModuleLoad mentioning %q", err, c.want)
			}
		})
	}
}

// TestAnErrorInsideAComponentNamesItsFile: once a screen is several files, a
// bare line number is a line of nobody knows which.
func TestAnErrorInsideAComponentNamesItsFile(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n title: Nope.x\n}"})
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { }\n}"))
	if err == nil || !strings.Contains(err.Error(), "ui/Card.qml:2:") {
		t.Fatalf("err = %v, want it placed at ui/Card.qml:2", err)
	}
}

func TestAComponentThatContainsItselfIsRefused(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{
		"A.qml": "Flex {\n B { }\n}",
		"B.qml": "Flex {\n A { }\n}",
	})
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n A { }\n}"))
	if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), "A → B → A") {
		t.Fatalf("err = %v, want the cycle named", err)
	}
}

// TestTwoModulesMayNotShareAComponentName, and a refused load leaves neither.
func TestTwoModulesMayNotShareAComponentName(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box { }"})
	other := fstest.MapFS{"x/Card.qml": &fstest.MapFile{Data: []byte("Flex { }")}}
	if err := tr.OfferModule("other.ui", "", decl.ComponentFiles(other, "x")); err != nil {
		t.Fatal(err)
	}
	err := tr.Mount(qmlDoc(t, "import app.ui\nimport other.ui\nFlex { }"))
	if !errors.Is(err, decl.ErrDuplicateExport) {
		t.Fatalf("err = %v, want ErrDuplicateExport", err)
	}
	if err := tr.Mount(qmlDoc(t, "import other.ui\nFlex {\n Card { }\n}")); err != nil {
		t.Fatalf("the refused mount left app.ui's Card claiming the name: %v", err)
	}
}

// TestAQualifiedImportReachesComponentsThroughItsQualifier.
func TestAQualifiedImportReachesComponentsThroughItsQualifier(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Card.qml": "Box { title: \"q\" }"})
	if err := tr.Mount(qmlDoc(t, "import app.ui as UI\nFlex {\n UI.Card { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if !strings.Contains(traced(rec), "title=string(q)") {
		t.Fatalf("UI.Card did not expand:\n%s", traced(rec))
	}
}

// TestAReloadReExpandsTheComponent: the component is expanded afresh for every
// document, so an edit to the USE reaches the screen.
func TestAReloadReExpandsTheComponent(t *testing.T) {
	tr, rec, c := componentTree(t, map[string]string{"Card.qml": "Box { title: \"a\" }"})
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	rec.trace = nil
	if _, err := tr.Reconcile(qmlDoc(t, "import app.ui\nFlex {\n Card { title: \"b\" }\n}")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !strings.Contains(traced(rec), "title=string(b)") || c.calls != 1 {
		t.Fatalf("loads %d; trace:\n%s", c.calls, traced(rec))
	}
}
