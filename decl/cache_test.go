package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// cache_test.go covers ClearComponentCache — Qt's QQmlEngine::clearComponentCache:
// loaded modules stay loaded until the host says their files changed, and the
// reconcile after that compares VALUES, so a line written the same under an
// edited theme is re-applied and one whose value held is not.

// themeFile is an offered value module whose "file" a test edits.
type themeFile struct {
	values map[string]string
	loads  int
	fail   error
}

func (f *themeFile) loader() decl.ModuleLoader {
	return func() (decl.ModuleContents, error) {
		f.loads++
		if f.fail != nil {
			return decl.ModuleContents{}, f.fail
		}
		c := decl.ModuleContents{Exports: []string{"Theme"}, Values: map[string]decl.Injected{}}
		for k, v := range f.values {
			c.Values["Theme."+k] = decl.Constant(sv(v))
		}
		return c, nil
	}
}

const cacheDoc = "import theme\nFlex { Text { id: a; text: Theme.title; hint: Theme.hint } }"

func themedTree(t *testing.T, f *themeFile) (*decl.Tree, *reactor) {
	t.Helper()
	rec := newReactor()
	tr := decl.New(rec)
	if err := tr.OfferModule("theme", "", f.loader()); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(mustSpec(t, cacheDoc)); err != nil {
		t.Fatal(err)
	}
	rec.trace = nil
	return tr, rec
}

func TestAnEditedModuleIsNotReadUntilTheCacheIsCleared(t *testing.T) {
	f := &themeFile{values: map[string]string{"title": "old", "hint": "h"}}
	tr, rec := themedTree(t, f)
	f.values["title"] = "new"

	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); err != nil {
		t.Fatal(err)
	}
	if f.loads != 1 || len(rec.trace) != 0 {
		t.Fatalf("without a clear the module was re-read (%d loads) or applied:\n%s", f.loads, strings.Join(rec.trace, "\n"))
	}

	if err := tr.ClearComponentCache(); err != nil {
		t.Fatal(err)
	}
	res, err := tr.Reconcile(mustSpec(t, cacheDoc))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := tr.NodeByID("a")
	// R9: only the value that moved is applied — the hint held.
	want := fmt.Sprintf("apply %d text=string(new) from-schema", a)
	if f.loads != 2 || strings.Join(rec.trace, "\n") != want || len(res.Rebuilt) != 0 {
		t.Fatalf("after a clear: %d loads, res %+v, trace:\n%s\nwant:\n%s", f.loads, res, strings.Join(rec.trace, "\n"), want)
	}

	// And the one after goes back to comparing declarations.
	rec.trace = nil
	f.values["title"] = "newer"
	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); err != nil || len(rec.trace) != 0 || f.loads != 2 {
		t.Fatalf("a reconcile after the refresh re-read or applied: %v %d\n%s", err, f.loads, strings.Join(rec.trace, "\n"))
	}
}

// TestARefusedReloadPutsThePreviousVersionBack: the edited theme lacks a
// name the document reads. The reload is refused, and the tree still resolves
// against the version it was built from — then the fixed file applies.
func TestARefusedReloadPutsThePreviousVersionBack(t *testing.T) {
	f := &themeFile{values: map[string]string{"title": "old", "hint": "h"}}
	tr, rec := themedTree(t, f)
	delete(f.values, "hint")
	_ = tr.ClearComponentCache()
	root := tr.Root()
	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); err == nil {
		t.Fatal("a theme without Theme.hint was accepted")
	}
	if len(rec.trace) != 0 || tr.Root() != root {
		t.Fatalf("a refused reload touched the tree:\n%s", strings.Join(rec.trace, "\n"))
	}
	// The broken file is still the file: the next reload reads it again, and
	// is refused again — the module stays stale until one succeeds.
	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); err == nil || f.loads != 3 {
		t.Fatalf("the second reload: %v after %d loads, want refused after 3", err, f.loads)
	}

	f.values["hint"] = "fixed"
	rec.trace = nil
	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rec.trace, "\n"), "hint=string(fixed)") {
		t.Fatalf("the fixed file never applied:\n%s", strings.Join(rec.trace, "\n"))
	}
}

// TestAFailedLoaderKeepsThePreviousVersion: a file that cannot be read now is
// not a module that vanished.
func TestAFailedLoaderKeepsThePreviousVersion(t *testing.T) {
	f := &themeFile{values: map[string]string{"title": "old", "hint": "h"}}
	tr, _ := themedTree(t, f)
	f.fail = errors.New("half-written")
	_ = tr.ClearComponentCache()
	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); !errors.Is(err, decl.ErrModuleLoad) {
		t.Fatalf("err = %v, want ErrModuleLoad", err)
	}
	f.fail = nil
	f.values["title"] = "later"
	if _, err := tr.Reconcile(mustSpec(t, cacheDoc)); err != nil {
		t.Fatalf("the module did not survive its loader failing: %v", err)
	}
}

