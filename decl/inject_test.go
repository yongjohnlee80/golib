package decl_test

import (
	"errors"
	"github.com/yongjohnlee80/golib/parse/qml"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
)

// These assert the RESOLUTION MATRIX of ADR-decl-0003: what an injected name may
// be, in which position, and — for the ones that are refused — what the author
// is told. The diagnostic is asserted alongside the sentinel because the wrong
// reason is its own defect: "unbound name" for a handler used as a value sends
// the reader to check an import that is perfectly fine.

// constReactor is a reactor that also publishes an adapter vocabulary, which is
// the SECOND scope a reference can resolve in.
type constReactor struct {
	*reactor
	consts map[string]qml.SpecValue
}

func newConstReactor() *constReactor {
	return &constReactor{
		reactor: newReactor(),
		consts: map[string]qml.SpecValue{
			"Tui.Horizontal": {Kind: qml.SpecValueString, Raw: "horizontal"},
			"Tui.Vertical":   {Kind: qml.SpecValueString, Raw: "vertical"},
		},
	}
}

func (c *constReactor) Constants() map[string]qml.SpecValue { return c.consts }

func num(n string) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: n}
}

// ref builds a reference the way the QML parser does, PATH INCLUDED — a resolver
// keyed on Raw alone would pass a test that hand-built the path wrong.
func ref(name string) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueRef, Raw: name, Path: strings.Split(name, ".")}
}

func call(name string, args ...qml.SpecValue) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueCall, Raw: name, Args: args}
}

// mountWith mounts a one-property schema built directly from a SpecValue, which
// is how a test reaches value shapes the QML surface does not yet spell.
func mountWith(t *testing.T, tr *decl.Tree, prop string, v qml.SpecValue) error {
	t.Helper()
	return tr.Mount(qml.SpecTree{
		// The fakes declare a `tui` module, and a module's names resolve only
		// once a document has imported it — so the import belongs here for the
		// same reason it belongs at the top of a .qml file.
		Imports: []qml.SpecImport{{Module: "tui", Path: []string{"tui"}, Version: "1.0"}},
		Root: &qml.SpecNode{
			Type:  "Text",
			Props: []qml.SpecProp{{Name: prop, Value: v}},
		}})
}

// ------------------------------------------------------------- the leaf rule

