package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// splicer is a recorder that CAN restructure, mirroring a real container's
// child list so the indices the engine asks for can be checked against what a
// container would actually hold.
//
// fixed names the schema TYPES that refuse restructuring, which is how the
// library's own widgets differ: a Flex splices, and a Split took both children
// as constructor arguments and cannot.
type splicer struct {
	*recorder
	kids  map[decl.NodeID][]decl.NodeID
	types map[decl.NodeID]string
	fixed map[string]bool
	// insertErr fails the next InsertChild, to exercise a mid-apply failure.
	insertErr error
}

func newSplicer(fixed ...string) *splicer {
	s := &splicer{
		recorder: newRecorder(),
		kids:     map[decl.NodeID][]decl.NodeID{},
		types:    map[decl.NodeID]string{},
		fixed:    map[string]bool{},
	}
	for _, f := range fixed {
		s.fixed[f] = true
	}
	return s
}

func (s *splicer) Create(c decl.Construction) ([]string, error) {
	consumed, err := s.recorder.Create(c)
	if err != nil {
		return nil, err
	}
	s.kids[c.Node] = append([]decl.NodeID(nil), c.Children...)
	s.types[c.Node] = c.Type
	return consumed, nil
}

func (s *splicer) CanRestructure(n decl.NodeID) bool {
	typ, ok := s.types[n]
	return ok && !s.fixed[typ]
}

func (s *splicer) InsertChild(parent, child decl.NodeID, at int) error {
	if s.insertErr != nil {
		err := s.insertErr
		s.insertErr = nil
		s.trace = append(s.trace, fmt.Sprintf("insert-REFUSED %d into %d", child, parent))
		return err
	}
	k := s.kids[parent]
	if at < 0 || at > len(k) {
		return fmt.Errorf("insert index %d out of range for %d children", at, len(k))
	}
	out := append([]decl.NodeID{}, k[:at]...)
	out = append(out, child)
	s.kids[parent] = append(out, k[at:]...)
	s.trace = append(s.trace, fmt.Sprintf("insert %d into %d at %d", child, parent, at))
	return nil
}

func (s *splicer) RemoveChild(parent, child decl.NodeID) error {
	k := s.kids[parent]
	for i, id := range k {
		if id == child {
			s.kids[parent] = append(append([]decl.NodeID{}, k[:i]...), k[i+1:]...)
			s.trace = append(s.trace, fmt.Sprintf("remove %d from %d", child, parent))
			return nil
		}
	}
	// A real container is a silent no-op here. This one complains, because a
	// detach of something the parent does not hold means the engine lost track,
	// and silence is what would hide that.
	return fmt.Errorf("node %d is not a child of %d", child, parent)
}

func (s *splicer) MoveChild(parent, child decl.NodeID, to int) error {
	k := s.kids[parent]
	from := -1
	for i, id := range k {
		if id == child {
			from = i
			break
		}
	}
	if from < 0 {
		return fmt.Errorf("node %d is not a child of %d", child, parent)
	}
	// tui.Container.Move PANICS on an out-of-range index, so an engine that
	// computed one would take the program down. This returns instead, which is
	// what lets a test observe the mistake.
	if to < 0 || to >= len(k) {
		return fmt.Errorf("move index %d out of range for %d children", to, len(k))
	}
	rest := append(append([]decl.NodeID{}, k[:from]...), k[from+1:]...)
	s.kids[parent] = append(append(append([]decl.NodeID{}, rest[:to]...), child), rest[to:]...)
	s.trace = append(s.trace, fmt.Sprintf("move %d in %d to %d", child, parent, to))
	return nil
}

// order renders a parent's child list the way the adapter holds it.
func (s *splicer) order(parent decl.NodeID) []decl.NodeID { return s.kids[parent] }

// mounted builds a tree from src and clears the mount trace, so a test asserts
// only what the RECONCILE did.
func mounted(t *testing.T, a decl.Adapter, rec *recorder, src string) *decl.Tree {
	t.Helper()
	tr := decl.New(a)
	if err := tr.Mount(mustSpec(t, src)); err != nil {
		t.Fatalf("fixture mount failed: %v", err)
	}
	rec.trace = nil
	return tr
}

