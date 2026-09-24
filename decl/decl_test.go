package decl_test

import (
	"errors"
	"fmt"
	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
	"sort"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// recorder is a test adapter that writes down every call in order. The trace it
// produces IS the assertion for most of this file, because the contracts being
// held are about ORDER and about which calls happen at all.
type recorder struct {
	trace []string

	createErr  map[string]error
	applyErr   map[string]error
	resolveErr map[string]error
	destroyErr error

	handlers map[string]func() error
	consume  map[string][]string
	// onApply runs inside Apply, which is the only place a test can reach in
	// while the engine is mid-walk.
	onApply  func(decl.Application)
	emitters map[decl.NodeID]map[string]func() error
}

func newRecorder() *recorder {
	return &recorder{
		createErr:  map[string]error{},
		applyErr:   map[string]error{},
		resolveErr: map[string]error{},
		handlers:   map[string]func() error{},
		consume:    map[string][]string{},
		emitters:   map[decl.NodeID]map[string]func() error{},
	}
}

// consume names the properties this fake claims at construction, per type.
func (r *recorder) Create(c decl.Construction) ([]string, error) {
	var sig []string
	for s := range c.Emitters {
		sig = append(sig, s)
	}
	sort.Strings(sig)
	r.trace = append(r.trace, fmt.Sprintf("create %d %s children=%v signals=%v",
		c.Node, c.Type, c.Children, sig))
	if err := r.createErr[c.Type]; err != nil {
		return nil, err
	}
	r.emitters[c.Node] = c.Emitters
	return r.consume[c.Type], nil
}

func (r *recorder) Apply(a decl.Application) error {
	r.trace = append(r.trace, fmt.Sprintf("apply %d %s=%s(%s) from-%s",
		a.Node, a.Prop, a.Value.Kind, a.Value.Raw, a.Origin))
	if r.onApply != nil {
		r.onApply(a)
	}
	return r.applyErr[a.Prop]
}

// injectHandlers gives tr one handler per name the schema CALLS, so these
// fixtures keep their old convenience — every handler name resolves unless the
// test says otherwise — under the engine's rule that a name must be injected to
// be reachable at all.
//
// It replaces the fake's ResolveHandler. Handler resolution is no longer the
// adapter's business: a host injects its effects, and the engine resolves them
// through the one registry every other name goes through.
func injectHandlers(t *testing.T, tr *decl.Tree, r *recorder, spec qml.SpecTree) {
	t.Helper()
	done := map[string]bool{}
	give := func(name string) {
		if done[name] || r.resolveErr[name] != nil {
			return
		}
		done[name] = true
		fn, ok := r.handlers[name]
		if !ok {
			fn = func() error { r.trace = append(r.trace, "run "+name); return nil }
		}
		if err := tr.Inject(name, decl.Handle(func([]qml.SpecValue) error { return fn() })); err != nil {
			t.Fatalf("inject handler %q: %v", name, err)
		}
	}
	// The fake's whole table, not only what this schema calls: injection is
	// fixed before Mount, so a RELOAD naming a handler the first schema did not
	// must still find it. That is the rule, not a workaround for it — the host's
	// capability set does not grow because a file changed.
	names := make([]string, 0, len(r.handlers))
	for n := range r.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		give(n)
	}

	var visit func(*qml.SpecNode)
	visit = func(n *qml.SpecNode) {
		if n == nil {
			return
		}
		for _, h := range n.Handlers {
			for i := range h.Body {
				h.Body[i].WalkExprs(func(e *js.Expr) bool {
					if e.Kind != js.ExprCall || e.Left == nil || e.Left.Kind != js.ExprIdent {
						return true
					}
					// A name the test marked unresolvable is deliberately NOT
					// injected: "the host did not hand this over" is what an
					// unresolvable handler now means.
					give(e.Left.Raw)
					return true
				})
			}
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(spec.Root)
}

// wiredSpec parses src and injects the handlers it calls, ready to Mount.
func wiredSpec(t *testing.T, tr *decl.Tree, r *recorder, src string) qml.SpecTree {
	t.Helper()
	spec := mustSpec(t, src)
	injectHandlers(t, tr, r, spec)
	return spec
}

func (r *recorder) Destroy(n decl.NodeID) error {
	r.trace = append(r.trace, fmt.Sprintf("destroy %d", n))
	return r.destroyErr
}

// Modules makes `tui` importable, which is what lets these fixtures write the
// `import tui 1.0` a QML document needs before naming anything inside it.
func (r *recorder) Modules() []decl.Module {
	return []decl.Module{{Name: "tui", Version: "1.0", Exports: []string{"Tui"}}}
}

// Constants gives the fake the qualified vocabulary a QML-faithful schema
// writes: `direction: Tui.Vertical` rather than a bare word or a quoted string.
func (r *recorder) Constants() map[string]qml.SpecValue {
	str := func(s string) qml.SpecValue {
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: s}
	}
	return map[string]qml.SpecValue{
		"Tui.Horizontal": str("horizontal"),
		"Tui.Vertical":   str("vertical"),
	}
}

func mustSpec(t *testing.T, src string) qml.SpecTree {
	t.Helper()
	tree, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("fixture schema does not parse: %v", err)
	}
	return tree
}