func TestAnInjectedKindIsRefusedInAPositionItCannotOccupy(t *testing.T) {
	cases := []struct {
		name    string
		inject  func(*decl.Tree) error
		value   qml.SpecValue
		wantErr error
		// wantMsg is a phrase from the diagnostic. It names the ACTUAL problem,
		// so a refusal that fired for a different reason fails here even though
		// the sentinel matches.
		wantMsg string
	}{
		{
			name: "a handler cannot produce a value",
			inject: func(tr *decl.Tree) error {
				return tr.Inject("save", decl.Handle(func([]qml.SpecValue) error { return nil }))
			},
			value:   call("save"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "a handler performs an effect and cannot produce a value",
		},
		{
			name: "a handler named but not called is still not a value",
			inject: func(tr *decl.Tree) error {
				return tr.Inject("save", decl.Handle(func([]qml.SpecValue) error { return nil }))
			},
			value:   ref("save"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "a handler performs an effect and cannot produce a value",
		},
		{
			name: "a function must be called",
			inject: func(tr *decl.Tree) error {
				return tr.Inject("upper", decl.Pure(func([]qml.SpecValue) (qml.SpecValue, error) { return sv("X"), nil }))
			},
			value:   ref("upper"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "a function must be called to produce a value",
		},
		{
			name:    "a source cannot be called",
			inject:  func(tr *decl.Tree) error { return tr.Inject("count", decl.SourceValue(num("1"))) },
			value:   call("count"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "cannot be called",
		},
		{
			name:    "a constant cannot be called",
			inject:  func(tr *decl.Tree) error { return tr.Inject("Greeting", decl.Constant(sv("hi"))) },
			value:   call("Greeting"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "cannot be called",
		},
		{
			name:    "a namespace is not a value",
			inject:  func(tr *decl.Tree) error { return tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))) },
			value:   ref("Theme"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "name something inside it",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := decl.New(newReactor())
			if err := c.inject(tr); err != nil {
				t.Fatalf("inject: %v", err)
			}
			err := mountWith(t, tr, "text", c.value)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
		})
	}
}

// TestTheThreeResolutionFailuresAreDistinguishable.
//
// A consumer responds differently to each — fix the import, fix the member, fix
// the position — so one shared sentinel would force it to match on message text.
func TestTheThreeResolutionFailuresAreDistinguishable(t *testing.T) {
	cases := []struct {
		name    string
		value   qml.SpecValue
		wantErr error
		wantMsg string
	}{
		{
			name:    "nothing injected under a bare name",
			value:   ref("nosuch"),
			wantErr: decl.ErrNotInjected,
			wantMsg: `unbound name "nosuch"`,
		},
		{
			name:    "nothing injected under a qualified prefix",
			value:   ref("nosuch.member"),
			wantErr: decl.ErrNotInjected,
			wantMsg: `nothing named "nosuch" was injected`,
		},
		{
			name:    "a known module with an unknown member",
			value:   ref("Theme.nope"),
			wantErr: decl.ErrNotResolvable,
			wantMsg: `"Theme" has no member "nope"`,
		},
		{
			name:    "a known ADAPTER singleton with an unknown member",
			value:   ref("Tui.Nope"),
			wantErr: decl.ErrNotResolvable,
			wantMsg: `"Tui" has no member "Nope"`,
		},
		{
			name:    "a resolved name in a position it cannot occupy",
			value:   ref("Theme"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "name something inside it",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := decl.New(newConstReactor())
			if err := tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))); err != nil {
				t.Fatalf("inject: %v", err)
			}
			err := mountWith(t, tr, "text", c.value)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
		})
	}
}

// ---------------------------------------------------- the structural judgement

// TestEveryArgumentIsValidatedBeforeTheFunctionRuns.
//
// This is the rule the leaf table cannot express, and the reason the recursion
// is the judgement rather than the table. A function that has already run has
// already had whatever effect it has; discovering the schema mistake afterwards
// is discovering it too late.
func TestEveryArgumentIsValidatedBeforeTheFunctionRuns(t *testing.T) {
	tr := decl.New(newReactor())
	var ran int
	if err := tr.Inject("join", decl.Pure(func(args []qml.SpecValue) (qml.SpecValue, error) {
		ran++
		return sv("joined"), nil
	})); err != nil {
		t.Fatalf("inject join: %v", err)
	}
	if err := tr.Inject("ok", decl.Constant(sv("fine"))); err != nil {
		t.Fatalf("inject ok: %v", err)
	}

	// The BAD argument is last, so an implementation that validated as it went
	// would already have evaluated the good one — and one that validated nothing
	// would have run join itself.
	err := mountWith(t, tr, "text", call("join", ref("ok"), ref("nosuch")))
	if !errors.Is(err, decl.ErrNotInjected) {
		t.Fatalf("err = %v, want ErrNotInjected", err)
	}
	if ran != 0 {
		t.Errorf("the function ran %d times; a schema mistake must be found before any host call", ran)
	}
}