func reconcile(t *testing.T, tr *decl.Tree, src string) decl.Result {
	t.Helper()
	res, err := tr.Reconcile(mustSpec(t, src))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

// ------------------------------------------------------------ the quiet case

// TestReconcileIdenticalSchemaTouchesNothing is the claim the whole phase rests
// on: reloading an unchanged file must not disturb the screen.
//
// It asserts an EMPTY trace rather than a count, because "nothing happened" is
// the contract and a count of zero applies would still pass if the engine had
// destroyed and rebuilt something.
func TestReconcileIdenticalSchemaTouchesNothing(t *testing.T) {
	const src = `Flex {
		direction: "vertical"
		Text { id: a text: "hello" }
		Button { id: b label: "go" onClicked: save }
	}`
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, src)
	before := tr.Root()

	res := reconcile(t, tr, src)

	if len(rec.trace) != 0 {
		t.Errorf("an unchanged schema touched the adapter:\n%s", strings.Join(rec.trace, "\n"))
	}
	if res.Applied != 0 || res.Created != 0 || res.Destroyed != 0 || res.Moved != 0 {
		t.Errorf("unchanged schema reported work: %+v", res)
	}
	if len(res.Rebuilt) != 0 {
		t.Errorf("unchanged schema rebuilt %v", res.Rebuilt)
	}
	if tr.Root() != before {
		t.Errorf("root identity changed: %d -> %d", before, tr.Root())
	}
}

// TestReconcileAppliesOnlyWhatChanged pins the narrowest useful edit: one
// property, one call.
func TestReconcileAppliesOnlyWhatChanged(t *testing.T) {
	const before = `Flex {
		Text { id: a text: "hello" }
		Text { id: b text: "world" }
	}`
	const after = `Flex {
		Text { id: a text: "hello" }
		Text { id: b text: "WORLD" }
	}`
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, before)

	res := reconcile(t, tr, after)

	want := []string{"apply 3 text=string(WORLD) from-schema"}
	if !equalTrace(rec.trace, want) {
		t.Errorf("trace\n got %v\nwant %v", rec.trace, want)
	}
	if res.Applied != 1 {
		t.Errorf("Applied = %d, want 1", res.Applied)
	}
}

// TestReconcileIgnoresAValueThatOnlyMOVED guards the specific mistake of
// comparing positions along with values.
//
// Adding a line above a property changes where it was written and nothing else.
// An engine that compared Position would re-run every setter in the file for an
// edit that changed no value — and since a setter is not required to be
// idempotent, that is a real repaint, not a free no-op.
func TestReconcileIgnoresAValueThatOnlyMOVED(t *testing.T) {
	const before = `Flex {
		Text { id: a text: "hello" }
	}`
	const after = `Flex {

		// a comment the author just added
		Text { id: a text: "hello" }
	}`
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, before)

	reconcile(t, tr, after)

	if len(rec.trace) != 0 {
		t.Errorf("moving a declaration re-applied it:\n%s", strings.Join(rec.trace, "\n"))
	}
}

// ------------------------------------------------------------ forced rebuilds

