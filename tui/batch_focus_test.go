package tui

// The two seams a composition needs to replace a set of children without
// focus landing somewhere nobody asked for: a mutation batch, and a way for the
// owner of a scope to say where focus belongs inside it.
//
// Both are easy to implement in a way that looks right and is not checkable, so
// these tests go after the properties that distinguish a real boundary from a
// convenient one: how many repairs actually ran, what happens on the panic path,
// and whether a nomination the provider had no right to make is refused.

import (
	"sync/atomic"
	"testing"
)

// repairCounter is a focus trap that counts the repairs the runtime performs
// inside it, by observing where focus lands rather than by instrumenting the
// runtime. Each repair moves focus, and each move delivers a FocusEvent that
// bubbles to this ancestor, so the count is what a component can actually see.
type repairCounter struct {
	scopeProbe
	gains atomic.Int64
}

func newRepairCounter() *repairCounter {
	r := &repairCounter{scopeProbe: *newScopeProbe(true)}
	return r
}

func (r *repairCounter) HandleEvent(ev Event) bool {
	if fe, ok := ev.(FocusEvent); ok && fe.Gained {
		r.gains.Add(1)
	}
	return false
}

// TestBatchTreeMutationRepairsOnceForTheWholeMutation.
//
// Unmounting three focusables one at a time repairs focus three times, each
// against a list that is neither the old set nor the new one. The batch is the
// only way a composition can get one repair against the final tree — sequencing
// the calls differently cannot help, because the repair lives inside Unmount.
func TestBatchTreeMutationRepairsOnceForTheWholeMutation(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	c := newFocusProbe("c", Size{W: 2, H: 1})
	keep := newFocusProbe("keep", Size{W: 2, H: 1})
	scope := newRepairCounter()
	scope.Add(a, b, c, keep)
	h := startApp(t, scope, 8, 8)

	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	scope.gains.Store(0)

	h.onLoop(func() {
		scope.Ctx().BatchTreeMutation(func() {
			scope.Remove(a)
			scope.Remove(b)
			scope.Remove(c)
		})
	})
	h.sync()

	if got := focusedID(h); got != keep.nodeID() {
		t.Errorf("focus is on node %d, want the one survivor %d", got, keep.nodeID())
	}
	// Exactly one node GAINED focus across the whole mutation. Three unbatched
	// unmounts would each re-home focus, and the intermediate landings are
	// visible here as extra gains even though the final resting place is the
	// same — which is why counting the destination alone would prove nothing.
	if got := scope.gains.Load(); got != 1 {
		t.Errorf("%d focus gains during the batch, want exactly 1; the mutation "+
			"repaired against intermediate trees", got)
	}
}

// TestBatchTreeMutationNestsByCountingNotByFlag.
//
// An inner batch is not a second boundary. With a boolean instead of a counter
// the inner call's exit clears the flag, and the rest of the outer batch then
// repairs per operation with nothing to show for it.
func TestBatchTreeMutationNestsByCountingNotByFlag(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	b := newFocusProbe("b", Size{W: 2, H: 1})
	keep := newFocusProbe("keep", Size{W: 2, H: 1})
	scope := newRepairCounter()
	scope.Add(a, b, keep)
	h := startApp(t, scope, 8, 8)

	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })
	scope.gains.Store(0)

	h.onLoop(func() {
		ctx := scope.Ctx()
		ctx.BatchTreeMutation(func() {
			ctx.BatchTreeMutation(func() { scope.Remove(a) })
			// If the inner exit had ended batching, this removal would repair
			// on its own and the count below would be 2.
			scope.Remove(b)
		})
	})
	h.sync()

	if got := scope.gains.Load(); got != 1 {
		t.Errorf("%d focus gains, want 1; an inner batch was treated as a boundary", got)
	}
}