// TestANESTEDCallIsNotRunBeforeALaterArgumentIsValidated.
//
// The case the test above does NOT reach, and the one that was wrong: it
// watches the OUTER function, which a single-pass walk never reaches anyway.
// The inner call is a CHILD, and evaluating a child call IS running a host
// function — so in `outer(inner(), absent)` the inner one had already happened
// by the time the unresolved name was found.
//
// "Every child before any call" is only true of a walk that validates without
// evaluating, which is why there are two passes.
func TestANESTEDCallIsNotRunBeforeALaterArgumentIsValidated(t *testing.T) {
	var inner, outer int
	tr := decl.New(newReactor())
	if err := tr.Inject("inner", decl.Pure(func([]qml.SpecValue) (qml.SpecValue, error) {
		inner++
		return sv("ok"), nil
	})); err != nil {
		t.Fatalf("inject inner: %v", err)
	}
	if err := tr.Inject("outer", decl.Pure(func(a []qml.SpecValue) (qml.SpecValue, error) {
		outer++
		return a[0], nil
	})); err != nil {
		t.Fatalf("inject outer: %v", err)
	}

	err := mountWith(t, tr, "text", call("outer", call("inner"), ref("absent")))
	if !errors.Is(err, decl.ErrNotInjected) {
		t.Fatalf("err = %v, want ErrNotInjected", err)
	}
	if inner != 0 || outer != 0 {
		t.Errorf("inner ran %d and outer ran %d; a schema mistake must be found "+
			"before ANY host call, nested ones included", inner, outer)
	}

	// The positive half: with the name bound, both DO run, exactly once each.
	// A validation pass that also evaluated would double every call.
	tr2 := decl.New(newReactor())
	inner, outer = 0, 0
	if err := tr2.Inject("inner", decl.Pure(func([]qml.SpecValue) (qml.SpecValue, error) {
		inner++
		return sv("ok"), nil
	})); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := tr2.Inject("outer", decl.Pure(func(a []qml.SpecValue) (qml.SpecValue, error) {
		outer++
		return a[0], nil
	})); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := tr2.Inject("here", decl.Constant(sv("x"))); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := mountWith(t, tr2, "text", call("outer", call("inner"), ref("here"))); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if inner != 1 || outer != 1 {
		t.Errorf("inner ran %d and outer ran %d, want 1 each — a validation pass "+
			"that also evaluated would double every call", inner, outer)
	}
}

// TestASourceReachedOnlyThroughACallIsStillTracked.
//
// The dependency set comes from the same recursion that evaluates, so a source
// buried in a nested argument is a dependency. Without this, the binding would
// evaluate correctly ONCE and then never recompute — a defect with no symptom
// until the source moves.
func TestASourceReachedOnlyThroughACallIsStillTracked(t *testing.T) {
	rec := newReactor()
	tr := decl.New(rec)
	if err := tr.Inject("name", decl.SourceValue(sv("ada"))); err != nil {
		t.Fatalf("inject name: %v", err)
	}
	if err := tr.Inject("shout", decl.Pure(func(args []qml.SpecValue) (qml.SpecValue, error) {
		return sv(strings.ToUpper(args[0].Raw)), nil
	})); err != nil {
		t.Fatalf("inject shout: %v", err)
	}
	if err := mountWith(t, tr, "text", call("shout", call("shout", ref("name")))); err != nil {
		t.Fatalf("mount: %v", err)
	}
	rec.trace = nil

	res, err := tr.SetSource("name", sv("grace"))
	if err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if res.Recomputed != 1 || res.Applied != 1 {
		t.Fatalf("result = %+v, want one recompute and one apply", res)
	}
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "GRACE") {
		t.Errorf("applied %v, want the nested source to have reached the setter", got)
	}
}

// TestAQualifiedSourceIsTracked.
//
// Tracking follows the INJECTED KIND, not the shape of the name. The predicate
// this replaced keyed on path length, which made every dotted name a constant —
// so a `Theme.surface` palette source would have updated and repainted nothing.
func TestAQualifiedSourceIsTracked(t *testing.T) {
	rec := newReactor()
	tr := decl.New(rec)
	if err := tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := mountWith(t, tr, "text", ref("Theme.surface")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	rec.trace = nil

	res, err := tr.SetSource("Theme.surface", sv("#eee"))
	if err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("result = %+v, want the qualified source to have repainted", res)
	}
	if got := applyLines(rec); len(got) != 1 || !strings.Contains(got[0], "#eee") {
		t.Errorf("applied %v, want the new palette value", got)
	}
}