// TestConsumedPropertyChangeForcesRebuild covers the case the adapter itself
// declared impossible.
//
// Consuming a property is how an adapter says "there is no setter for this".
// Split's orientation is the real instance: changing it has no path through
// Apply, so patching in place is not available and the node must be rebuilt.
func TestConsumedPropertyChangeForcesRebuild(t *testing.T) {
	rec := newSplicer()
	rec.consume["Split"] = []string{"orientation"}
	const before = `Split { orientation: "horizontal" Text {} Text {} }`
	const after = `Split { orientation: "vertical" Text {} Text {} }`

	tr := decl.New(rec)
	if err := tr.Mount(mustSpec(t, before)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	rec.trace = nil

	res := reconcile(t, tr, after)

	if len(res.Rebuilt) != 1 {
		t.Fatalf("Rebuilt = %v, want exactly the Split", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "orientation") ||
		!strings.Contains(res.Rebuilt[0].Reason, "no setter") {
		t.Errorf("the reason does not name the cause: %q", res.Rebuilt[0].Reason)
	}
	if !res.RootReplaced {
		t.Error("the root was rebuilt but RootReplaced is false, so a host would keep rendering a dead tree")
	}
	// The rebuild must not have gone through Apply: there is no setter.
	for _, line := range rec.trace {
		if strings.HasPrefix(line, "apply") && strings.Contains(line, "orientation") {
			t.Errorf("a consumed property was applied after all: %q", line)
		}
	}
}

// TestRemovedPropertyForcesRebuild states a cost rather than hiding it.
//
// There is no "unset" in the seam and the engine holds no default, so a
// property deleted from the schema cannot be walked back. Leaving the old value
// on screen would make the file and the display disagree with no way to tell.
func TestRemovedPropertyForcesRebuild(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Text { id: a text: "hello" } }`)

	res := reconcile(t, tr, `Flex { Text { id: a } }`)

	if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != "Text" {
		t.Fatalf("Rebuilt = %v, want the Text", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "removed") {
		t.Errorf("reason does not name the removal: %q", res.Rebuilt[0].Reason)
	}
	if res.RootReplaced {
		t.Error("only a child was rebuilt; the root should have survived")
	}
}

// TestNewSignalForcesRebuild covers a widget that can only be wired once.
//
// Button takes its activation callback as a constructor option and has no
// SetOnActivate, so a signal that appears for the first time in a reloaded
// schema cannot be attached to the already-built widget.
func TestNewSignalForcesRebuild(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Button { id: b } }`)

	res := reconcile(t, tr, `Flex { Button { id: b onClicked: save } }`)

	if len(res.Rebuilt) != 1 {
		t.Fatalf("Rebuilt = %v, want the Button", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "clicked") {
		t.Errorf("reason does not name the signal: %q", res.Rebuilt[0].Reason)
	}
}

// TestChangedHandlerRebindsWithoutRebuilding is the other half of the signal
// rule, and the one that makes editing a schema pleasant.
//
// Re-pointing an existing signal at a different host function is FREE: the
// emitter wired into the widget calls Emit, which reads the binding at call
// time. The widget is never touched, and the node keeps everything it owns.
func TestChangedHandlerRebindsWithoutRebuilding(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Button { id: b onClicked: save } }`)
	btn := tr.Children(tr.Root())[0]

	res := reconcile(t, tr, `Flex { Button { id: b onClicked: discard } }`)

	if len(res.Rebuilt) != 0 {
		t.Fatalf("rebinding a signal rebuilt something: %v", res.Rebuilt)
	}
	if got := tr.HandlerNames(btn, "clicked"); len(got) != 1 || got[0] != "discard" {
		t.Fatalf("HandlerNames = %v, want [discard]", got)
	}
	// Assert the live wiring, not just the record: the emitter the widget holds
	// must now reach the NEW function. A rebind that updated only the bookkeeping
	// would pass the check above and still run the old handler.
	rec.trace = nil
	if err := tr.Emit(btn, "clicked"); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !equalTrace(rec.trace, []string{"run discard"}) {
		t.Errorf("emitting after a rebind ran %v, want [run discard]", rec.trace)
	}
}

// TestParentThatCannotRestructureIsRebuilt is the degradation path.
//
// The adapter here refuses to restructure a Split, exactly as the real one does
// because widget.Split implements no Add, Remove or Move at all. The child list
// changed, so the Split itself has to be built again.
func TestParentThatCannotRestructureIsRebuilt(t *testing.T) {
	rec := newSplicer("Split")
	tr := mounted(t, rec, rec.recorder,
		`Flex { Split { Text { id: a } Text { id: b } } }`)

	res := reconcile(t, tr,
		`Flex { Split { Text { id: b } Text { id: a } } }`)

	if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != "Split" {
		t.Fatalf("Rebuilt = %v, want the Split", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "cannot be restructured") {
		t.Errorf("reason does not name the cause: %q", res.Rebuilt[0].Reason)
	}
	for _, line := range rec.trace {
		if strings.HasPrefix(line, "move ") {
			t.Errorf("a node that refused restructuring was moved anyway: %q", line)
		}
	}
}

// TestAdapterWithoutTheCapabilityStillReconciles proves the capability is
// genuinely optional.
//
// recorder implements decl.Adapter and nothing more. Property edits must still
// work; only structural ones degrade to a rebuild.
func TestAdapterWithoutTheCapabilityStillReconciles(t *testing.T) {
	rec := newRecorder()
	if _, ok := any(rec).(decl.Restructurer); ok {
		t.Fatal("fixture is wrong: the plain recorder must NOT be a Restructurer")
	}
	tr := mounted(t, rec, rec, `Flex { Text { id: a text: "one" } }`)

	// A property edit: patched in place, no rebuild.
	res := reconcile(t, tr, `Flex { Text { id: a text: "two" } }`)
	if len(res.Rebuilt) != 0 || res.Applied != 1 {
		t.Fatalf("property edit without the capability: %+v", res)
	}

	// A structural edit: the whole parent is rebuilt instead.
	res = reconcile(t, tr, `Flex { Text { id: a text: "two" } Text { id: c } }`)
	if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != "Flex" {
		t.Fatalf("Rebuilt = %v, want the Flex", res.Rebuilt)
	}
}

// --------------------------------------------------------------- structure

// TestReorderByDeclaredIDMovesAndKeepsIdentity is the acceptance case for the
// whole phase.
//
// Three children swap order in the file. Every one of them must keep its NodeID
// — that identity is what the toolkit hangs scroll offset, focus and in-flight
// work on — so the assertion is that NOTHING was created or destroyed and the
// adapter's own child order ends up correct.
func TestReorderByDeclaredIDMovesAndKeepsIdentity(t *testing.T) {
	const before = `Flex {
		Text { id: a }
		Text { id: b }
		Text { id: c }
	}`
	const after = `Flex {
		Text { id: c }
		Text { id: a }
		Text { id: b }
	}`
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, before)
	root := tr.Root()
	was := tr.Children(root)

	res := reconcile(t, tr, after)

	if res.Created != 0 || res.Destroyed != 0 {
		t.Fatalf("a reorder created %d and destroyed %d; both must be 0",
			res.Created, res.Destroyed)
	}
	if len(res.Rebuilt) != 0 {
		t.Fatalf("a reorder rebuilt %v", res.Rebuilt)
	}
	want := []decl.NodeID{was[2], was[0], was[1]}
	if got := tr.Children(root); !equalIDs(got, want) {
		t.Errorf("tree order = %v, want %v", got, want)
	}
	// The engine's bookkeeping agreeing with itself proves nothing. The
	// ADAPTER's order is what a container actually holds, and it is the thing a
	// user sees.
	if got := rec.order(root); !equalIDs(got, want) {
		t.Errorf("adapter order = %v, want %v", got, want)
	}
	if res.Moved == 0 {
		t.Error("Moved = 0, so no identity-preserving relocation was reported")
	}
}

// TestInsertAndRemoveLandAtTheRightIndices exercises the three operations
// together, which is where index arithmetic goes wrong.
func TestInsertAndRemoveLandAtTheRightIndices(t *testing.T) {
	const before = `Flex {
		Text { id: a }
		Text { id: b }
		Text { id: c }
	}`
	// b leaves, x arrives at the front, and c moves ahead of a.
	const after = `Flex {
		Text { id: x }
		Text { id: c }
		Text { id: a }
	}`
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, before)
	root := tr.Root()
	was := tr.Children(root)

	res := reconcile(t, tr, after)

	if res.Destroyed != 1 {
		t.Errorf("Destroyed = %d, want 1 (b)", res.Destroyed)
	}
	if res.Created != 1 {
		t.Errorf("Created = %d, want 1 (x)", res.Created)
	}
	got := tr.Children(root)
	if len(got) != 3 {
		t.Fatalf("children = %v, want 3", got)
	}
	if got[1] != was[2] || got[2] != was[0] {
		t.Errorf("survivors lost their identity: got %v, a=%d c=%d", got, was[0], was[2])
	}
	if adapter := rec.order(root); !equalIDs(adapter, got) {
		t.Errorf("adapter order %v disagrees with the tree %v", adapter, got)
	}
}

// TestDetachHappensBeforeRelease pins an ordering that is invisible until it
// breaks.
//
// A departing child must be detached from its parent while its component still
// exists. Destroy is when the adapter FORGETS the component, so releasing first
// would leave the later detach with nothing to work with — and a real container
// would then silently keep a child the schema deleted.
func TestDetachHappensBeforeRelease(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Text { id: a } Text { id: b } }`)

	reconcile(t, tr, `Flex { Text { id: a } }`)

	remove, destroy := -1, -1
	for i, line := range rec.trace {
		if strings.HasPrefix(line, "remove ") && remove < 0 {
			remove = i
		}
		if strings.HasPrefix(line, "destroy ") && destroy < 0 {
			destroy = i
		}
	}
	if remove < 0 || destroy < 0 {
		t.Fatalf("expected both a detach and a release:\n%s", strings.Join(rec.trace, "\n"))
	}
	if remove > destroy {
		t.Errorf("released before detaching:\n%s", strings.Join(rec.trace, "\n"))
	}
}

