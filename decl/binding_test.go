package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
)

// reactor is a splicer that also classifies properties, which a schema with
// bindings requires (ADR-decl-0001b R11). The plain splicer deliberately does
// NOT, so the two fakes together cover both sides of that rule.
type reactor struct {
	*splicer
	// runtime and ctor name the properties of each kind, by type.
	runtime map[string]map[string]bool
	ctor    map[string]map[string]bool
}

func newReactor() *reactor {
	return &reactor{
		splicer: newSplicer(),
		runtime: map[string]map[string]bool{
			"Text":   {"text": true, "hint": true},
			"Box":    {"title": true},
			"Flex":   {},
			"Button": {"label": true},
		},
		ctor: map[string]map[string]bool{
			"Flex":  {"direction": true},
			"Split": {"orientation": true},
		},
	}
}

func (r *reactor) ClassifyProperty(typeName, prop string) decl.PropertyKind {
	if r.runtime[typeName][prop] {
		return decl.PropRuntime
	}
	if r.ctor[typeName][prop] {
		return decl.PropConstructorOnly
	}
	return decl.PropUnknown
}

func sv(s string) parse.SpecValue {
	return parse.SpecValue{Kind: parse.SpecValueString, Raw: s}
}

// tree mounts src against a classifying adapter, with the given sources and
// functions declared first, and clears the trace so a test sees only what came
// after.
func tree(t *testing.T, rec *reactor, src string,
	sources map[string]string, funcs map[string]decl.ValueFunc) *decl.Tree {
	t.Helper()
	tr := decl.New(rec)
	for n, v := range sources {
		if err := tr.DeclareSource(n, sv(v)); err != nil {
			t.Fatalf("DeclareSource %q: %v", n, err)
		}
	}
	for n, f := range funcs {
		if err := tr.DeclareFunc(n, f); err != nil {
			t.Fatalf("DeclareFunc %q: %v", n, err)
		}
	}
	if err := tr.Mount(wiredSpec(t, tr, rec.recorder, src)); err != nil {
		t.Fatalf("fixture mount: %v", err)
	}
	rec.trace = nil
	return tr
}

func applyLines(rec *reactor) []string {
	var out []string
	for _, l := range rec.trace {
		if strings.HasPrefix(l, "apply") {
			out = append(out, l)
		}
	}
	return out
}

// ------------------------------------------------------- declaration lifecycle