// ---------------------------------------------------------------- ordering

// TestMountOrderIsReadableOffTheFile pins the whole build sequence.
//
// Construction is POST-ORDER: children are built before the parent, because a
// constructor may REQUIRE them and offer no way to supply them later. Identity
// is still allocated in schema order, so a diagnostic's node numbers read the
// way the file does rather than backwards.
func TestMountOrderIsReadableOffTheFile(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	spec := wiredSpec(t, tr, r, `Column {
        id: root
        spacing: 2
        title: "hello"
        onReady: warm()
        Button { label: "a" }
        Button { label: "b" }
    }`)
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	want := []string{
		// No handler resolution appears in this trace at all. It used to: the
		// engine asked the adapter to turn "warm" into a function. Handlers are
		// now compiled by the ENGINE from the injected registry, so the adapter
		// learns which signals a node has from its Construction — which is why
		// the create line below still carries signals=[ready].
		"create 2 Button children=[] signals=[]",
		"apply 2 label=string(a) from-schema",
		"create 3 Button children=[] signals=[]",
		"apply 3 label=string(b) from-schema",
		// The parent is built LAST, with its children in hand.
		"create 1 Column children=[2 3] signals=[ready]",
		"apply 1 spacing=number(2) from-schema",
		"apply 1 title=string(hello) from-schema",
	}
	if strings.Join(r.trace, "\n") != strings.Join(want, "\n") {
		t.Errorf("trace:\n%s\n\nwant:\n%s", strings.Join(r.trace, "\n"), strings.Join(want, "\n"))
	}
}