// TestAnAdapterConstantIsResolvedAndNotTracked.
//
// The counterpart: a constant resolves to its value and creates no dependency,
// so it costs the reactive graph nothing.
func TestAnAdapterConstantIsResolvedAndNotTracked(t *testing.T) {
	rec := newConstReactor()
	tr := decl.New(rec)
	if err := mountWith(t, tr, "text", ref("Tui.Horizontal")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	// The adapter must have received the TERMINAL, not the reference.
	var saw bool
	for _, l := range rec.trace {
		if strings.Contains(l, "horizontal") {
			saw = true
		}
		if strings.Contains(l, "Tui.Horizontal") {
			t.Errorf("the adapter received the unresolved reference: %q", l)
		}
	}
	if !saw {
		t.Errorf("trace = %v, want the resolved constant", rec.trace)
	}
}

// -------------------------------------------------------- injection is a gate

func TestInjectionRefusesNamesThatWouldChangeMeaningSilently(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*decl.Tree) error
		inject  func(*decl.Tree) error
		wantErr error
		wantMsg string
	}{
		{
			name:    "a reserved name",
			inject:  func(tr *decl.Tree) error { return tr.Inject("parent", decl.Constant(sv("x"))) },
			wantErr: decl.ErrReservedName,
			wantMsg: `"parent"`,
		},
		{
			name:    "a reserved name in a LATER segment",
			inject:  func(tr *decl.Tree) error { return tr.Inject("Theme.root", decl.Constant(sv("x"))) },
			wantErr: decl.ErrReservedName,
			wantMsg: `"root"`,
		},
		{
			name:    "a name the adapter already defines",
			inject:  func(tr *decl.Tree) error { return tr.Inject("Tui.Horizontal", decl.Constant(sv("x"))) },
			wantErr: decl.ErrAmbiguousName,
			wantMsg: "the adapter already defines",
		},
		{
			name:    "the same name twice",
			setup:   func(tr *decl.Tree) error { return tr.Inject("count", decl.SourceValue(num("1"))) },
			inject:  func(tr *decl.Tree) error { return tr.Inject("count", decl.Constant(num("2"))) },
			wantErr: decl.ErrDuplicateDecl,
			wantMsg: "already injected as a source",
		},
		{
			name:    "a member of something that is not a module",
			setup:   func(tr *decl.Tree) error { return tr.Inject("Theme", decl.Constant(sv("dark"))) },
			inject:  func(tr *decl.Tree) error { return tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))) },
			wantErr: decl.ErrWrongKind,
			wantMsg: `"Theme" is a constant`,
		},
		{
			name:    "no kind at all",
			inject:  func(tr *decl.Tree) error { return tr.Inject("x", decl.Injected{}) },
			wantErr: decl.ErrWrongKind,
			wantMsg: "no kind was declared",
		},
		{
			name:    "an empty segment",
			inject:  func(tr *decl.Tree) error { return tr.Inject("a..b", decl.Constant(sv("x"))) },
			wantErr: decl.ErrWrongKind,
			wantMsg: "empty segment",
		},
		{
			name:    "a source holding an expression",
			inject:  func(tr *decl.Tree) error { return tr.Inject("s", decl.SourceValue(ref("other"))) },
			wantErr: decl.ErrNotTerminal,
			wantMsg: "holds a",
		},
		{
			name:    "a function with no implementation",
			inject:  func(tr *decl.Tree) error { return tr.Inject("f", decl.Pure(nil)) },
			wantErr: decl.ErrWrongKind,
			wantMsg: "no implementation",
		},
		{
			name:    "a handler with no implementation",
			inject:  func(tr *decl.Tree) error { return tr.Inject("h", decl.Handle(nil)) },
			wantErr: decl.ErrWrongKind,
			wantMsg: "no implementation",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := decl.New(newConstReactor())
			if c.setup != nil {
				if err := c.setup(tr); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}
			err := c.inject(tr)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
		})
	}
}