// TestSourceAndFuncDeclarationLifecycle — rows 9 and 17.
//
// Each case is listed because it has ONE answer, and a design where "unknown"
// only means "not pushed yet" cannot refuse an unknown reference at planning.
func TestSourceAndFuncDeclarationLifecycle(t *testing.T) {
	t.Run("duplicate source", func(t *testing.T) {
		tr := decl.New(newReactor())
		if err := tr.DeclareSource("a", sv("1")); err != nil {
			t.Fatal(err)
		}
		if err := tr.DeclareSource("a", sv("2")); !errors.Is(err, decl.ErrDuplicateDecl) {
			t.Fatalf("err = %v, want ErrDuplicateDecl", err)
		}
	})
	t.Run("duplicate func", func(t *testing.T) {
		tr := decl.New(newReactor())
		f := func([]parse.SpecValue) (parse.SpecValue, error) { return sv(""), nil }
		if err := tr.DeclareFunc("f", f); err != nil {
			t.Fatal(err)
		}
		if err := tr.DeclareFunc("f", f); !errors.Is(err, decl.ErrDuplicateDecl) {
			t.Fatalf("err = %v, want ErrDuplicateDecl", err)
		}
	})
	t.Run("nil func is refused at declaration", func(t *testing.T) {
		// Not discovered at the first call, which could be a reload away.
		tr := decl.New(newReactor())
		if err := tr.DeclareFunc("f", nil); err == nil {
			t.Fatal("a nil function was accepted")
		}
	})
	t.Run("declaration after Mount", func(t *testing.T) {
		rec := newReactor()
		tr := tree(t, rec, `Flex { Text { id: a text: "x" } }`, nil, nil)
		if err := tr.DeclareSource("late", sv("v")); !errors.Is(err, decl.ErrPhase) {
			t.Errorf("source: err = %v, want ErrPhase", err)
		}
		if err := tr.DeclareFunc("late", func([]parse.SpecValue) (parse.SpecValue, error) {
			return sv(""), nil
		}); !errors.Is(err, decl.ErrPhase) {
			t.Errorf("func: err = %v, want ErrPhase", err)
		}
	})
	t.Run("SetSource of an undeclared name", func(t *testing.T) {
		rec := newReactor()
		tr := tree(t, rec, `Flex { Text { id: a text: "x" } }`, nil, nil)
		if _, err := tr.SetSource("nope", sv("v")); !errors.Is(err, decl.ErrNoSuchSource) {
			t.Fatalf("err = %v, want ErrNoSuchSource", err)
		}
	})
	t.Run("an expression is not a runtime value", func(t *testing.T) {
		// Row 7. A Ref or Call as a SOURCE value is what would make sources
		// depend on sources, and the bipartite graph rests on them not.
		tr := decl.New(newReactor())
		for _, v := range []parse.SpecValue{
			{Kind: parse.SpecValueRef, Raw: "other"},
			{Kind: parse.SpecValueCall, Raw: "f"},
			{Kind: parse.SpecValueRef, Raw: "bare"},
		} {
			if err := tr.DeclareSource("s"+v.Raw, v); !errors.Is(err, decl.ErrNotTerminal) {
				t.Errorf("DeclareSource(%s): err = %v, want ErrNotTerminal", v.Kind, err)
			}
		}
		if err := tr.DeclareSource("ok", sv("v")); err != nil {
			t.Fatal(err)
		}
		if _, err := tr.SetSource("ok", parse.SpecValue{Kind: parse.SpecValueRef, Raw: "x"}); !errors.Is(err, decl.ErrNotTerminal) {
			t.Errorf("SetSource: err = %v, want ErrNotTerminal", err)
		}
	})
	t.Run("unknown and non-terminal functions refuse during planning", func(t *testing.T) {
		// Row 17.
		rec := newReactor()
		tr := decl.New(rec)
		_ = tr.DeclareSource("g", sv("v"))
		_ = tr.DeclareFunc("bad", func([]parse.SpecValue) (parse.SpecValue, error) {
			return parse.SpecValue{Kind: parse.SpecValueRef, Raw: "expr"}, nil
		})
		// `good` returns a proper terminal, so when it is used below only the
		// ARGUMENT check can refuse. An earlier version used `bad` for the
		// argument case too, and passed because the non-terminal RESULT was
		// refused — proving the wrong rule.
		_ = tr.DeclareFunc("good", func([]parse.SpecValue) (parse.SpecValue, error) {
			return sv("fine"), nil
		})
		for name, src := range map[string]string{
			"unknown function":      `Flex { Text { id: a text: missing(g) } }`,
			"non-terminal result":   `Flex { Text { id: a text: bad(g) } }`,
			"bare word in the call": `Flex { Text { id: a text: good(horizontal) } }`,
		} {
			t.Run(name, func(t *testing.T) {
				err := tr.Mount(mustSpec(t, src))
				if err == nil {
					t.Fatal("accepted")
				}
				if tr.Len() != 0 {
					t.Errorf("row 19: %d nodes left behind", tr.Len())
				}
				for _, l := range rec.trace {
					if strings.HasPrefix(l, "create") || strings.HasPrefix(l, "apply") {
						t.Errorf("the adapter was touched: %q", l)
					}
				}
			})
		}
	})
}

// ------------------------------------------------------------- binding basics

// TestDependenciesAreExactlyTheSourcesWritten — rows 3 and 13.
func TestDependenciesAreExactlyTheSourcesWritten(t *testing.T) {
	rec := newReactor()
	calls := 0
	tr := tree(t, rec,
		`Flex { Text { id: a text: join(x, "lit", x) } Text { id: b text: y } }`,
		map[string]string{"x": "1", "y": "2", "unrelated": "3"},
		map[string]decl.ValueFunc{"join": func(a []parse.SpecValue) (parse.SpecValue, error) {
			calls++
			return sv(a[0].Raw + a[1].Raw + a[2].Raw), nil
		}})

	// A duplicate x is ONE dependency and ONE recompute.
	calls = 0
	res, err := tr.SetSource("x", sv("9"))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("a duplicated source caused %d evaluations, want 1", calls)
	}
	if res.Recomputed != 1 || res.Applied != 1 {
		t.Errorf("res = %+v, want 1 recomputed / 1 applied", res)
	}

	// An unrelated source recomputes nothing at all.
	calls = 0
	rec.trace = nil
	res, err = tr.SetSource("unrelated", sv("changed"))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || res.Recomputed != 0 || len(rec.trace) != 0 {
		t.Errorf("an unrelated source did work: calls=%d res=%+v trace=%v", calls, res, rec.trace)
	}
}

