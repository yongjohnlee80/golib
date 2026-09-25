package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// offer_test.go covers OFFERED modules: importable without being loaded, and
// loaded by the first document whose import names them.
//
// Every claim is checked by what was LOADED, counted at the loader, and not
// only by whether a document mounted — a module system that loaded everything
// up front would mount every one of these documents too.

const (
	monoTheme  = `Theme { menu { window: "white"; accent: "black" } }`
	retroTheme = `Theme { menu { window: "white"; accent: "red" } }`
)

// counted wraps a loader so a test can see how often it ran.
type counted struct{ calls int }

func (c *counted) loader(src string) decl.ModuleLoader {
	load := decl.ValueModule([]byte(src))
	return func() (decl.ModuleContents, error) {
		c.calls++
		return load()
	}
}

// themed returns a tree offering two themes that both export Theme.
func themed(t *testing.T) (*decl.Tree, *recorder, *counted, *counted) {
	t.Helper()
	a := newReactor()
	tr := decl.New(a)
	mono, retro := &counted{}, &counted{}
	if err := tr.OfferModule("theme.mono", "1.0", mono.loader(monoTheme)); err != nil {
		t.Fatal(err)
	}
	if err := tr.OfferModule("theme.retro", "1.0", retro.loader(retroTheme)); err != nil {
		t.Fatal(err)
	}
	return tr, a.recorder, mono, retro
}