// TestInjectionIsRefusedOncePlanningHasBegun.
//
// The set a schema is checked against must be fixed before the check runs; a
// name injected afterwards would make an already-refused schema retroactively
// legal without anything re-checking it.
func TestInjectionIsRefusedOncePlanningHasBegun(t *testing.T) {
	tr := decl.New(newReactor())
	if err := mountWith(t, tr, "text", sv("hello")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	err := tr.Inject("late", decl.Constant(sv("x")))
	if err == nil {
		t.Fatal("injection after Mount was accepted")
	}
	if !strings.Contains(err.Error(), "before Mount") {
		t.Errorf("diagnostic = %q, want it to name the ordering rule", err)
	}
}

// TestTheOldDeclarationSurfaceWritesTheSameRegistry.
//
// DeclareSource and Inject are one write path. Two registries would let a name
// be a source in one and a function in the other, and which answer a schema got
// would depend on which map the resolver happened to consult first.
func TestTheOldDeclarationSurfaceWritesTheSameRegistry(t *testing.T) {
	tr := decl.New(newReactor())
	if err := tr.DeclareSource("count", num("1")); err != nil {
		t.Fatalf("DeclareSource: %v", err)
	}
	in, ok := tr.Lookup("count")
	if !ok || in.Kind != decl.KindSource {
		t.Fatalf("Lookup(count) = %v, %v; want a source", in.Kind, ok)
	}
	if err := tr.Inject("count", decl.Constant(sv("x"))); !errors.Is(err, decl.ErrDuplicateDecl) {
		t.Errorf("re-injecting a declared source = %v, want ErrDuplicateDecl", err)
	}

	if err := tr.DeclareFunc("f", func([]qml.SpecValue) (qml.SpecValue, error) { return sv("y"), nil }); err != nil {
		t.Fatalf("DeclareFunc: %v", err)
	}
	if in, ok := tr.Lookup("f"); !ok || in.Kind != decl.KindPureFunction {
		t.Errorf("Lookup(f) = %v, %v; want a function", in.Kind, ok)
	}
}

// TestAHostFunctionsOwnErrorIsReportedAsItsOwn.
//
// A function that fails is not a name used in the wrong place, and reporting it
// as one sends the reader to check a schema that is correct.
func TestAHostFunctionsOwnErrorIsReportedAsItsOwn(t *testing.T) {
	boom := errors.New("the database is down")
	tr := decl.New(newReactor())
	if err := tr.Inject("load", decl.Pure(func([]qml.SpecValue) (qml.SpecValue, error) {
		return qml.SpecValue{}, boom
	})); err != nil {
		t.Fatalf("inject: %v", err)
	}
	err := mountWith(t, tr, "text", call("load"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the host's own error", err)
	}
	if errors.Is(err, decl.ErrWrongKind) {
		t.Errorf("err = %v, want it NOT to claim the name was misused", err)
	}
}

// ------------------------------------------------------------ handler bodies

// mountSrc parses QML and mounts it, so a handler test reads as the schema an
// author would actually write.
func mountSrc(t *testing.T, tr *decl.Tree, src string) error {
	t.Helper()
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return tr.Mount(spec)
}

// TestABareHandlerNameIsRefusedRatherThanInvoked.
//
// `onClicked: save` is the IDENTIFIER save. It is NOT a call to it, and an
// engine that invokes it anyway is guessing at the author's meaning in the one
// place guessing costs the most: the difference is invisible in the source, and
// the wrong guess runs an effect nobody asked for.
//
// The parser keeps the distinction; this is where it is acted on.
func TestABareHandlerNameIsRefusedRatherThanInvoked(t *testing.T) {
	var ran int
	tr := decl.New(newReactor())
	if err := tr.Inject("save", decl.Handle(func([]qml.SpecValue) error {
		ran++
		return nil
	})); err != nil {
		t.Fatalf("inject: %v", err)
	}

	err := mountSrc(t, tr, "Button {\n  onClicked: save\n}")
	if !errors.Is(err, decl.ErrHandlerBody) {
		t.Fatalf("err = %v, want ErrHandlerBody", err)
	}
	if !strings.Contains(err.Error(), "save()") {
		t.Errorf("diagnostic = %q, want it to show the author the call form", err)
	}
	if ran != 0 {
		t.Errorf("the handler ran %d times; naming a handler must not invoke it", ran)
	}

	// The positive half, at the same limit: written as a call, it binds and runs.
	tr2 := decl.New(newReactor())
	var ran2 int
	if err := tr2.Inject("save", decl.Handle(func([]qml.SpecValue) error {
		ran2++
		return nil
	})); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := mountSrc(t, tr2, "Button {\n  onClicked: save()\n}"); err != nil {
		t.Fatalf("a called handler was refused: %v", err)
	}
	if err := tr2.Emit(tr2.Root(), "clicked"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if ran2 != 1 {
		t.Errorf("the handler ran %d times, want 1", ran2)
	}
}

// TestAHandlerBodyThisEngineCannotRunSaysSoWithoutClaimingASyntaxError.
//
// The source is valid QML in every row. What is true is that THIS ENGINE does
// not run it, and saying that is a different statement from "you wrote this
// wrong" — only one of which is honest, and only one of which tells a reader
// that the next release might accept it.
func TestAHandlerBodyThisEngineCannotRunSaysSoWithoutClaimingASyntaxError(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"a declaration", "{ let x = 1\n save() }", "declaration statement"},
		{"a conditional", "{ if (a) save() }", "if statement"},
		{"a return", "{ return save() }", "return statement"},
		{"an operator", "a + b", "binary expression"},
		{"a call on a computed member", "handlers[k]()", "not a name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := decl.New(newReactor())
			if err := tr.Inject("save", decl.Handle(func([]qml.SpecValue) error { return nil })); err != nil {
				t.Fatalf("inject: %v", err)
			}
			err := mountSrc(t, tr, "Button {\n  onClicked: "+c.body+"\n}")
			if !errors.Is(err, decl.ErrHandlerBody) {
				t.Fatalf("err = %v, want ErrHandlerBody", err)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to name %q", err, c.wantMsg)
			}
		})
	}
}