// TestAnUnchangedDerivedValueNeverReachesTheSetter — rows 2 and 12.
//
// The two halves are different claims. A source set to the value it already has
// does nothing at all. A source set to a DIFFERENT value whose derived result is
// the same must recompute — and still reach no setter, because the engine owns
// that comparison. The widgets do not agree about it: SetLabel and SetText
// self-guard, SetTitle and SetStatus assign and invalidate unconditionally.
func TestAnUnchangedDerivedValueNeverReachesTheSetter(t *testing.T) {
	rec := newReactor()
	calls := 0
	tr := tree(t, rec, `Flex { Text { id: a text: constant(x) } }`,
		map[string]string{"x": "1"},
		map[string]decl.ValueFunc{"constant": func([]parse.SpecValue) (parse.SpecValue, error) {
			calls++
			return sv("always the same"), nil
		}})

	calls = 0
	res, err := tr.SetSource("x", sv("1")) // the value it already has
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || res != (decl.PropagationResult{}) {
		t.Errorf("an unchanged SOURCE did work: calls=%d res=%+v", calls, res)
	}

	calls = 0
	res, err = tr.SetSource("x", sv("2")) // different source, same derived value
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("evaluations = %d, want 1", calls)
	}
	if res.Recomputed != 1 || res.Quiet != 1 || res.Applied != 0 {
		t.Errorf("res = %+v, want 1 recomputed / 1 quiet / 0 applied", res)
	}
	if got := applyLines(rec); len(got) != 0 {
		t.Errorf("an unchanged derived value reached the setter: %v", got)
	}
}