// TestBatchTreeMutationSurvivesAPanicInside.
//
// A panic must not strand the runtime in batching mode. If the depth is left
// raised, every later mutation in the whole application silently defers its
// focus repair forever — a failure that shows up nowhere near the panic.
func TestBatchTreeMutationSurvivesAPanicInside(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 2, H: 1})
	keep := newFocusProbe("keep", Size{W: 2, H: 1})
	scope := newRepairCounter()
	scope.Add(a, keep)
	h := startApp(t, scope, 8, 8)

	h.onLoop(func() { a.ctx.RequestFocus() })
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })

	var depthAfter int
	var recovered any
	h.onLoop(func() {
		func() {
			defer func() { recovered = recover() }()
			scope.Ctx().BatchTreeMutation(func() {
				scope.Remove(a)
				panic("from inside the batch")
			})
		}()
		depthAfter = h.app.batchDepth
	})
	h.sync()

	if recovered == nil {
		t.Fatal("the panic did not propagate out of BatchTreeMutation")
	}
	if depthAfter != 0 {
		t.Errorf("batch depth is %d after a panic, want 0; the runtime is stuck batching", depthAfter)
	}
	// And the pending repair still ran, so focus is not left on a dead node.
	if got := focusedID(h); got != keep.nodeID() {
		t.Errorf("focus is %d after the panic, want %d; the deferred repair was lost",
			got, keep.nodeID())
	}
}

// nominatingScope is a focus trap that nominates a specific component, so the
// provider seam can be driven directly rather than through a widget.
type nominatingScope struct {
	scopeProbe
	nominee  Component
	nominate bool
}

func newNominatingScope() *nominatingScope {
	return &nominatingScope{scopeProbe: *newScopeProbe(true)}
}

func (s *nominatingScope) InitialFocus() (Component, bool) {
	return s.nominee, s.nominate
}

// TestInitialFocusProviderIsPreferredOverDocumentOrder.
func TestInitialFocusProviderIsPreferredOverDocumentOrder(t *testing.T) {
	t.Parallel()
	first := newFocusProbe("first", Size{W: 2, H: 1})
	wanted := newFocusProbe("wanted", Size{W: 2, H: 1})
	scope := newNominatingScope()
	scope.Add(first, wanted)
	scope.nominee, scope.nominate = wanted, true
	h := startApp(t, scope, 8, 8)

	h.onLoop(func() { h.app.repairFocus() })
	h.sync()

	if got := focusedID(h); got != wanted.nodeID() {
		t.Errorf("repair focused %d, want the nominee %d; document order won",
			got, wanted.nodeID())
	}
}

// TestAnIneligibleNominationIsRefused.
//
// A nomination is a preference, not an instruction. The provider is ordinary
// component code and can name something unmounted, something disabled, or
// something in another subtree — and honouring any of those parks focus where
// the user cannot reach it, leaving the scope with no live focus at all.
func TestAnIneligibleNominationIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(h *harness, scope *nominatingScope, first, other *focusProbe)
	}{
		{
			name: "nominee is not focusable right now",
			setup: func(_ *harness, scope *nominatingScope, _, other *focusProbe) {
				other.accepts.Store(false)
				scope.nominee, scope.nominate = other, true
			},
		},
		{
			name: "nominee is not mounted at all",
			setup: func(_ *harness, scope *nominatingScope, _, _ *focusProbe) {
				scope.nominee, scope.nominate = newFocusProbe("stranger", Size{W: 2, H: 1}), true
			},
		},
		{
			name: "provider claims a nomination it cannot supply",
			setup: func(_ *harness, scope *nominatingScope, _, _ *focusProbe) {
				scope.nominee, scope.nominate = nil, true
			},
		},
		{
			name: "provider declines to nominate",
			setup: func(_ *harness, scope *nominatingScope, _, other *focusProbe) {
				scope.nominee, scope.nominate = other, false
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := newFocusProbe("first", Size{W: 2, H: 1})
			other := newFocusProbe("other", Size{W: 2, H: 1})
			scope := newNominatingScope()
			scope.Add(first, other)
			h := startApp(t, scope, 8, 8)
			tc.setup(h, scope, first, other)

			h.onLoop(func() { h.app.repairFocus() })
			h.sync()

			if got := focusedID(h); got != first.nodeID() {
				t.Errorf("repair focused %d, want the document-order fallback %d",
					got, first.nodeID())
			}
		})
	}
}