// TestAHandlerRunsItsStatementsInOrderAndStopsAtTheFirstFailure.
func TestAHandlerRunsItsStatementsInOrderAndStopsAtTheFirstFailure(t *testing.T) {
	var ran []string
	boom := errors.New("the first one failed")
	tr := decl.New(newReactor())
	give := func(name string, err error) {
		if e := tr.Inject(name, decl.Handle(func([]qml.SpecValue) error {
			ran = append(ran, name)
			return err
		})); e != nil {
			t.Fatalf("inject %q: %v", name, e)
		}
	}
	give("first", nil)
	give("second", boom)
	give("third", nil)

	if err := mountSrc(t, tr, "Button {\n  onClicked: { first()\n second()\n third() }\n}"); err != nil {
		t.Fatalf("mount: %v", err)
	}
	err := tr.Emit(tr.Root(), "clicked")
	if !errors.Is(err, boom) {
		t.Fatalf("Emit = %v, want the failing statement's error", err)
	}
	if strings.Join(ran, ",") != "first,second" {
		t.Errorf("ran %v, want the tail of a failed handler NOT to run", ran)
	}
}

// TestAHandlerArgumentIsResolvedThroughTheSameMatrix.
//
// Arguments are values wherever a call sits, which is what lets a handler read
// a source without this engine running JavaScript at all.
func TestAHandlerArgumentIsResolvedThroughTheSameMatrix(t *testing.T) {
	var got []qml.SpecValue
	tr := decl.New(newReactor())
	if err := tr.Inject("count", decl.SourceValue(num("1"))); err != nil {
		t.Fatalf("inject count: %v", err)
	}
	if err := tr.Inject("submit", decl.Handle(func(args []qml.SpecValue) error {
		got = args
		return nil
	})); err != nil {
		t.Fatalf("inject submit: %v", err)
	}
	if err := mountSrc(t, tr, "Button {\n  onClicked: submit(count, \"now\")\n}"); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := tr.Emit(tr.Root(), "clicked"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(got) != 2 || got[0].Raw != "1" || got[1].Raw != "now" {
		t.Fatalf("args = %+v, want the source's value and the literal", got)
	}

	// An argument naming something unbound is refused at MOUNT, not at the
	// first click — a handler nobody has pressed yet is still a schema mistake.
	tr2 := decl.New(newReactor())
	if err := tr2.Inject("submit", decl.Handle(func([]qml.SpecValue) error { return nil })); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if err := mountSrc(t, tr2, "Button {\n  onClicked: submit(nosuch)\n}"); !errors.Is(err, decl.ErrNotInjected) {
		t.Errorf("err = %v, want the unbound argument refused at mount", err)
	}
}

// TestOnlyAHandlerMayBeCalledFromASignalBody.
//
// The other half of the matrix's handler column. A source or a pure function is
// a VALUE — naming one where an effect belongs is a schema mistake, and it has
// to be caught at the gate rather than at the call: an injected function has no
// Handle to invoke, so an engine that skipped the check would reach a nil
// callback on the first click.
func TestOnlyAHandlerMayBeCalledFromASignalBody(t *testing.T) {
	cases := []struct {
		name    string
		inject  func(*decl.Tree) error
		body    string
		wantMsg string
	}{
		{
			name: "a pure function",
			inject: func(tr *decl.Tree) error {
				return tr.Inject("total", decl.Pure(func([]qml.SpecValue) (qml.SpecValue, error) { return num("1"), nil }))
			},
			body:    "total()",
			wantMsg: "a function produces a value, and a signal wants an effect",
		},
		{
			name:    "a source",
			inject:  func(tr *decl.Tree) error { return tr.Inject("count", decl.SourceValue(num("1"))) },
			body:    "count()",
			wantMsg: "cannot be called",
		},
		{
			name:    "a constant",
			inject:  func(tr *decl.Tree) error { return tr.Inject("Greeting", decl.Constant(sv("hi"))) },
			body:    "Greeting()",
			wantMsg: "cannot be called",
		},
		{
			name:    "a namespace",
			inject:  func(tr *decl.Tree) error { return tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))) },
			body:    "Theme()",
			wantMsg: "a namespace is not a value",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := decl.New(newReactor())
			if err := c.inject(tr); err != nil {
				t.Fatalf("inject: %v", err)
			}
			err := mountSrc(t, tr, "Button {\n  onClicked: "+c.body+"\n}")
			if !errors.Is(err, decl.ErrWrongKind) {
				t.Fatalf("err = %v, want ErrWrongKind", err)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
		})
	}
}