// TestFanOutIsDocumentOrderAndStopsOnFailure — row 14.
func TestFanOutIsDocumentOrderAndStopsOnFailure(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec,
		`Flex { Text { id: a text: x } Text { id: b text: x } Text { id: c text: x } }`,
		map[string]string{"x": "0"}, nil)

	// Document order, all three.
	res, err := tr.SetSource("x", sv("1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 3 {
		t.Fatalf("Applied = %d, want 3", res.Applied)
	}
	var order []string
	for _, l := range applyLines(rec) {
		order = append(order, strings.Fields(l)[1])
	}
	if !equalTrace(order, []string{"2", "3", "4"}) {
		t.Errorf("fan-out order = %v, want document order [2 3 4]", order)
	}

	// The middle one refuses: the first stays applied, the third never runs.
	rec.trace = nil
	boom := errors.New("this setter refuses")
	rec.applyErr["text"] = nil
	rec.onApply = func(a decl.Application) {
		if a.Node == 3 {
			rec.applyErr["text"] = boom
		} else {
			rec.applyErr["text"] = nil
		}
	}
	res, err = tr.SetSource("x", sv("2"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the setter's error", err)
	}
	if res.Applied != 1 {
		t.Errorf("Applied = %d, want 1 — the count must say how far it got", res.Applied)
	}
	var reached []string
	for _, l := range applyLines(rec) {
		reached = append(reached, strings.Fields(l)[1])
	}
	if !equalTrace(reached, []string{"2", "3"}) {
		t.Errorf("reached %v, want [2 3]: node 4 must not have run", reached)
	}
}

// TestAFailedApplyLeavesTheSourceUncommittedSoARetryWorks — rows 15, 20, 21.
//
// This is the contract that would otherwise make a failure permanent in
// silence: if the source committed, retrying the same value would take the
// "unchanged, do nothing" path and the failed setter would never be called
// again, no matter what the binding cache held.
func TestAFailedApplyLeavesTheSourceUncommittedSoARetryWorks(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec,
		`Flex { Text { id: a text: x } Text { id: b text: x } }`,
		map[string]string{"x": "0"}, nil)

	boom := errors.New("this setter refuses")
	fail := true
	rec.onApply = func(a decl.Application) {
		if a.Node == 3 && fail {
			rec.applyErr["text"] = boom
		} else {
			rec.applyErr["text"] = nil
		}
	}

	res, err := tr.SetSource("x", sv("1"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the setter's error", err)
	}
	if res.Recomputed != 2 || res.Applied != 1 {
		t.Errorf("counts must be pinned at the failure: %+v, want 2 recomputed / 1 applied", res)
	}
	// The source did NOT commit.
	if got, _ := tr.Source("x"); got.Raw != "0" {
		t.Errorf("source committed to %q despite the failure", got.Raw)
	}

	// The retry: same value, which is a CHANGED-source attempt because the
	// source is still old. Node 2 already has it and goes quiet; node 3 is
	// called again.
	fail = false
	rec.trace = nil
	res, err = tr.SetSource("x", sv("1"))
	if err != nil {
		t.Fatalf("the retry failed: %v", err)
	}
	if res.Quiet != 1 || res.Applied != 1 {
		t.Errorf("res = %+v, want 1 quiet (the earlier success) / 1 applied (the retry)", res)
	}
	var reached []string
	for _, l := range applyLines(rec) {
		reached = append(reached, strings.Fields(l)[1])
	}
	if !equalTrace(reached, []string{"3"}) {
		t.Errorf("the retry reached %v, want only [3]", reached)
	}
	if got, _ := tr.Source("x"); got.Raw != "1" {
		t.Errorf("source = %q after a successful fan-out, want 1", got.Raw)
	}
	// And the tree was never latched.
	if _, err := tr.Reconcile(mustSpec(t,
		`Flex { Text { id: a text: x } Text { id: b text: x } }`)); err != nil {
		t.Fatalf("a propagation failure latched the tree: %v", err)
	}
}

// ------------------------------------------------------------------- phases

// TestThePropagationPhaseRefusesEveryMutatingEntry — row 26.
//
// Every arm of the matrix individually, including Mount and Reload, so a
// missing switch arm cannot survive. And the POSITIVE direction, because a cell
// that only asserts refusals would pass against an engine that refused the
// legal case too.
func TestThePropagationPhaseRefusesEveryMutatingEntry(t *testing.T) {
	rec := newReactor()
	var tr *decl.Tree
	got := map[string]error{}
	// The attempts run INSIDE the value function, which is the only moment the
	// propagation phase is actually in force. An earlier version of this test
	// captured closures here and ran them after SetSource returned — by which
	// time the phase was idle again and every one of them was legal. It
	// reported six failures against correct code, which is the good direction
	// for that mistake to fail in.
	fn := func(a []parse.SpecValue) (parse.SpecValue, error) {
		if tr == nil {
			// The first call happens during Mount, before tree() has returned.
			return sv(a[0].Raw), nil
		}
		got["SetSource"] = func() error { _, e := tr.SetSource("x", sv("z")); return e }()
		got["SetProp"] = tr.SetProp(2, "hint", sv("v"))
		got["Emit"] = tr.Emit(2, "clicked")
		got["Mount"] = tr.Mount(mustSpec(t, `Flex {}`))
		got["Reload"] = func() error { _, e := tr.Reload([]byte(`Flex {}`)); return e }()
		got["Reconcile"] = func() error { _, e := tr.Reconcile(mustSpec(t, `Flex {}`)); return e }()
		got["Destroy"] = tr.Destroy()
		return sv(a[0].Raw), nil
	}
	tr = tree(t, rec, `Flex { Text { id: a text: pass(x) } }`,
		map[string]string{"x": "1"}, map[string]decl.ValueFunc{"pass": fn})

	if _, err := tr.SetSource("x", sv("2")); err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("the value function ran %d attempts; this test proves nothing", len(got))
	}
	for name, err := range got {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(err, decl.ErrPhase) {
				t.Errorf("%s during propagation returned %v, want ErrPhase", name, err)
			}
		})
	}
	// The tree survived every refused attempt, which is what "before effects"
	// means: a refusal that had already half-run would show up here.
	if tr.Len() != 2 {
		t.Errorf("Len = %d, want 2: a refused entry mutated the tree anyway", tr.Len())
	}

	// Read-only queries observe; they do not mutate.
	t.Run("read-only queries stay legal", func(t *testing.T) {
		var n int
		var ok bool
		rec2 := newReactor()
		tr2 := decl.New(rec2)
		_ = tr2.DeclareSource("x", sv("1"))
		_ = tr2.DeclareFunc("pass", func(a []parse.SpecValue) (parse.SpecValue, error) {
			n = tr2.Len()
			_, ok = tr2.TypeOf(tr2.Root())
			return sv(a[0].Raw + "!"), nil
		})
		if err := tr2.Mount(mustSpec(t, `Flex { Text { id: a text: pass(x) } }`)); err != nil {
			t.Fatal(err)
		}
		if _, err := tr2.SetSource("x", sv("2")); err != nil {
			t.Fatal(err)
		}
		if n == 0 || !ok {
			t.Errorf("a read-only query was refused during propagation: Len=%d TypeOf ok=%v", n, ok)
		}
	})
}