// TestANominationOutsideTheProvidersSubtreeIsRefused.
//
// A provider may only place focus within itself. Nominating a control belonging
// to a sibling would let a trap hand focus out of the very scope it exists to
// confine.
func TestANominationOutsideTheProvidersSubtreeIsRefused(t *testing.T) {
	t.Parallel()
	inside := newFocusProbe("inside", Size{W: 2, H: 1})
	outside := newFocusProbe("outside", Size{W: 2, H: 1})
	scope := newNominatingScope()
	scope.Add(inside)
	root := NewFlex(Vertical)
	root.Add(scope, outside)
	h := startApp(t, root, 8, 8)

	scope.nominee, scope.nominate = outside, true
	h.onLoop(func() {
		inside.ctx.RequestFocus() // enter the trap so it is the active scope
	})
	waitFor(t, "inside focused", func() bool { return focusedID(h) == inside.nodeID() })
	h.onLoop(func() { h.app.repairFocus() })
	h.sync()

	if got := focusedID(h); got == outside.nodeID() {
		t.Error("the trap nominated a component outside itself and the runtime honoured it")
	}
	if got := focusedID(h); got != inside.nodeID() {
		t.Errorf("repair focused %d, want the in-scope fallback %d", got, inside.nodeID())
	}
}

// TestForwardActionCarriesRuntimeProvenanceAndRefusesOutsideAHandler.
//
// The whole reason this seam exists is that a composite must be able to make a
// child act as though the user had acted on it directly. That is only true if
// the child's activation is dispatched by the runtime, with the runtime's own
// record of what the user did — a provenance the caller could state is one the
// caller could forge.
func TestForwardActionCarriesRuntimeProvenanceAndRefusesOutsideAHandler(t *testing.T) {
	t.Parallel()
	child := newActivatableProbe("child")
	parent := newForwardingProbe(child)
	parent.Add(child)
	h := startApp(t, parent, 8, 8)

	var events []ControlActivatedEvent
	unsub := Subscribe(h.app.Bus(), func(ev ControlActivatedEvent) {
		events = append(events, ev)
	})
	defer unsub()

	// Outside a handler there is no invocation to take provenance from.
	var outside bool
	h.onLoop(func() { outside = parent.ctx.ForwardAction(child, ActivateAction{}) })
	if outside {
		t.Error("ForwardAction succeeded outside an action handler, so it invented a provenance")
	}

	// Driven by a real KEYPRESS, deliberately not by DoAction. The provenance
	// under test must be the one the runtime recorded for the user's input, and
	// a programmatic trigger would leave OriginProgrammatic — which is also what
	// a forged or defaulted origin looks like, so the assertion would accept the
	// very defect it is meant to catch.
	h.onLoop(func() { parent.ctx.RequestFocus() })
	waitFor(t, "parent focused", func() bool { return focusedID(h) == parent.ctx.ID() })
	h.inject(KeyEvent{Kind: KeyPress, Code: KeyF1})
	waitFor(t, "child activated", func() bool { return child.activations.Load() == 1 })
	h.sync()

	if child.activations.Load() != 1 {
		t.Fatalf("child activated %d times, want 1", child.activations.Load())
	}
	if len(events) != 1 {
		t.Fatalf("%d ControlActivatedEvent, want exactly 1; a direct Activate call "+
			"runs the callback but publishes nothing", len(events))
	}
	if events[0].Owner != child.nodeID() {
		t.Errorf("event names node %d, want the child %d", events[0].Owner, child.nodeID())
	}
	if events[0].Origin != OriginKey {
		t.Errorf("origin = %v, want OriginKey: the child's activation must carry the "+
			"provenance of the keypress that reached the parent", events[0].Origin)
	}
	if got := ActionOrigin(child.lastOrigin.Load()); got != OriginKey {
		t.Errorf("Activate saw origin %v, want OriginKey", got)
	}
}

// forwardTrigger is the action the forwarding parent handles.
type forwardTrigger struct{}

func (forwardTrigger) ActionID() ActionID { return "test.forward" }

// forwardingProbe forwards its own action to a named child.
type forwardingProbe struct {
	Flex
	ctx     *Context
	target  Component
	handled atomic.Int64
}