// TestRemovedSubtreeIsReleasedChildrenFirst carries the teardown rule into the
// reconciler, where it is easy to forget it applies at all.
//
// IDs are allocated pre-order at mount, so the dropped Flex is 3 and its two
// Texts are 4 and 5. Children-before-parents therefore means exactly [4 5 3],
// and asserting the literal sequence catches a reversal that a set comparison
// would wave through.
func TestRemovedSubtreeIsReleasedChildrenFirst(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Text { id: keep } Flex { id: drop Text {} Text {} } }`)

	reconcile(t, tr, `Flex { Text { id: keep } }`)

	var got []string
	for _, line := range rec.trace {
		if strings.HasPrefix(line, "destroy ") {
			got = append(got, line)
		}
	}
	want := []string{"destroy 4", "destroy 5", "destroy 3"}
	if !equalTrace(got, want) {
		t.Errorf("release order\n got %v\nwant %v\nfull trace:\n%s",
			got, want, strings.Join(rec.trace, "\n"))
	}
}

// TestPositionalMatchingWhenNothingDeclaresAnID covers the fallback rule.
func TestPositionalMatchingWhenNothingDeclaresAnID(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Text { text: "one" } Text { text: "two" } }`)
	root := tr.Root()
	was := tr.Children(root)

	res := reconcile(t, tr, `Flex { Text { text: "ONE" } Text { text: "two" } }`)

	if res.Created != 0 || res.Destroyed != 0 || len(res.Rebuilt) != 0 {
		t.Fatalf("positional match should have patched in place: %+v", res)
	}
	if got := tr.Children(root); !equalIDs(got, was) {
		t.Errorf("identity changed: %v -> %v", was, got)
	}
	if res.Applied != 1 {
		t.Errorf("Applied = %d, want 1", res.Applied)
	}
}