// TestSetSourceFromASignalHandlerIsLegal — row 26, the positive direction.
//
// This is the case the whole feature exists for: a button's handler updates a
// source and the screen follows. The enclosing emission's phase and active
// stack must survive it, or the outer signal's cycle detection would be altered
// by whatever the propagation did.
func TestSetSourceFromASignalHandlerIsLegal(t *testing.T) {
	rec := newReactor()
	tr := decl.New(rec)
	if err := tr.DeclareSource("x", sv("before")); err != nil {
		t.Fatal(err)
	}
	var propagated decl.PropagationResult
	var perr error
	rec.handlers["bump"] = func() error {
		propagated, perr = tr.SetSource("x", sv("after"))
		return perr
	}
	if err := tr.Mount(wiredSpec(t, tr, rec.recorder,
		`Flex { Button { id: b onClicked: bump() } Text { id: a text: x } }`)); err != nil {
		t.Fatal(err)
	}
	btn := tr.Children(tr.Root())[0]
	rec.trace = nil

	if err := tr.Emit(btn, "clicked"); err != nil {
		t.Fatalf("a handler updating a source was refused: %v", err)
	}
	if perr != nil {
		t.Fatalf("SetSource from a handler failed: %v", perr)
	}
	if propagated.Applied != 1 {
		t.Errorf("propagation = %+v, want 1 applied", propagated)
	}
	// The emission survived: the same signal can run again, which it could not
	// if the active stack had been left behind.
	if err := tr.Emit(btn, "clicked"); err != nil {
		t.Errorf("the emission's active stack was not restored: %v", err)
	}
}

// TestABoundPropertyHasOneWriter — row 11.
func TestABoundPropertyHasOneWriter(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, `Flex { Text { id: a text: x hint: "free" } }`,
		map[string]string{"x": "1"}, nil)
	text := tr.Children(tr.Root())[0]

	if err := tr.SetProp(text, "text", sv("host wins?")); !errors.Is(err, decl.ErrPhase) {
		t.Errorf("SetProp on a bound property returned %v, want a refusal", err)
	}
	// An unbound property on the same node is unaffected: the rule is about the
	// property, not the node.
	if err := tr.SetProp(text, "hint", sv("ok")); err != nil {
		t.Errorf("SetProp on an UNBOUND property was refused: %v", err)
	}
}

// --------------------------------------------------------------- reconcile

// TestAnIdenticalReloadInvokesNoValueFunction — row 5, and the implementation
// defect that hid behind it.
//
// The count that matters is the HOST FUNCTION's, not the adapter's. An earlier
// version re-evaluated every binding on every reload and still reported zero
// applications, because the apply is skipped when the declaration is unchanged.
// "An unchanged file changes nothing" was true of what reached the adapter and
// false of what the host was asked to compute.
func TestAnIdenticalReloadInvokesNoValueFunction(t *testing.T) {
	rec := newReactor()
	calls := 0
	const src = `Flex { Text { id: a text: f(x) } Text { id: b text: f(x) } }`
	tr := tree(t, rec, src, map[string]string{"x": "1"},
		map[string]decl.ValueFunc{"f": func(a []parse.SpecValue) (parse.SpecValue, error) {
			calls++
			return sv("d" + a[0].Raw), nil
		}})

	calls = 0
	res := reconcile(t, tr, src)
	if calls != 0 {
		t.Errorf("an identical reload invoked the value function %d time(s), want 0", calls)
	}
	if res.Applied != 0 || len(rec.trace) != 0 {
		t.Errorf("an identical reload touched the adapter: %+v %v", res, rec.trace)
	}

	// A sibling-only change leaves the survivor's function alone too.
	calls = 0
	reconcile(t, tr, `Flex { Text { id: a text: f(x) } Text { id: b text: "lit" } }`)
	if calls != 0 {
		t.Errorf("a sibling-only change re-evaluated the untouched binding %d time(s)", calls)
	}

	// And the surviving binding kept its cache: a source tick that derives the
	// same value is still quiet.
	calls = 0
	r2, err := tr.SetSource("x", sv("1"))
	if err != nil {
		t.Fatal(err)
	}
	if r2 != (decl.PropagationResult{}) {
		t.Errorf("the registration or cache was lost across the reload: %+v", r2)
	}
}