func newForwardingProbe(target Component) *forwardingProbe {
	return &forwardingProbe{Flex: *NewFlex(Vertical), target: target}
}

func (p *forwardingProbe) Init(ctx *Context) {
	p.ctx = ctx
	p.Flex.Init(ctx)
	ctx.SetDefaultActionResolvers(p)
}

// AcceptsFocus makes the parent a focus target so a keypress reaches it.
func (p *forwardingProbe) AcceptsFocus() bool { return true }

// Resolve binds F1 to the action this probe forwards, so the invocation the
// runtime builds carries keyboard provenance.
func (p *forwardingProbe) Resolve(ev Event) (Action, bool) {
	if k, ok := ev.(KeyEvent); ok && k.Kind == KeyPress && k.Code == KeyF1 {
		return forwardTrigger{}, true
	}
	return nil, false
}

func (p *forwardingProbe) HandleAction(inv ActionInvocation) bool {
	if _, ok := inv.Action.(forwardTrigger); !ok {
		return false
	}
	p.handled.Add(1)
	return p.ctx.ForwardAction(p.target, ActivateAction{})
}

// activatableProbe is a focusable leaf that counts its activations.
type activatableProbe struct {
	focusProbe
	activations atomic.Int64
	lastOrigin  atomic.Int64
	armed       atomic.Bool
}

func newActivatableProbe(name string) *activatableProbe {
	p := &activatableProbe{focusProbe: *newFocusProbe(name, Size{W: 2, H: 1})}
	return p
}

func (p *activatableProbe) Activate(origin ActionOrigin) bool {
	p.lastOrigin.Store(int64(origin))
	p.activations.Add(1)
	return true
}

// SetArmed completes the Activatable contract. The probe has no visual state to
// arm, so it only records that the runtime drove the transition.
func (p *activatableProbe) SetArmed(v bool) { p.armed.Store(v) }

// TestForwardActionRefusesATargetOutsideTheSubtree.
//
// Forwarding carries the runtime's own record of what the user did. Letting a
// component aim that at an arbitrary node would let anything act in another's
// name with borrowed provenance — the same hole the handler-identity rule
// already closes for pointer capture.
func TestForwardActionRefusesATargetOutsideTheSubtree(t *testing.T) {
	t.Parallel()
	// Both directions in ONE fixture, because "nothing was activated" is also
	// what a keypress that never arrived looks like. The in-subtree target is the
	// positive control: it proves the forward actually ran before the
	// out-of-subtree target is asked to have been refused.
	for _, tc := range []struct {
		name    string
		outside bool
	}{{"a target inside the subtree is forwarded to", false},
		{"a target outside the subtree is refused", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stranger := newActivatableProbe("stranger")
			inner := newActivatableProbe("inner")
			target := Component(inner)
			if tc.outside {
				target = stranger
			}
			parent := newForwardingProbe(target)
			parent.Add(inner)
			root := NewFlex(Vertical)
			root.Add(parent, stranger)
			h := startApp(t, root, 8, 8)

			h.onLoop(func() { parent.ctx.RequestFocus() })
			waitFor(t, "parent focused", func() bool { return focusedID(h) == parent.ctx.ID() })
			h.inject(KeyEvent{Kind: KeyPress, Code: KeyF1})
			if tc.outside {
				// Wait on something that must happen either way, so the absence
				// below is measured after the dispatch rather than before it.
				waitFor(t, "the key was dispatched", func() bool {
					var handled bool
					h.onLoop(func() { handled = parent.handled.Load() > 0 })
					return handled
				})
			} else {
				waitFor(t, "inner activated", func() bool { return inner.activations.Load() == 1 })
			}
			h.sync()

			if got := stranger.activations.Load(); got != 0 {
				t.Errorf("a component outside the forwarding node's subtree was "+
					"activated %d times with borrowed provenance", got)
			}
			if !tc.outside && inner.activations.Load() != 1 {
				t.Errorf("the in-subtree target was activated %d times, want 1",
					inner.activations.Load())
			}
		})
	}
}