// TestAnEmptyHandlerBodyIsRefused.
//
// The QML surface cannot produce one — `onClicked:` with nothing after it is a
// truncated file, which the parser reports as incomplete. A consumer BUILDING a
// SpecTree can, though, and a handler that binds a signal to nothing would wire
// a live callback that does nothing rather than report the mistake.
func TestAnEmptyHandlerBodyIsRefused(t *testing.T) {
	tr := decl.New(newReactor())
	err := tr.Mount(qml.SpecTree{Root: &qml.SpecNode{
		Type:     "Button",
		Handlers: []qml.SpecHandler{{Signal: "clicked", Pos: parse.Position{Line: 1, Column: 1}}},
	}})
	if !errors.Is(err, decl.ErrHandlerBody) {
		t.Fatalf("err = %v, want ErrHandlerBody", err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("diagnostic = %q, want it to say the handler is empty", err)
	}
}

// ----------------------------------------------------------- expression values

// TestAnExpressionValueIsDeclinedAndNeverForwarded.
//
// The parser reads every JavaScript expression QML allows; this engine
// evaluates a subset. The two things that must both be true:
//
//   - the refusal says the ENGINE is the limit, not that the document is wrong,
//     because the document is valid QML and a reader who "fixes" it is fixing
//     nothing;
//   - the value never reaches the adapter. A kind that resolution does not know
//     about is not rejected by default — it sails past and arrives at a setter
//     as an un-evaluated tree, which is a defect with no diagnostic at all.
func TestAnExpressionValueIsDeclinedAndNeverForwarded(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"arithmetic", `Text { text: a + b }`, `the operator "+"`},
		{"a comparison", `Text { text: count > 0 }`, `the operator ">"`},
		{"a conditional", `Text { text: a ? b : c }`, "a conditional"},
		{"an index", `Text { text: items[0] }`, "member expression"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRecorder()
			tr := decl.New(r)
			err := tr.Mount(qmlDoc(t, c.src))
			if !errors.Is(err, decl.ErrExpressionValue) {
				t.Fatalf("err = %v, want ErrExpressionValue", err)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("diagnostic = %q, want it to contain %q", err, c.wantMsg)
			}
			// It must not read as the author's mistake.
			if errors.Is(err, decl.ErrNotInjected) || errors.Is(err, decl.ErrWrongKind) {
				t.Errorf("err = %v, want it to blame this engine's reach, not the document", err)
			}
			for _, line := range r.trace {
				if strings.HasPrefix(line, "apply") {
					t.Errorf("an un-evaluated expression reached the adapter: %q", line)
				}
			}
		})
	}
}