// TestAChangedBindingEvaluatesExactlyOnce — the third implementation check.
func TestAChangedBindingEvaluatesExactlyOnce(t *testing.T) {
	rec := newReactor()
	calls := 0
	tr := tree(t, rec, `Flex { Text { id: a text: "lit" } }`,
		map[string]string{"x": "1"},
		map[string]decl.ValueFunc{"f": func(a []parse.SpecValue) (parse.SpecValue, error) {
			calls++
			return sv("d" + a[0].Raw), nil
		}})

	calls = 0
	reconcile(t, tr, `Flex { Text { id: a text: f(x) } Text { id: b text: f(x) } }`)
	if calls != 2 {
		t.Errorf("one changed and one fresh binding evaluated %d times, want 2", calls)
	}
}

// TestAFailedEvaluationDuringReconcileLeavesTheTreeUntouched — row 16.
func TestAFailedEvaluationDuringReconcileLeavesTheTreeUntouched(t *testing.T) {
	rec := newReactor()
	boom := errors.New("the host function refuses")
	tr := tree(t, rec, `Flex { Text { id: a text: "one" } }`,
		map[string]string{"x": "1"},
		map[string]decl.ValueFunc{"f": func([]parse.SpecValue) (parse.SpecValue, error) {
			return parse.SpecValue{}, boom
		}})
	before := tr.Children(tr.Root())

	_, err := tr.Reconcile(mustSpec(t, `Flex { Text { id: a text: f(x) } }`))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the function's error", err)
	}
	for _, l := range rec.trace {
		if strings.HasPrefix(l, "apply") || strings.HasPrefix(l, "create") ||
			strings.HasPrefix(l, "destroy") {
			t.Errorf("the tree was mutated: %q", l)
		}
	}
	if got := tr.Children(tr.Root()); !equalIDs(got, before) {
		t.Errorf("the tree changed: %v -> %v", before, got)
	}
	if _, err := tr.Reconcile(mustSpec(t, `Flex { Text { id: a text: "two" } }`)); err != nil {
		t.Fatalf("the tree was latched by a pure evaluation failure: %v", err)
	}
}

// TestAHostSourceOutlivesEverySchemaReference — row 22.
//
// Sources are host-declared CAPABILITIES, not schema artefacts. A schema that
// stops referring to one must not revoke it, or a reload that removes a binding
// and a later one that restores it would need the host to redeclare in between.
func TestAHostSourceOutlivesEverySchemaReference(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, `Flex { Text { id: a text: x } }`, map[string]string{"x": "1"}, nil)

	reconcile(t, tr, `Flex { Text { id: a text: "literal now" } }`)
	if _, ok := tr.Source("x"); !ok {
		t.Fatal("the source was removed when the schema stopped referring to it")
	}
	if _, err := tr.SetSource("x", sv("still settable")); err != nil {
		t.Errorf("the source became unsettable: %v", err)
	}
	// And re-adding the binding works with no redeclaration.
	res := reconcile(t, tr, `Flex { Text { id: a text: x } }`)
	if res.Applied != 1 {
		t.Errorf("re-adding the binding applied %d, want 1", res.Applied)
	}
	r2, err := tr.SetSource("x", sv("reconnected"))
	if err != nil || r2.Applied != 1 {
		t.Errorf("the restored binding does not track: %+v err=%v", r2, err)
	}
}

// ---------------------------------------------------------- the 0001c rules

