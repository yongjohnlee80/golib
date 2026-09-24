package decl_test

import (
	"errors"
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
	consts map[string]parse.SpecValue
}

func newConstReactor() *constReactor {
	return &constReactor{
		reactor: newReactor(),
		consts: map[string]parse.SpecValue{
			"tui.Horizontal": {Kind: parse.SpecValueToken, Raw: "horizontal"},
			"tui.Vertical":   {Kind: parse.SpecValueToken, Raw: "vertical"},
		},
	}
}

func (c *constReactor) Constants() map[string]parse.SpecValue { return c.consts }

func num(n string) parse.SpecValue {
	return parse.SpecValue{Kind: parse.SpecValueNumber, Raw: n}
}

// ref builds a reference the way the QML parser does, PATH INCLUDED — a resolver
// keyed on Raw alone would pass a test that hand-built the path wrong.
func ref(name string) parse.SpecValue {
	return parse.SpecValue{Kind: parse.SpecValueRef, Raw: name, Path: strings.Split(name, ".")}
}

func call(name string, args ...parse.SpecValue) parse.SpecValue {
	return parse.SpecValue{Kind: parse.SpecValueCall, Raw: name, Args: args}
}

// mountWith mounts a one-property schema built directly from a SpecValue, which
// is how a test reaches value shapes the QML surface does not yet spell.
func mountWith(t *testing.T, tr *decl.Tree, prop string, v parse.SpecValue) error {
	t.Helper()
	return tr.Mount(parse.SpecTree{Root: &parse.SpecNode{
		Type:  "Text",
		Props: []parse.SpecProp{{Name: prop, Value: v}},
	}})
}

// ------------------------------------------------------------- the leaf rule

func TestAnInjectedKindIsRefusedInAPositionItCannotOccupy(t *testing.T) {
	cases := []struct {
		name    string
		inject  func(*decl.Tree) error
		value   parse.SpecValue
		wantErr error
		// wantMsg is a phrase from the diagnostic. It names the ACTUAL problem,
		// so a refusal that fired for a different reason fails here even though
		// the sentinel matches.
		wantMsg string
	}{
		{
			name:    "a handler cannot produce a value",
			inject:  func(tr *decl.Tree) error { return tr.Inject("save", decl.Handle(func([]parse.SpecValue) error { return nil })) },
			value:   call("save"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "a handler performs an effect and cannot produce a value",
		},
		{
			name:    "a handler named but not called is still not a value",
			inject:  func(tr *decl.Tree) error { return tr.Inject("save", decl.Handle(func([]parse.SpecValue) error { return nil })) },
			value:   ref("save"),
			wantErr: decl.ErrWrongKind,
			wantMsg: "a handler performs an effect and cannot produce a value",
		},
		{
			name:    "a function must be called",
			inject:  func(tr *decl.Tree) error { return tr.Inject("upper", decl.Pure(func([]parse.SpecValue) (parse.SpecValue, error) { return sv("X"), nil })) },
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
		value   parse.SpecValue
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
			name:    "a known ADAPTER module with an unknown member",
			value:   ref("tui.Nope"),
			wantErr: decl.ErrNotResolvable,
			wantMsg: `"tui" has no member "Nope"`,
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
	if err := tr.Inject("join", decl.Pure(func(args []parse.SpecValue) (parse.SpecValue, error) {
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
	if err := tr.Inject("shout", decl.Pure(func(args []parse.SpecValue) (parse.SpecValue, error) {
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
	if err := mountWith(t, tr, "text", ref("tui.Horizontal")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	// The adapter must have received the TERMINAL, not the reference.
	var saw bool
	for _, l := range rec.trace {
		if strings.Contains(l, "horizontal") {
			saw = true
		}
		if strings.Contains(l, "tui.Horizontal") {
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
			inject:  func(tr *decl.Tree) error { return tr.Inject("tui.Horizontal", decl.Constant(sv("x"))) },
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

	if err := tr.DeclareFunc("f", func([]parse.SpecValue) (parse.SpecValue, error) { return sv("y"), nil }); err != nil {
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
	if err := tr.Inject("load", decl.Pure(func([]parse.SpecValue) (parse.SpecValue, error) {
		return parse.SpecValue{}, boom
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