// TestTypeChangeAtTheSamePositionRebuildsOnlyThatNode guards against treating a
// position as an identity when the thing in it became something else.
func TestTypeChangeAtTheSamePositionRebuildsOnlyThatNode(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Text { id: a } Text { id: b } }`)
	root := tr.Root()
	was := tr.Children(root)

	res := reconcile(t, tr, `Flex { Button { id: a } Text { id: b } }`)

	if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != "Button" {
		t.Fatalf("Rebuilt = %v, want the Button", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "type changed") {
		t.Errorf("reason does not name the cause: %q", res.Rebuilt[0].Reason)
	}
	got := tr.Children(root)
	if got[1] != was[1] {
		t.Errorf("the untouched sibling lost its identity: %d -> %d", was[1], got[1])
	}
	if got[0] == was[0] {
		t.Error("the retyped node kept its identity, so it was not actually rebuilt")
	}
	if adapter := rec.order(root); !equalIDs(adapter, got) {
		t.Errorf("adapter order %v disagrees with the tree %v", adapter, got)
	}
}

// ------------------------------------------------------------------ failure

// TestFailedApplyLatchesTheTree holds the engine to the same rule a failed
// Mount already follows.
//
// A setter is the one thing that cannot be pre-checked, so a failure there
// leaves the tree partially reconciled. Refusing the next operation is how that
// is made safe; pretending the reconcile was atomic would not be.
func TestFailedApplyLatchesTheTree(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Text { id: a text: "one" } }`)
	boom := errors.New("this setter refuses")
	rec.applyErr["text"] = boom

	_, err := tr.Reconcile(mustSpec(t, `Flex { Text { id: a text: "two" } }`))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the setter's own error", err)
	}

	_, err = tr.Reconcile(mustSpec(t, `Flex { Text { id: a text: "one" } }`))
	if !errors.Is(err, decl.ErrPhase) {
		t.Fatalf("a second reconcile after a failure returned %v, want ErrPhase", err)
	}
	if err := tr.Mount(mustSpec(t, `Flex {}`)); !errors.Is(err, decl.ErrPhase) {
		t.Fatalf("Mount after a failure returned %v, want ErrPhase", err)
	}
}