// TestABareIdentifierIsNeverABinding — 0001c rows 1 and 3.
//
// This is the regression that nine shipped cells caught: a bare word is the
// adapter's vocabulary, and `orientation: Tui.Horizontal` must keep meaning what it
// always did — even when a source happens to be called "horizontal".
func TestABareIdentifierIsNeverABinding(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, `import tui 1.0
Flex { direction: Tui.Vertical Text { id: a text: "x" } }`, nil, nil)
	if tr.Len() == 0 {
		t.Fatal("a schema of bare words did not mount")
	}

	// Now with a source of the same spelling declared: nothing changes.
	rec2 := newReactor()
	tr2 := decl.New(rec2)
	if err := tr2.DeclareSource("vertical", sv("SHADOW")); err != nil {
		t.Fatal(err)
	}
	if err := tr2.Mount(mustSpec(t, `import tui 1.0
Flex { direction: Tui.Vertical Text { id: a text: "x" } }`)); err != nil {
		t.Fatalf("a source shadowed an adapter identifier: %v", err)
	}
	for _, l := range rec2.trace {
		if strings.Contains(l, "SHADOW") {
			t.Errorf("the source's value reached the adapter: %q", l)
		}
	}
	// And the source is untouched by the schema that spells the same word.
	if v, _ := tr2.Source("vertical"); v.Raw != "SHADOW" {
		t.Errorf("source = %q", v.Raw)
	}
}

// TestSourcesNestInsideCalls — 0001c row 6.
func TestSourcesNestInsideCalls(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, `Flex { Text { id: a text: outer(inner(x), "lit") } }`,
		map[string]string{"x": "1"},
		map[string]decl.ValueFunc{
			"inner": func(a []parse.SpecValue) (parse.SpecValue, error) { return sv("[" + a[0].Raw + "]"), nil },
			"outer": func(a []parse.SpecValue) (parse.SpecValue, error) { return sv(a[0].Raw + a[1].Raw), nil },
		})
	res, err := tr.SetSource("x", sv("2"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 1 {
		t.Fatalf("a nested source did not propagate: %+v", res)
	}
	want := []string{"apply 2 text=string([2]lit) from-binding"}
	if !equalTrace(applyLines(rec), want) {
		t.Errorf("trace\n got %v\nwant %v", applyLines(rec), want)
	}
}

// TestBindingsRequireAClassifier — row 23.
//
// Both halves on the SAME adapter, because the refusal and the preserved
// fallback are two claims and one cell proves only one.
func TestBindingsRequireAClassifier(t *testing.T) {
	plain := newSplicer() // implements decl.Adapter and nothing more
	if _, ok := any(plain).(decl.Classifier); ok {
		t.Fatal("fixture is wrong: the plain splicer must not classify")
	}

	tr := decl.New(plain)
	if err := tr.DeclareSource("x", sv("1")); err != nil {
		t.Fatal(err)
	}
	err := tr.Mount(mustSpec(t, `Flex { Text { id: a text: x } }`))
	if !errors.Is(err, decl.ErrBindingUnsupported) {
		t.Fatalf("err = %v, want ErrBindingUnsupported", err)
	}
	if tr.Len() != 0 {
		t.Errorf("%d nodes left behind", tr.Len())
	}

	// The literal-only fallback is INTACT on that same adapter.
	if err := tr.Mount(mustSpec(t, `Flex { Text { id: a text: "literal" } }`)); err != nil {
		t.Fatalf("the literal-only path was broken: %v", err)
	}
}

// TestABindingOnAConstructorOnlyPropertyIsRefused — row 10.
func TestABindingOnAConstructorOnlyPropertyIsRefused(t *testing.T) {
	rec := newReactor()
	tr := decl.New(rec)
	if err := tr.DeclareSource("dir", sv("vertical")); err != nil {
		t.Fatal(err)
	}
	err := tr.Mount(mustSpec(t, `Flex { direction: dir Text { id: a text: "x" } }`))
	if err == nil {
		t.Fatal("a binding on a constructor-only property was accepted")
	}
	if !strings.Contains(err.Error(), "rebuilding the node") {
		t.Errorf("the reason does not say what binding it would cost: %v", err)
	}
	if tr.Len() != 0 {
		t.Errorf("%d nodes left behind", tr.Len())
	}
}

// TestDuplicateDeclarationsInvolvingABinding — row 24.
func TestDuplicateDeclarationsInvolvingABinding(t *testing.T) {
	for name, src := range map[string]string{
		"binding then literal": `Flex { Text { id: a text: x text: "fixed" } }`,
		"literal then binding": `Flex { Text { id: a text: "fixed" text: x } }`,
		"binding then binding": `Flex { Text { id: a text: x text: y } }`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := newReactor()
			tr := decl.New(rec)
			_ = tr.DeclareSource("x", sv("1"))
			_ = tr.DeclareSource("y", sv("2"))
			err := tr.Mount(mustSpec(t, src))
			if !errors.Is(err, decl.ErrDuplicateBinding) {
				t.Fatalf("err = %v, want ErrDuplicateBinding", err)
			}
			if tr.Len() != 0 {
				t.Errorf("%d nodes left behind", tr.Len())
			}
		})
	}
	t.Run("duplicate LITERALS still mount, last wins", func(t *testing.T) {
		// P2 pinned this deliberately; the binding rule must not disturb it.
		rec := newReactor()
		tr := tree(t, rec, `Flex { Text { id: a text: "first" text: "second" } }`, nil, nil)
		if tr.Len() == 0 {
			t.Fatal("duplicate literals were refused")
		}
	})
}