// ------------------------------------------------ partial state and timing

// TestAHandlerArgumentIsEvaluatedWHENTHESIGNALFIRES.
//
// The first version validated AND evaluated arguments at compile time and baked
// the results into the closure, so `submit(count)` submitted the count the
// screen had when it was BUILT — forever, however many times the source moved.
//
// The doc comment claimed the opposite, which is what made it survive a
// mutation pass: nothing asserted the timing, and the prose asserting it was
// not a mechanism.
func TestAHandlerArgumentIsEvaluatedWHENTHESIGNALFIRES(t *testing.T) {
	var got []string
	tr := decl.New(newReactor())
	if err := tr.Inject("count", decl.SourceValue(num("1"))); err != nil {
		t.Fatalf("inject count: %v", err)
	}
	if err := tr.Inject("submit", decl.Handle(func(args []qml.SpecValue) error {
		got = append(got, args[0].Raw)
		return nil
	})); err != nil {
		t.Fatalf("inject submit: %v", err)
	}
	if err := mountSrc(t, tr, "Button {\n  onClicked: submit(count)\n}"); err != nil {
		t.Fatalf("mount: %v", err)
	}

	if err := tr.Emit(tr.Root(), "clicked"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if _, err := tr.SetSource("count", num("2")); err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if err := tr.Emit(tr.Root(), "clicked"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if strings.Join(got, ",") != "1,2" {
		t.Errorf("handler saw %v, want [1 2] — the second press must read the "+
			"value the source holds THEN, not the one it held at mount", got)
	}
}

// TestARefusedInjectionLeavesNothingBehind.
//
// The name was written into the registry BEFORE its prefixes were validated, so
// a refused injection left its own name resolvable: the caller was told no, and
// a schema could still reach what it had been told did not exist.
func TestARefusedInjectionLeavesNothingBehind(t *testing.T) {
	tr := decl.New(newReactor())
	if err := tr.Inject("Theme", decl.Constant(sv("dark"))); err != nil {
		t.Fatalf("inject Theme: %v", err)
	}
	if err := tr.Inject("Theme.surface", decl.SourceValue(sv("#111"))); err == nil {
		t.Fatal("injecting a member of a constant was accepted")
	}
	if _, ok := tr.Lookup("Theme.surface"); ok {
		t.Error("a refused injection left its name in the registry")
	}
	if _, ok := tr.Source("Theme.surface"); ok {
		t.Error("a refused injection left a source behind")
	}
	// And the name that WAS there is untouched.
	if in, ok := tr.Lookup("Theme"); !ok || in.Kind != decl.KindConstant {
		t.Errorf("Theme = %v, %v; the refusal disturbed an unrelated name", in.Kind, ok)
	}
}