// TestPlanningFailsBeforeAnythingIsTouched is the payoff of planning first.
//
// An unresolvable handler name is the common typo, and it is discovered while
// the tree is still intact — so the screen keeps rendering instead of being left
// half-edited.
func TestPlanningFailsBeforeAnythingIsTouched(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Text { id: a text: "one" } Button { id: b onClicked: save } }`)
	rec.resolveErr["nosuchfunc"] = errors.New("no host function named nosuchfunc")

	// The Text's property edit comes FIRST in document order, so an engine that
	// applied as it walked would have already changed it by the time it reached
	// the bad handler.
	_, err := tr.Reconcile(mustSpec(t,
		`Flex { Text { id: a text: "two" } Button { id: b onClicked: nosuchfunc } }`))
	if err == nil {
		t.Fatal("expected the unresolvable handler to fail the reconcile")
	}
	for _, line := range rec.trace {
		if strings.HasPrefix(line, "apply") || strings.HasPrefix(line, "create") ||
			strings.HasPrefix(line, "destroy") {
			t.Errorf("the tree was mutated before planning failed: %q", line)
		}
	}
	// And because nothing was mutated, the tree is still usable.
	if _, err := tr.Reconcile(mustSpec(t,
		`Flex { Text { id: a text: "two" } Button { id: b onClicked: save } }`)); err != nil {
		t.Fatalf("the tree should still be usable after a planning failure: %v", err)
	}
}

// TestReconcileRefusesWhenNotMounted covers the empty case explicitly.
func TestReconcileRefusesWhenNotMounted(t *testing.T) {
	rec := newSplicer()
	tr := decl.New(rec)
	if _, err := tr.Reconcile(mustSpec(t, `Flex {}`)); !errors.Is(err, decl.ErrNotMounted) {
		t.Fatalf("err = %v, want ErrNotMounted", err)
	}
}

// TestEmitIsRefusedDuringAReconcile closes a re-entrancy hole.
//
// A widget can fire while a reconcile is walking the tree — a container
// relaying a removal, say — and a handler that ran then would mutate the
// structure underneath the walk rebuilding it.
func TestEmitIsRefusedDuringAReconcile(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Button { id: b onClicked: save } Text { id: a text: "one" } }`)
	btn := tr.Children(tr.Root())[0]

	var reentry error
	rec.applyErr["text"] = nil
	rec.onApply = func(decl.Application) { reentry = tr.Emit(btn, "clicked") }

	if _, err := tr.Reconcile(mustSpec(t,
		`Flex { Button { id: b onClicked: save } Text { id: a text: "two" } }`)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !errors.Is(reentry, decl.ErrPhase) {
		t.Fatalf("emitting during a reconcile returned %v, want ErrPhase", reentry)
	}
}

// --------------------------------------------------------------- reload

// TestReloadHoldsTheTreeOnIncompleteSource is the rule that makes reload usable
// while someone is typing.
//
// A watcher observes a file part-way through being written. That is not an
// error to show anyone; it is a reason to wait. The tree must be untouched, and
// the caller must be able to TELL this apart from a real syntax error.
func TestReloadHoldsTheTreeOnIncompleteSource(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Text { id: a text: "one" } }`)
	before := tr.Children(tr.Root())

	_, err := tr.Reload([]byte(`Flex { Text { id: a text: "two" }`))
	if !errors.Is(err, decl.ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
	if len(rec.trace) != 0 {
		t.Errorf("an incomplete save touched the adapter:\n%s", strings.Join(rec.trace, "\n"))
	}
	if got := tr.Children(tr.Root()); !equalIDs(got, before) {
		t.Errorf("the tree changed: %v -> %v", before, got)
	}
	// The next, complete save must go through normally.
	if _, err := tr.Reload([]byte(`Flex { Text { id: a text: "two" } }`)); err != nil {
		t.Fatalf("the completed save failed: %v", err)
	}
}

// TestReloadKeepsTheLastGoodTreeOnASyntaxError is the other half of D7: a typo
// must not blank the screen.
func TestReloadKeepsTheLastGoodTreeOnASyntaxError(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Text { id: a text: "one" } }`)
	before := tr.Children(tr.Root())

	_, err := tr.Reload([]byte(`Flex { Text { id: a text: } }`))
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	if errors.Is(err, decl.ErrIncomplete) {
		t.Fatalf("a complete-but-invalid file was reported as incomplete: %v", err)
	}
	if len(rec.trace) != 0 {
		t.Errorf("an invalid save touched the adapter:\n%s", strings.Join(rec.trace, "\n"))
	}
	if got := tr.Children(tr.Root()); !equalIDs(got, before) {
		t.Errorf("the tree changed: %v -> %v", before, got)
	}
}

// TestReloadAppliesAValidSave is the positive control for the two above: the
// checks they make would also pass against a Reload that never worked at all.
func TestReloadAppliesAValidSave(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, `Flex { Text { id: a text: "one" } }`)

	res, err := tr.Reload([]byte(`Flex { Text { id: a text: "two" } }`))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("Applied = %d, want 1", res.Applied)
	}
}

// ------------------------------------------------------------------ helpers

func equalTrace(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalIDs(a, b []decl.NodeID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