// applied reports whether the trace set prop to the string value.
func applied(rec *recorder, prop, value string) bool {
	want := fmt.Sprintf(" %s=string(%s) ", prop, value)
	for _, l := range rec.trace {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// TestOnlyTheImportedModuleIsLoaded is the whole point: the import line
// selects, and the module nobody imported is never read.
func TestOnlyTheImportedModuleIsLoaded(t *testing.T) {
	tr, rec, mono, retro := themed(t)
	err := tr.Mount(qmlDoc(t, "import theme.retro 1.0\nText { text: Theme.menu.accent }"))
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	if retro.calls != 1 || mono.calls != 0 {
		t.Fatalf("loads: retro=%d mono=%d, want 1 and 0", retro.calls, mono.calls)
	}
	if !applied(rec, "text", "red") {
		t.Fatalf("the retro value did not reach the widget; trace:\n%s", strings.Join(rec.trace, "\n"))
	}
}

// TestSwitchingTheImportSwitchesTheModule: the same document with the other
// import line gets the other module's values, and nothing else changes.
func TestSwitchingTheImportSwitchesTheModule(t *testing.T) {
	tr, rec, mono, retro := themed(t)
	if err := tr.Mount(qmlDoc(t, "import theme.mono 1.0\nText { text: Theme.menu.accent }")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if mono.calls != 1 || retro.calls != 0 || !applied(rec, "text", "black") {
		t.Fatalf("mono: loads mono=%d retro=%d; trace:\n%s", mono.calls, retro.calls, strings.Join(rec.trace, "\n"))
	}
}

// TestImportingTwoModulesExportingOneNameIsRefused — and the refusal leaves
// nothing loaded, so the first of the two is not left claiming the name.
func TestImportingTwoModulesExportingOneNameIsRefused(t *testing.T) {
	tr, _, mono, _ := themed(t)
	err := tr.Mount(qmlDoc(t, "import theme.retro 1.0\nimport theme.mono 1.0\nText { text: Theme.menu.accent }"))
	if !errors.Is(err, decl.ErrDuplicateExport) {
		t.Fatalf("err = %v, want ErrDuplicateExport", err)
	}
	// Were retro still loaded, mono could never load: it exports the same name.
	if err := tr.Mount(qmlDoc(t, "import theme.mono 1.0\nText { text: Theme.menu.accent }")); err != nil {
		t.Fatalf("after the refusal, a document importing mono alone was refused: %v", err)
	}
	if mono.calls != 2 {
		t.Fatalf("mono loads = %d, want 2 (refused attempt, then the mount)", mono.calls)
	}
}

// TestARefusedMountUnloadsWhatItLoaded: a document refused AFTER its modules
// loaded — here for naming a value the theme lacks — leaves the tree as it
// found it.
func TestARefusedMountUnloadsWhatItLoaded(t *testing.T) {
	tr, _, _, retro := themed(t)
	err := tr.Mount(qmlDoc(t, "import theme.retro 1.0\nText { text: Theme.menu.missing }"))
	if err == nil {
		t.Fatal("a document naming a value the theme lacks mounted")
	}
	if err := tr.Mount(qmlDoc(t, "import theme.mono 1.0\nText { text: Theme.menu.accent }")); err != nil {
		t.Fatalf("retro was left loaded by the refused mount: %v", err)
	}
	if retro.calls != 1 {
		t.Fatalf("retro loads = %d, want 1", retro.calls)
	}
}

// TestAReconcileLoadsAModuleItsDocumentIsFirstToImport — the large-application
// case: a module arrives when the document that needs it does.
func TestAReconcileLoadsAModuleItsDocumentIsFirstToImport(t *testing.T) {
	a := newReactor()
	tr := decl.New(a)
	dialogs := &counted{}
	if err := tr.OfferModule("app.dialogs", "", dialogs.loader(`Dialogs { save { title: "Save As" } }`)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, `Text { text: "editing" }`)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if dialogs.calls != 0 {
		t.Fatalf("dialogs loaded by a document that does not import it (%d)", dialogs.calls)
	}
	if _, err := tr.Reconcile(qmlDoc(t, "import app.dialogs\nText { text: Dialogs.save.title }")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if dialogs.calls != 1 || !applied(a.recorder, "text", "Save As") {
		t.Fatalf("dialogs loads = %d; trace:\n%s", dialogs.calls, strings.Join(a.recorder.trace, "\n"))
	}
	// Still imported by the next reload: loaded once, not again.
	if _, err := tr.Reconcile(qmlDoc(t, "import app.dialogs\nText { text: Dialogs.save.title }")); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if dialogs.calls != 1 {
		t.Fatalf("dialogs loads = %d after a second reload, want 1", dialogs.calls)
	}
}

// TestARefusedReconcileUnloadsWhatItLoaded: the live tree keeps the last good
// document, and the module that document never imported goes with the refusal.
func TestARefusedReconcileUnloadsWhatItLoaded(t *testing.T) {
	tr, _, mono, retro := themed(t)
	if err := tr.Mount(qmlDoc(t, `Text { text: "plain" }`)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if _, err := tr.Reconcile(qmlDoc(t, "import theme.retro 1.0\nText { text: Theme.menu.missing }")); err == nil {
		t.Fatal("a reload naming a missing value was accepted")
	}
	if _, err := tr.Reconcile(qmlDoc(t, "import theme.mono 1.0\nText { text: Theme.menu.accent }")); err != nil {
		t.Fatalf("retro was left loaded by the refused reload: %v", err)
	}
	if retro.calls != 1 || mono.calls != 1 {
		t.Fatalf("loads: retro=%d mono=%d, want 1 and 1", retro.calls, mono.calls)
	}
}

// TestDestroyUnloadsEveryModule: the values went with the registry, so the
// modules must too — or a remount would find a module with nothing in it.
func TestDestroyUnloadsEveryModule(t *testing.T) {
	tr, rec, _, retro := themed(t)
	doc := "import theme.retro 1.0\nText { text: Theme.menu.accent }"
	if err := tr.Mount(qmlDoc(t, doc)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := tr.Destroy(); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	rec.trace = nil
	if err := tr.Mount(qmlDoc(t, doc)); err != nil {
		t.Fatalf("remount: %v", err)
	}
	if retro.calls != 2 || !applied(rec, "text", "red") {
		t.Fatalf("retro loads = %d; trace:\n%s", retro.calls, strings.Join(rec.trace, "\n"))
	}
}

// TestAWrongVersionIsRefusedBeforeTheLoaderRuns: a module loaded only to be
// told it was the wrong one would report the load's problems, not the import's.
func TestAWrongVersionIsRefusedBeforeTheLoaderRuns(t *testing.T) {
	tr, _, _, retro := themed(t)
	err := tr.Mount(qmlDoc(t, "import theme.retro 2.0\nText { text: Theme.menu.accent }"))
	if !errors.Is(err, decl.ErrUndefinedModule) || !strings.Contains(err.Error(), `"2.0"`) {
		t.Fatalf("err = %v, want the version refused", err)
	}
	if retro.calls != 0 {
		t.Fatalf("retro loads = %d, want 0", retro.calls)
	}
}

// TestALoaderFailureIsReportedAtTheImport.
func TestALoaderFailureIsReportedAtTheImport(t *testing.T) {
	tr := decl.New(newReactor())
	boom := errors.New("theme file is corrupt")
	if err := tr.OfferModule("theme.broken", "", func() (decl.ModuleContents, error) {
		return decl.ModuleContents{}, boom
	}); err != nil {
		t.Fatal(err)
	}
	err := tr.Mount(qmlDoc(t, "Text { text: \"x\" }\n"))
	if err != nil {
		t.Fatalf("a document not importing the broken module was refused: %v", err)
	}
	tr2 := decl.New(newReactor())
	_ = tr2.OfferModule("theme.broken", "", func() (decl.ModuleContents, error) {
		return decl.ModuleContents{}, boom
	})
	err = tr2.Mount(qmlDoc(t, "\nimport theme.broken\nText { text: \"x\" }"))
	var se decl.SchemaError
	if !errors.Is(err, decl.ErrModuleLoad) || !errors.As(err, &se) || se.Pos.Line != 2 {
		t.Fatalf("err = %v, want ErrModuleLoad at the import on line 2", err)
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("diagnostic = %q, want the loader's reason", err)
	}
}

// TestAModuleMayOnlyBringItsOwnSingletonsIntoScope: a loader writing a name
// outside its exports would be injecting a global nobody imported.
func TestAModuleMayOnlyBringItsOwnSingletonsIntoScope(t *testing.T) {
	tr := decl.New(newReactor())
	_ = tr.OfferModule("sneaky", "", func() (decl.ModuleContents, error) {
		return decl.ModuleContents{
			Exports: []string{"Theme"},
			Values:  map[string]decl.Injected{"App.quit": decl.Constant(sv("x"))},
		}, nil
	})
	err := tr.Mount(qmlDoc(t, "import sneaky\nText { text: \"x\" }"))
	if !errors.Is(err, decl.ErrModuleLoad) || !strings.Contains(err.Error(), "App.quit") {
		t.Fatalf("err = %v, want ErrModuleLoad naming App.quit", err)
	}
}

// TestAValueModuleHoldsLiteralsOnly.
func TestAValueModuleHoldsLiteralsOnly(t *testing.T) {
	for name, src := range map[string]string{
		"binding":  `Theme { accent: Other.red }`,
		"child":    `Theme { Text { } }`,
		"import":   "import tui 1.0\nTheme { accent: \"red\" }",
		"id":       `Theme { id: t; accent: "red" }`,
		"handler":  `Theme { onChanged: App.x() }`,
		"twice":    `Theme { accent: "red"; accent: "blue" }`,
		"no parse": `Theme {`,
	} {
		if _, err := decl.ValueModule([]byte(src))(); err == nil {
			t.Errorf("%s: accepted %q", name, src)
		}
	}
	c, err := decl.ValueModule([]byte("Theme {\n menu { window: \"white\" }\n depth: 2; bold: true\n}"))()
	if err != nil {
		t.Fatalf("literal module refused: %v", err)
	}
	if len(c.Exports) != 1 || c.Exports[0] != "Theme" || len(c.Values) != 3 ||
		c.Values["Theme.menu.window"].Value.Raw != "white" {
		t.Fatalf("contents = %+v", c)
	}
}

// TestAValueFileNamesItselfInItsDiagnostics: a theme is a file, and a broken
// one is reported in it — as a component file is — not at a bare line number.
func TestAValueFileNamesItselfInItsDiagnostics(t *testing.T) {
	fsys := fstest.MapFS{
		"themes/mono.qml":   {Data: []byte("Theme {\n accent: Other.red\n}")},
		"themes/broken.qml": {Data: []byte("Theme {")},
		"themes/good.qml":   {Data: []byte(`Theme { accent: "red" }`)},
	}
	for file, want := range map[string]string{
		"themes/mono.qml":   "themes/mono.qml:2:",
		"themes/broken.qml": "themes/broken.qml:1:",
		"themes/gone.qml":   "gone.qml",
	} {
		_, err := decl.ValueFile(fsys, file)()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it placed at %s", file, err, want)
		}
	}
	c, err := decl.ValueFile(fsys, "themes/good.qml")()
	if err != nil || c.Values["Theme.accent"].Value.Raw != "red" {
		t.Fatalf("good.qml: %+v, %v", c, err)
	}
	if pos := c.Values["Theme.accent"].Value.Pos.String(); !strings.HasPrefix(pos, "themes/good.qml:") {
		t.Errorf("a constant's position is %q, want it in its file", pos)
	}
}

// TestOfferedAndDeclaredModulesShareOneNamespace.
func TestOfferedAndDeclaredModulesShareOneNamespace(t *testing.T) {
	load := decl.ValueModule([]byte(monoTheme))
	tr := decl.New(newReactor())
	if err := tr.OfferModule("tui", "", load); !errors.Is(err, decl.ErrDuplicateDecl) {
		t.Errorf("offering the adapter's module name: err = %v, want ErrDuplicateDecl", err)
	}
	if err := tr.OfferModule("theme", "", load); err != nil {
		t.Fatal(err)
	}
	if err := tr.OfferModule("theme", "", load); !errors.Is(err, decl.ErrDuplicateDecl) {
		t.Errorf("offering twice: err = %v, want ErrDuplicateDecl", err)
	}
	if err := tr.DeclareModule(decl.Module{Name: "theme"}); !errors.Is(err, decl.ErrDuplicateDecl) {
		t.Errorf("declaring an offered name: err = %v, want ErrDuplicateDecl", err)
	}
	if err := tr.Mount(qmlDoc(t, `Text { text: "x" }`)); err != nil {
		t.Fatal(err)
	}
	if err := tr.OfferModule("late", "", load); !errors.Is(err, decl.ErrPhase) {
		t.Errorf("offering after Mount: err = %v, want ErrPhase", err)
	}
}

// bareAdapter builds anything and offers no modules of its own.
type bareAdapter struct{}

func (bareAdapter) Create(decl.Construction) ([]string, error) { return nil, nil }
func (bareAdapter) Apply(decl.Application) error               { return nil }
func (bareAdapter) Destroy(decl.NodeID) error                  { return nil }

// TestAnUnknownImportNamesWhatIsImportable: the diagnostic lists offered
// modules as importable, whether loaded or not, and each once.
func TestAnUnknownImportNamesWhatIsImportable(t *testing.T) {
	tr := decl.New(bareAdapter{})
	err := tr.Mount(qmlDoc(t, "import nosuch\nX { }"))
	if !errors.Is(err, decl.ErrUndefinedModule) || !strings.Contains(err.Error(), "no modules at all") {
		t.Fatalf("with nothing importable: err = %v, want it said", err)
	}

	tr, _, _, _ = themed(t)
	err = tr.Mount(qmlDoc(t, "import nosuch\nText { }"))
	for _, want := range []string{"theme.mono", "theme.retro", "tui"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want the importable %q named", err, want)
		}
	}
	// Loaded, retro is a module AND an offer: named once, not twice.
	if err := tr.Mount(qmlDoc(t, "import theme.retro 1.0\nText { text: Theme.menu.accent }")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	_, err = tr.Reconcile(qmlDoc(t, "import theme.retro 1.0\nimport nosuch\nText { text: Theme.menu.accent }"))
	if err == nil || strings.Count(err.Error(), "theme.retro") != 1 {
		t.Fatalf("err = %v, want theme.retro named exactly once", err)
	}
}

// TestImportsTheEngineRefusesSayWhy: a path import, a qualified name the
// module does not export, and a component whose name is not a type.
func TestImportsTheEngineRefusesSayWhy(t *testing.T) {
	// The QML parser refuses a path import itself; the engine takes trees from
	// ANY producer, so its own guard is reached with one built by hand.
	tr := decl.New(newReactor())
	pathImport := qml.SpecTree{Imports: []qml.SpecImport{{Module: ""}}, Root: &qml.SpecNode{Type: "Text"}}
	if err := tr.Mount(pathImport); !errors.Is(err, decl.ErrUndefinedModule) ||
		!strings.Contains(err.Error(), "not paths") {
		t.Errorf("a path import: err = %v, want it refused as a path", err)
	}
	tr = decl.New(newReactor())
	if err := tr.Mount(qmlDoc(t, "import tui 1.0 as T\nText { text: T.Nope.x }")); err == nil ||
		!strings.Contains(err.Error(), "exports no") {
		t.Errorf("a name the qualified module does not export: err = %v", err)
	}
	tr = decl.New(newReactor())
	if err := tr.OfferModule("ui", "", func() (decl.ModuleContents, error) {
		return decl.ModuleContents{Components: map[string]*qml.SpecNode{"": {Type: "Text"}}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, "import ui\nText { }")); !errors.Is(err, decl.ErrComponent) {
		t.Errorf("a component with no name: err = %v, want ErrComponent", err)
	}
}