// TestChildrenAreBuiltBeforeTheirParent is the reason construction is
// post-order, stated as its own claim: a real container may take required
// children positionally and expose no method to add them afterwards.
func TestChildrenAreBuiltBeforeTheirParent(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	if err := tr.Mount(mustSpec(t, `Split { Left { } Right { } }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	var order []string
	for _, line := range r.trace {
		if strings.HasPrefix(line, "create ") {
			order = append(order, strings.Fields(line)[2])
		}
	}
	if strings.Join(order, ",") != "Left,Right,Split" {
		t.Errorf("creation order = %v, want children before the parent", order)
	}
	// And the parent received them, rather than being expected to collect them.
	for _, line := range r.trace {
		if strings.HasPrefix(line, "create 1 Split") && !strings.Contains(line, "children=[2 3]") {
			t.Errorf("the parent was built without its children: %q", line)
		}
	}
}

// TestConsumedPropertiesAreNotReApplied. A property claimed at construction is
// NOT streamed again.
//
// The tempting shortcut is to re-apply everything and rely on setters being
// idempotent. They are not, uniformly: some assign and invalidate
// unconditionally, and some constructor-only properties have no setter at all.
// So a replay is either a second visible effect or impossible, and the engine
// has to respect what Create says it took.
func TestConsumedPropertiesAreNotReApplied(t *testing.T) {
	r := newRecorder()
	r.consume["Split"] = []string{"orientation"}
	tr := decl.New(r)
	if err := tr.Mount(mustSpec(t, `import tui 1.0
Split { orientation: Tui.Vertical gap: 2 }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	for _, line := range r.trace {
		if strings.Contains(line, "orientation=") {
			t.Errorf("a constructor-consumed property was applied again: %q", line)
		}
	}
	var sawGap bool
	for _, line := range r.trace {
		if strings.Contains(line, "gap=") {
			sawGap = true
		}
	}
	if !sawGap {
		t.Error("an unconsumed property was not applied")
	}
}

// TestOneEmitterPerSignalNotPerHandler. Three handlers on one signal share ONE
// emitter, so a single widget event runs the list once. Wiring per handler
// would run it three times, and each pass would look correct in isolation.
func TestOneEmitterPerSignalNotPerHandler(t *testing.T) {
	r := newRecorder()
	var runs int
	for _, n := range []string{"a", "b", "c"} {
		r.handlers[n] = func() error { runs++; return nil }
	}
	tr := decl.New(r)
	if err := tr.Mount(wiredSpec(t, tr, r, "B {\n onGo: a()\n onGo: b()\n onGo: c()\n onStop: a()\n}")); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	em := r.emitters[tr.Root()]
	if len(em) != 2 {
		t.Fatalf("emitters = %d (%v), want one per distinct signal: go, stop", len(em), em)
	}
	if err := em["go"](); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if runs != 3 {
		t.Errorf("one event ran %d handlers, want 3 — exactly once each", runs)
	}
}

// TestEmitterRunsUnderTheEngineRules: the emitter handed to the adapter is not
// a shortcut around the contract. It is the contract's own entry point, so a
// cycle through it is still refused.
func TestEmitterRunsUnderTheEngineRules(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	var inner error
	var passes int
	r.handlers["again"] = func() error {
		passes++
		inner = r.emitters[tr.Root()]["go"]()
		return nil
	}
	if err := tr.Mount(wiredSpec(t, tr, r, `B { onGo: again() }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := r.emitters[tr.Root()]["go"](); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if passes != 1 {
		t.Errorf("handler ran %d times through the adapter's emitter; the rules were bypassed", passes)
	}
	if !errors.Is(inner, decl.ErrSignalCycle) {
		t.Errorf("re-entry through the emitter = %v, want ErrSignalCycle", inner)
	}
}

// TestIDIsRecordedButNotApplied: `id` addresses a node; it does not configure a
// widget. An adapter that received it as a property would have to know to
// ignore it, which is knowledge in the wrong place.
func TestIDIsRecordedButNotApplied(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	if err := tr.Mount(mustSpec(t, `Column { id: root spacing: 1 }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	for _, line := range r.trace {
		if strings.Contains(line, "id=") {
			t.Errorf("id reached the adapter as a property: %q", line)
		}
	}
	got, ok := tr.SchemaID(tr.Root())
	if !ok || got != "root" {
		t.Errorf("SchemaID = %q, %v; want \"root\", true", got, ok)
	}
}

// TestNodeIDsAreNeverReused: a callback that outlived its node must not be able
// to name a different one. Mount, destroy, mount again — the second tree's IDs
// must not collide with the first's.
func TestNodeIDsAreNeverReused(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	if err := tr.Mount(mustSpec(t, `A { B { } }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	firstRoot := tr.Root()
	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if err := tr.Mount(mustSpec(t, `A { B { } }`)); err != nil {
		t.Fatalf("second Mount: %v", err)
	}
	if tr.Root() == firstRoot {
		t.Errorf("second mount reused NodeID %d; IDs must never be reused", firstRoot)
	}
}

// TestDestroyReleasesChildrenFirst: a parent released before its child would
// hand the adapter a child whose parent is already gone.
func TestDestroyReleasesChildrenFirst(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	if err := tr.Mount(mustSpec(t, `A { B { C { } } }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	r.trace = nil
	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	want := []string{"destroy 3", "destroy 2", "destroy 1"}
	if strings.Join(r.trace, ",") != strings.Join(want, ",") {
		t.Errorf("destroy order = %v, want %v (deepest first)", r.trace, want)
	}
}

// ---------------------------------------------------------------- the seam

// TestApplicationCarriesTheValueNotAnInstruction is the portability claim in
// test form. What crosses the seam is a value the adapter can set; if it were a
// "repaint" notification the adapter would have to decide what the engine meant,
// and a second toolkit could not reuse any of it.
func TestApplicationCarriesTheValueNotAnInstruction(t *testing.T) {
	var got decl.Application
	r := newRecorder()
	seen := false
	tr := decl.New(&capturingAdapter{recorder: r, onApply: func(a decl.Application) { got, seen = a, true }})
	if err := tr.Mount(mustSpec(t, `Text { text: "hi" }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if !seen {
		t.Fatal("no Application reached the adapter")
	}
	if got.Prop != "text" || got.Value.Kind != qml.SpecValueString || got.Value.Raw != "hi" {
		t.Errorf("Application = %+v, want text/string/hi", got)
	}
	if got.Origin != decl.FromSchema {
		t.Errorf("Origin = %v, want schema", got.Origin)
	}
	// The position survives the crossing, so an adapter that rejects the value
	// can say where it was written without the engine remembering for it.
	if got.Value.Pos.Line == 0 {
		t.Error("Application lost the schema Position")
	}
}

type capturingAdapter struct {
	*recorder
	onApply func(decl.Application)
}

func (c *capturingAdapter) Apply(a decl.Application) error {
	c.onApply(a)
	return c.recorder.Apply(a)
}

// TestHostSetIsDistinguishableFromSchema: provenance exists so that when a
// value is wrong, "who set it" is answerable.
func TestHostSetIsDistinguishableFromSchema(t *testing.T) {
	var origins []decl.Provenance
	r := newRecorder()
	tr := decl.New(&capturingAdapter{recorder: r,
		onApply: func(a decl.Application) { origins = append(origins, a.Origin) }})
	if err := tr.Mount(mustSpec(t, `Text { text: "a" }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.SetProp(tr.Root(), "text", qml.SpecValue{Kind: qml.SpecValueString, Raw: "b"}); err != nil {
		t.Fatalf("SetProp: %v", err)
	}
	if len(origins) != 2 || origins[0] != decl.FromSchema || origins[1] != decl.FromHost {
		t.Errorf("origins = %v, want [schema host]", origins)
	}
}

func TestSetPropOnAnUnknownNode(t *testing.T) {
	tr := decl.New(newRecorder())
	err := tr.SetProp(decl.NodeID(99), "x", qml.SpecValue{})
	if !errors.Is(err, decl.ErrNoSuchNode) {
		t.Errorf("err = %v, want ErrNoSuchNode", err)
	}
}

// ---------------------------------------------------------------- signals

func TestEmitRunsHandlersInDocumentOrder(t *testing.T) {
	r := newRecorder()
	var ran []string
	for _, name := range []string{"first", "second", "third"} {
		r.handlers[name] = func() error { ran = append(ran, name); return nil }
	}
	tr := decl.New(r)
	if err := tr.Mount(wiredSpec(t, tr, r, "B {\n onGo: first()\n onGo: second()\n onGo: third()\n}")); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Emit(tr.Root(), "go"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if strings.Join(ran, ",") != "first,second,third" {
		t.Errorf("ran %v, want document order", ran)
	}
}

// TestEmitRefusesACycleImmediately: a depth cap alone would let a two-signal
// cycle run many times before stopping, doing real work each pass. Identity
// catches it on the FIRST re-entry.
func TestEmitRefusesACycleImmediately(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	var passes int
	var emitErr error
	r.handlers["again"] = func() error {
		passes++
		emitErr = tr.Emit(tr.Root(), "go")
		return nil
	}
	if err := tr.Mount(wiredSpec(t, tr, r, `B { onGo: again() }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Emit(tr.Root(), "go"); err != nil {
		t.Fatalf("outer Emit: %v", err)
	}
	if passes != 1 {
		t.Errorf("handler ran %d times; the cycle must be refused on first re-entry", passes)
	}
	if !errors.Is(emitErr, decl.ErrSignalCycle) {
		t.Errorf("re-entry error = %v, want ErrSignalCycle", emitErr)
	}
}

// TestEmitDepthCapCatchesALongAcyclicChain uses MORE DISTINCT NODES than the
// cap, so no (node, signal) pair ever repeats and the cycle detector cannot
// fire. That separation is the point: the two mechanisms answer different
// questions, and a test where either could fire proves neither.
func TestEmitDepthCapCatchesALongAcyclicChain(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r, decl.WithMaxEmitDepth(3))

	var errs []error
	var runs int
	// Each node hands off to the NEXT one, so the chain is strictly acyclic.
	r.handlers["chain"] = func() error {
		runs++
		next := decl.NodeID(runs + 1)
		if err := tr.Emit(next, "go"); err != nil {
			errs = append(errs, err)
		}
		return nil
	}
	if err := tr.Mount(wiredSpec(t, tr, r, `A {
        onGo: chain()
        B { onGo: chain() }
        C { onGo: chain() }
        D { onGo: chain() }
        E { onGo: chain() }
    }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	if err := tr.Emit(tr.Root(), "go"); err != nil {
		t.Fatalf("outer Emit: %v", err)
	}

	if len(errs) != 1 {
		t.Fatalf("collected %d refusals, want exactly 1: %v", len(errs), errs)
	}
	if !errors.Is(errs[0], decl.ErrEmitDepth) {
		t.Errorf("refusal = %v, want ErrEmitDepth", errs[0])
	}
	// No pair repeated, so the cycle detector must NOT be what stopped it.
	if errors.Is(errs[0], decl.ErrSignalCycle) {
		t.Error("an acyclic chain was refused as a cycle")
	}
	// The cap is 3, so the chain stops after the third live emission.
	if runs != 3 {
		t.Errorf("handler ran %d times with a cap of 3", runs)
	}
}

// TestHandlerErrorStopsTheEmissionAndCommitsWhatRan is the honest half of the
// contract. No rollback is attempted, and the test asserts the write SURVIVES —
// because an implementation that quietly tried to undo it would be pretending
// the adapter's setters are reversible.
func TestHandlerErrorStopsTheEmissionAndCommitsWhatRan(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	boom := errors.New("handler failed")
	var applied []string
	adapter := &capturingAdapter{recorder: r,
		onApply: func(a decl.Application) { applied = append(applied, a.Prop) }}
	tr = decl.New(adapter)
	r.handlers["writes"] = func() error {
		return tr.SetProp(tr.Root(), "written", qml.SpecValue{Kind: qml.SpecValueBool, Raw: "true"})
	}
	r.handlers["fails"] = func() error { return boom }
	r.handlers["never"] = func() error { applied = append(applied, "NEVER-RAN"); return nil }

	if err := tr.Mount(wiredSpec(t, tr, r, "B {\n onGo: writes()\n onGo: fails()\n onGo: never()\n}")); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	err := tr.Emit(tr.Root(), "go")
	if !errors.Is(err, boom) {
		t.Fatalf("Emit error = %v, want the handler's error", err)
	}
	joined := strings.Join(applied, ",")
	if !strings.Contains(joined, "written") {
		t.Error("the write made before the error was lost; there is no rollback and none should be faked")
	}
	if strings.Contains(joined, "NEVER-RAN") {
		t.Error("a handler after the failing one ran")
	}
}

func TestEmitOnAnUnboundSignalIsANoOp(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	if err := tr.Mount(mustSpec(t, `B { label: "x" }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Emit(tr.Root(), "nobodyListens"); err != nil {
		t.Errorf("emitting an unbound signal returned %v; it should be a no-op", err)
	}
}

// ---------------------------------------------------------------- phases

func TestMountIsRefusedDuringEmission(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	var inner error
	r.handlers["remount"] = func() error {
		inner = tr.Mount(mustSpec(t, `Other { }`))
		return nil
	}
	if err := tr.Mount(wiredSpec(t, tr, r, `B { onGo: remount() }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Emit(tr.Root(), "go"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !errors.Is(inner, decl.ErrPhase) {
		t.Errorf("mount during emission returned %v, want ErrPhase", inner)
	}
}

func TestMountTwiceIsRefused(t *testing.T) {
	tr := decl.New(newRecorder())
	if err := tr.Mount(mustSpec(t, `A { }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Mount(mustSpec(t, `A { }`)); !errors.Is(err, decl.ErrPhase) {
		t.Errorf("second Mount = %v, want ErrPhase", err)
	}
}

// ---------------------------------------------------------------- failures

// TestAdapterRefusalsAreTypedAndPositioned: a schema is INPUT. An unknown type
// or property is the author's mistake to see and fix, so the error has to carry
// the line, and it must never be a panic.
//
// An unresolvable HANDLER used to be a row here. It is not an adapter refusal
// any more — the adapter never sees handler names — so it moved to
// [TestAnUnresolvableHandlerIsTheEnginesRefusalAndKeepsItsPosition], which
// asserts the new sentinel rather than quietly accepting a different one.
func TestAdapterRefusalsAreTypedAndPositioned(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		setup func(*recorder)
		op    string
	}{
		{"unknown type", "Nope { }", func(r *recorder) {
			r.createErr["Nope"] = errors.New("no such widget")
		}, "create"},
		{"unknown property", "A {\n  bogus: 1\n}", func(r *recorder) {
			r.applyErr["bogus"] = errors.New("no such property")
		}, "apply"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRecorder()
			c.setup(r)
			tr := decl.New(r)
			err := tr.Mount(mustSpec(t, c.src))
			if err == nil {
				t.Fatal("Mount succeeded; the adapter refused and that must surface")
			}
			if !errors.Is(err, decl.ErrAdapter) {
				t.Errorf("err = %v, want it to wrap ErrAdapter", err)
			}
			var se decl.SchemaError
			if !errors.As(err, &se) {
				t.Fatalf("err %T, want decl.SchemaError", err)
			}
			if se.Op != c.op {
				t.Errorf("Op = %q, want %q", se.Op, c.op)
			}
			if se.Pos.Line == 0 {
				t.Error("the error lost the schema position; an author cannot find the line")
			}
		})
	}
}

func TestNilAdapterFailsAtConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New(nil) did not panic; a nil adapter is a programming mistake and should fail at the call site")
		}
	}()
	decl.New(nil)
}

// ---------------------------------------------------- what the review found

// hostileAdapter re-enters the Tree from a chosen callback. It exists because
// "can a callback corrupt the tree" has to be asked at EVERY point the adapter
// gets control, not at one convenient one: the first version of this engine
// survived a Destroy from the first Create — mounting simply continued and
// repopulated the map — and fell over when the same call came from a later
// callback. A single-point test would have reported that as safe.
type hostileAdapter struct {
	*recorder
	tr *decl.Tree
	at string // "create" | "apply" | "destroy"
	do func()
	// fired keeps the re-entry to once, so the test observes one intrusion
	// rather than a loop.
	fired bool
}

func (h *hostileAdapter) maybe(point string) {
	if h.at == point && !h.fired {
		h.fired = true
		h.do()
	}
}

func (h *hostileAdapter) Create(c decl.Construction) ([]string, error) {
	h.maybe("create")
	return h.recorder.Create(c)
}
func (h *hostileAdapter) Apply(a decl.Application) error {
	h.maybe("apply")
	return h.recorder.Apply(a)
}
func (h *hostileAdapter) Destroy(n decl.NodeID) error {
	h.maybe("destroy")
	return h.recorder.Destroy(n)
}

// TestTheTreeCannotBeCorruptedFromAnyAdapterCallback sweeps every point the
// adapter gets control and every re-entrant call it could make. The assertion
// is an INVARIANT rather than a specific error: whatever the engine decides to
// do, it must not end up claiming a root it does not have, or holding nodes
// under no root.
func TestTheTreeCannotBeCorruptedFromAnyAdapterCallback(t *testing.T) {
	// Handler resolution used to be a fourth point here. It is gone because the
	// adapter no longer resolves handlers at all — the host injects them and the
	// engine resolves them itself, so there is no callback left to re-enter from.
	points := []string{"create", "apply", "destroy"}
	intrusions := []string{"destroy", "mount", "setprop"}

	for _, point := range points {
		for _, intrusion := range intrusions {
			t.Run(point+"/"+intrusion, func(t *testing.T) {
				h := &hostileAdapter{recorder: newRecorder(), at: point}
				tr := decl.New(h)
				h.tr = tr
				h.do = func() {
					switch intrusion {
					case "destroy":
						_ = tr.Destroy()
					case "mount":
						_ = tr.Mount(mustSpec(t, `Intruder { }`))
					case "setprop":
						_ = tr.SetProp(1, "x", qml.SpecValue{Kind: qml.SpecValueBool, Raw: "true"})
					}
				}

				// Must not panic, whatever happens.
				err := tr.Mount(wiredSpec(t, tr, h.recorder, "A {\n label: \"x\"\n onGo: h()\n B { label: \"y\" }\n}"))
				_ = err

				if tr.Root() != decl.NoNode && tr.Len() == 0 {
					t.Errorf("tree claims root %d with no nodes — a torn-down tree still reporting a root",
						tr.Root())
				}
				if tr.Root() == decl.NoNode && tr.Len() > 0 {
					// Legal only while the failure is remembered, so the next
					// Mount is refused rather than grafting onto the wreckage.
					if err2 := tr.Mount(mustSpec(t, `Second { }`)); err2 == nil {
						t.Error("a second Mount was accepted onto a partial tree; the graphs would combine")
					}
				}
			})
		}
	}
}

// TestRemountIsRefusedAfterAFailedMount. The first guard keyed on "is there a
// root", and a failed mount never sets one — so the check was blind to exactly
// the state that needs it, and a second Mount grafted a second graph onto the
// wreckage of the first.
func TestRemountIsRefusedAfterAFailedMount(t *testing.T) {
	r := newRecorder()
	r.applyErr["bad"] = errors.New("no such property")
	tr := decl.New(r)

	if err := tr.Mount(mustSpec(t, `A { bad: 1 }`)); err == nil {
		t.Fatal("the fixture mount should have failed")
	}
	before := tr.Len()

	err := tr.Mount(mustSpec(t, `Z { }`))
	if !errors.Is(err, decl.ErrPhase) {
		t.Fatalf("second Mount = %v, want ErrPhase; two graphs must not combine", err)
	}
	if tr.Len() != before {
		t.Errorf("the refused mount still added nodes: %d -> %d", before, tr.Len())
	}

	// Destroy is the documented way out, and it must work on a partial tree.
	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy after a failed mount: %v", err)
	}
	if err := tr.Mount(mustSpec(t, `Z { }`)); err != nil {
		t.Errorf("Mount after Destroy = %v, want success", err)
	}
}

// TestCycleErrorNamesBothEmissions. A cycle involves two points in the file:
// where the signal was already running and where it tried to run again.
// Reporting one of them leaves the reader to find the other, which on a
// two-node cycle is the harder half.
func TestCycleErrorNamesBothEmissions(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)

	var ids []decl.NodeID
	var inner error
	// A -> B -> A: a genuine two-node cycle, not self re-entry.
	r.handlers["toB"] = func() error { return tr.Emit(ids[1], "go") }
	r.handlers["toA"] = func() error {
		inner = tr.Emit(ids[0], "go")
		return nil
	}
	if err := tr.Mount(wiredSpec(t, tr, r, "A {\n  onGo: toB()\n  B {\n    onGo: toA()\n  }\n}")); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	ids = []decl.NodeID{tr.Root(), tr.Children(tr.Root())[0]}

	if err := tr.Emit(ids[0], "go"); err != nil {
		t.Fatalf("outer Emit: %v", err)
	}
	if !errors.Is(inner, decl.ErrSignalCycle) {
		t.Fatalf("A->B->A gave %v, want ErrSignalCycle", inner)
	}

	msg := inner.Error()
	var se decl.SchemaError
	if !errors.As(inner, &se) {
		t.Fatalf("error %T, want decl.SchemaError", inner)
	}
	// Both ends must be identifiable: the line the emission started at and the
	// line it re-entered from.
	if !strings.Contains(msg, "started at") || !strings.Contains(msg, "re-entered from") {
		t.Errorf("cycle error names only one end: %q", msg)
	}
	if se.Pos.Line == 0 {
		t.Error("the cycle error carries no position at all")
	}
}

// TestAnUnresolvableHandlerIsTheEnginesRefusalAndKeepsItsPosition.
//
// The adapter is no longer asked to resolve handler names, so a name nothing
// was injected under is the ENGINE's refusal and carries the engine's sentinel.
// What must not change is the part an author depends on: the line.
func TestAnUnresolvableHandlerIsTheEnginesRefusalAndKeepsItsPosition(t *testing.T) {
	tr := decl.New(newRecorder())
	err := tr.Mount(mustSpec(t, "A {\n  onGo: missing()\n}"))
	if !errors.Is(err, decl.ErrNotInjected) {
		t.Fatalf("err = %v, want ErrNotInjected", err)
	}
	if errors.Is(err, decl.ErrAdapter) {
		t.Errorf("err = %v, want it NOT to blame the adapter, which never saw the name", err)
	}
	var se decl.SchemaError
	if !errors.As(err, &se) {
		t.Fatalf("err %T, want decl.SchemaError", err)
	}
	if se.Pos.Line != 2 {
		t.Errorf("Pos = %s, want line 2 — an author cannot find the handler without it", se.Pos)
	}
}