// TestAConstructorPropertyReadingAnEditedValueRebuilds: a value taken at
// construction has no setter to re-apply through.
func TestAConstructorPropertyReadingAnEditedValueRebuilds(t *testing.T) {
	f := &themeFile{values: map[string]string{"dir": "row"}}
	rec := newReactor()
	tr := decl.New(rec)
	_ = tr.OfferModule("theme", "", f.loader())
	const src = "import theme\nFlex { direction: Theme.dir; Text { text: \"x\" } }"
	if err := tr.Mount(mustSpec(t, src)); err != nil {
		t.Fatal(err)
	}
	f.values["dir"] = "column"
	_ = tr.ClearComponentCache()
	res, err := tr.Reconcile(mustSpec(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rebuilt) != 1 || !strings.Contains(res.Rebuilt[0].Reason, "changed since the last load") {
		t.Fatalf("res = %+v, want the Flex rebuilt for its direction", res)
	}
}

// liveTree is a tree whose binding reads both a source and the theme, so it
// re-resolves the theme whenever the source moves — and counts its calls.
func liveTree(t *testing.T, f *themeFile) (*decl.Tree, *reactor, *int) {
	t.Helper()
	calls := 0
	rec := newReactor()
	tr := decl.New(rec)
	_ = tr.OfferModule("theme", "", f.loader())
	_ = tr.DeclareSource("s", sv("1"))
	_ = tr.DeclareFunc("join", func(a []qml.SpecValue) (qml.SpecValue, error) {
		calls++
		return sv(a[0].Raw + a[1].Raw), nil
	})
	if err := tr.Mount(mustSpec(t, liveDoc)); err != nil {
		t.Fatal(err)
	}
	rec.trace = nil
	return tr, rec, &calls
}

const liveDoc = "import theme\nText { id: a; text: join(s, Theme.title) }"

// resolvesTheme moves the source and requires the binding to re-resolve the
// theme to want.
func resolvesTheme(t *testing.T, tr *decl.Tree, rec *reactor, to, want string) {
	t.Helper()
	rec.trace = nil
	if _, err := tr.SetSource("s", sv(to)); err != nil {
		t.Fatalf("the live binding lost the theme it was built with: %v", err)
	}
	if got := strings.Join(rec.trace, "\n"); !strings.Contains(got, "text=string("+want+")") {
		t.Fatalf("trace:\n%s\nwant text=%s", got, want)
	}
}

// TestTheLiveTreeResolvesThePreviousVersionAfterARefusal: between a refused
// reload and the next save the program keeps running, and a binding that
// reads the theme re-evaluates when its source moves. It must still find the
// version the tree was built from — after a refused document, and after a
// loader that failed.
func TestTheLiveTreeResolvesThePreviousVersionAfterARefusal(t *testing.T) {
	f := &themeFile{values: map[string]string{"title": "T"}}
	tr, rec, _ := liveTree(t, f)
	f.values = map[string]string{} // the edited file lost Theme.title
	_ = tr.ClearComponentCache()
	if _, err := tr.Reconcile(mustSpec(t, liveDoc)); err == nil {
		t.Fatal("a theme without Theme.title was accepted")
	}
	resolvesTheme(t, tr, rec, "2", "2T")

	f.values, f.fail = map[string]string{"title": "U"}, errors.New("half-written")
	if _, err := tr.Reconcile(mustSpec(t, liveDoc)); err == nil {
		t.Fatal("a failed loader was accepted")
	}
	resolvesTheme(t, tr, rec, "3", "3T")
}

// TestTheRefreshEndsWithTheReconcileThatSucceeds: after it, a reload trusts
// unchanged bindings again and evaluates nothing.
func TestTheRefreshEndsWithTheReconcileThatSucceeds(t *testing.T) {
	f := &themeFile{values: map[string]string{"title": "T"}}
	tr, _, calls := liveTree(t, f)
	f.values["title"] = "U"
	_ = tr.ClearComponentCache()
	if _, err := tr.Reconcile(mustSpec(t, liveDoc)); err != nil {
		t.Fatal(err)
	}
	*calls = 0
	if _, err := tr.Reconcile(mustSpec(t, liveDoc)); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("an unchanged reload after the refresh evaluated the binding %d times", *calls)
	}
}
