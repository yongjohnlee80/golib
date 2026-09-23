package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
)

// recorder is a test adapter that writes down every call in order. The trace it
// produces IS the assertion for most of this file, because the contracts being
// held are about ORDER and about which calls happen at all.
type recorder struct {
	trace []string

	createErr  map[string]error
	applyErr   map[string]error
	resolveErr map[string]error
	attachErr  error
	destroyErr error

	handlers map[string]func() error
}

func newRecorder() *recorder {
	return &recorder{
		createErr:  map[string]error{},
		applyErr:   map[string]error{},
		resolveErr: map[string]error{},
		handlers:   map[string]func() error{},
	}
}

func (r *recorder) Create(n decl.NodeID, typeName string, _ parse.Position) error {
	r.trace = append(r.trace, fmt.Sprintf("create %d %s", n, typeName))
	return r.createErr[typeName]
}

func (r *recorder) Apply(a decl.Application) error {
	r.trace = append(r.trace, fmt.Sprintf("apply %d %s=%s(%s) from-%s",
		a.Node, a.Prop, a.Value.Kind, a.Value.Raw, a.Origin))
	return r.applyErr[a.Prop]
}

func (r *recorder) Attach(parent, child decl.NodeID) error {
	r.trace = append(r.trace, fmt.Sprintf("attach %d->%d", parent, child))
	return r.attachErr
}

func (r *recorder) ResolveHandler(n decl.NodeID, signal, name string, _ parse.Position) (func() error, error) {
	r.trace = append(r.trace, fmt.Sprintf("resolve %d %s->%s", n, signal, name))
	if err := r.resolveErr[name]; err != nil {
		return nil, err
	}
	if fn, ok := r.handlers[name]; ok {
		return fn, nil
	}
	return func() error { r.trace = append(r.trace, "run "+name); return nil }, nil
}

func (r *recorder) Destroy(n decl.NodeID) error {
	r.trace = append(r.trace, fmt.Sprintf("destroy %d", n))
	return r.destroyErr
}

func mustSpec(t *testing.T, src string) parse.SpecTree {
	t.Helper()
	tree, err := parse.QML{}.Parse([]byte(src))
	if err != nil {
		t.Fatalf("fixture schema does not parse: %v", err)
	}
	return tree
}

// ---------------------------------------------------------------- ordering

// TestMountOrderIsReadableOffTheFile is the contract that makes a declarative
// file mean anything: create, then properties in DOCUMENT order, then handlers,
// then each child in turn. A map anywhere in the implementation breaks it, and
// nothing else in this file would notice.
func TestMountOrderIsReadableOffTheFile(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	spec := mustSpec(t, `Column {
        id: root
        spacing: 2
        title: "hello"
        onReady: warm
        Button { label: "a" }
        Button { label: "b" }
    }`)
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	want := []string{
		"create 1 Column",
		"apply 1 spacing=number(2) from-schema",
		"apply 1 title=string(hello) from-schema",
		"resolve 1 ready->warm",
		"create 2 Button",
		"apply 2 label=string(a) from-schema",
		"attach 1->2",
		"create 3 Button",
		"apply 3 label=string(b) from-schema",
		"attach 1->3",
	}
	if strings.Join(r.trace, "\n") != strings.Join(want, "\n") {
		t.Errorf("trace:\n%s\n\nwant:\n%s", strings.Join(r.trace, "\n"), strings.Join(want, "\n"))
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
	if got.Prop != "text" || got.Value.Kind != parse.SpecValueString || got.Value.Raw != "hi" {
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
	if err := tr.SetProp(tr.Root(), "text", parse.SpecValue{Kind: parse.SpecValueString, Raw: "b"}); err != nil {
		t.Fatalf("SetProp: %v", err)
	}
	if len(origins) != 2 || origins[0] != decl.FromSchema || origins[1] != decl.FromHost {
		t.Errorf("origins = %v, want [schema host]", origins)
	}
}

func TestSetPropOnAnUnknownNode(t *testing.T) {
	tr := decl.New(newRecorder())
	err := tr.SetProp(decl.NodeID(99), "x", parse.SpecValue{})
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
	if err := tr.Mount(mustSpec(t, `B { onGo: first onGo: second onGo: third }`)); err != nil {
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
	if err := tr.Mount(mustSpec(t, `B { onGo: again }`)); err != nil {
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
	if err := tr.Mount(mustSpec(t, `A {
        onGo: chain
        B { onGo: chain }
        C { onGo: chain }
        D { onGo: chain }
        E { onGo: chain }
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
		return tr.SetProp(tr.Root(), "written", parse.SpecValue{Kind: parse.SpecValueBool, Raw: "true"})
	}
	r.handlers["fails"] = func() error { return boom }
	r.handlers["never"] = func() error { applied = append(applied, "NEVER-RAN"); return nil }

	if err := tr.Mount(mustSpec(t, `B { onGo: writes onGo: fails onGo: never }`)); err != nil {
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
	if err := tr.Mount(mustSpec(t, `B { onGo: remount }`)); err != nil {
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
		{"unresolvable handler", "A {\n  onGo: missing\n}", func(r *recorder) {
			r.resolveErr["missing"] = errors.New("no such function")
		}, "bind"},
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