// TestACreateConsumedBindingInitialisesItsCache — row 25.
//
// A builder that claims a bound property leaves no Apply to succeed, so the
// terminal handed to Construction is what seeds the cache. The tick below
// CHANGES the source to a value that derives to the same result — the only
// shape that reads the cache. An earlier version ticked the source to the value
// it already had, which short-circuits on the SOURCE comparison and never
// consults the cache at all: it passed with the seeding removed.
func TestACreateConsumedBindingInitialisesItsCache(t *testing.T) {
	rec := newReactor()
	rec.consume["Text"] = []string{"text"} // the builder claims it at construction
	tr := decl.New(rec)
	if err := tr.DeclareSource("x", sv("first")); err != nil {
		t.Fatal(err)
	}
	if err := tr.DeclareFunc("same", func([]parse.SpecValue) (parse.SpecValue, error) {
		return sv("always"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(mustSpec(t, `Flex { Text { id: a text: same(x) } }`)); err != nil {
		t.Fatal(err)
	}
	rec.trace = nil

	// A DIFFERENT source value deriving to the same result: quiet only if the
	// cache was seeded at construction.
	res, err := tr.SetSource("x", sv("second"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Recomputed != 1 {
		t.Fatalf("res = %+v, want the binding recomputed", res)
	}
	if res.Quiet != 1 || res.Applied != 0 {
		t.Errorf("res = %+v, want 1 quiet / 0 applied — the cache was not seeded by Create", res)
	}
	if got := applyLines(rec); len(got) != 0 {
		t.Errorf("a value the widget already had reached the setter: %v", got)
	}
}

// TestDestroyDropsSourcesAndBindings — ADR-decl-0001b R2's lifetime row.
//
// Destroy is the lifetime boundary for both. The alternative is a half-reset
// engine: DeclareSource is refused after Mount, so a tree re-mounted with its
// sources carried over could never have that set corrected.
func TestDestroyDropsSourcesAndBindings(t *testing.T) {
	rec := newReactor()
	tr := tree(t, rec, `Flex { Text { id: a text: x } }`, map[string]string{"x": "1"}, nil)

	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, ok := tr.Source("x"); ok {
		t.Error("a source outlived Destroy")
	}
	if _, err := tr.SetSource("x", sv("2")); !errors.Is(err, decl.ErrNoSuchSource) {
		t.Errorf("SetSource after Destroy = %v, want ErrNoSuchSource", err)
	}
	// And the set can be declared again, which is the point of dropping it.
	if err := tr.DeclareSource("x", sv("fresh")); err != nil {
		t.Fatalf("redeclaration after Destroy was refused: %v", err)
	}
	if err := tr.Mount(mustSpec(t, `Flex { Text { id: a text: x } }`)); err != nil {
		t.Fatalf("re-mount after Destroy: %v", err)
	}
	r, err := tr.SetSource("x", sv("tracks"))
	if err != nil || r.Applied != 1 {
		t.Errorf("the re-mounted binding does not track: %+v err=%v", r, err)
	}
}
