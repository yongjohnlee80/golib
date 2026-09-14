package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// Actions exist so a widget states an intent once instead of once per input
// device. These tests therefore care most about the two things that make that
// true: the SAME action arriving from a key and from a pointer reaching the
// same handler, and provenance being something the runtime records rather than
// something a producer can assert.

// moveAction is a test-local action standing in for the widget-owned ones.
type moveAction struct{ Delta int }

func (moveAction) ActionID() ActionID { return "test.move" }

// otherAction exists so "the right action arrived" is distinguishable from
// "an action arrived".
type otherAction struct{}

func (otherAction) ActionID() ActionID { return "test.other" }

// actor records the actions and raw events it receives. It consumes actions
// only when told to, so the fall-through to raw can be exercised on demand.
type actor struct {
	MultiChild
	size Size

	takeAction  atomic.Bool
	actions     atomic.Int64
	raws        atomic.Int64
	activations atomic.Int64

	mu       sync.Mutex
	lastInv  ActionInvocation
	lastOrig ActionOrigin
}

func (ac *actor) Layout(cs Constraints) Size {
	if ac.ctx != nil {
		for _, ch := range ac.All() {
			sz := ac.ctx.LayoutChild(ch, Loose(Size{W: cs.MaxW, H: cs.MaxH}))
			ac.ctx.PlaceChild(ch, Rect{W: sz.W, H: sz.H})
		}
	}
	return cs.Constrain(ac.size)
}
func (ac *actor) Render(Surface)     {}
func (ac *actor) AcceptsFocus() bool { return true }

func (ac *actor) HandleEvent(ev Event) bool {
	switch ev.(type) {
	case KeyEvent, MouseEvent, UserEvent:
		ac.raws.Add(1)
	}
	return false
}

func (ac *actor) HandleAction(inv ActionInvocation) bool {
	ac.actions.Add(1)
	ac.mu.Lock()
	ac.lastInv = inv
	ac.mu.Unlock()
	return ac.takeAction.Load()
}

func (ac *actor) Activate(origin ActionOrigin) bool {
	ac.activations.Add(1)
	ac.lastOrig = origin
	return true
}

func (ac *actor) SetArmed(bool) {} // visual only; nothing drives it yet

func (ac *actor) inv() ActionInvocation {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.lastInv
}

// moveResolver turns any key into a move, and any press into the same move.
// One resolver for both devices is the whole point being tested.
type moveResolver struct{ delta int }

func (m moveResolver) Resolve(ev Event) (Action, bool) {
	switch e := ev.(type) {
	case KeyEvent:
		return moveAction{Delta: m.delta}, true
	case MouseEvent:
		if e.Kind == MousePress {
			return moveAction{Delta: m.delta}, true
		}
	}
	return nil, false
}

// TestOneActionServesKeyAndPointer is the reason the layer exists: a widget
// implements the intent once and both devices reach it.
func TestOneActionServesKeyAndPointer(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	ac.takeAction.Store(true)
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 1})
		h.app.requestFocus(h.app.byComp[ac])
	})
	h.sync()

	h.inject(keyEv('j'))
	waitFor(t, "key resolved to an action", func() bool { return ac.actions.Load() == 1 })
	h.sync()
	if got := ac.inv().Origin; got != OriginKey {
		t.Errorf("origin from a key = %v, want %v", got, OriginKey)
	}

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "press resolved to an action", func() bool { return ac.actions.Load() == 2 })
	h.sync()
	if got := ac.inv().Origin; got != OriginPointer {
		t.Errorf("origin from a press = %v, want %v", got, OriginPointer)
	}
	if got := ac.inv().Action.ActionID(); got != "test.move" {
		t.Errorf("action = %q, want test.move: both devices must reach the same intent", got)
	}
	// Raw delivery must NOT also have happened for a consumed action.
	if got := ac.raws.Load(); got != 0 {
		t.Errorf("the node also received %d raw events for consumed actions, want 0", got)
	}
}

// forgingResolver tries to claim a provenance it does not have, by returning an
// action for a mouse event that a naive design might let it label as a key.
type forgingResolver struct{}

// It matches only pointer input: a resolver that matched every event would
// also claim the focus notifications the runtime bubbles, and the test would
// then be counting those too.
func (forgingResolver) Resolve(ev Event) (Action, bool) {
	if _, ok := ev.(MouseEvent); ok {
		return moveAction{Delta: 9}, true
	}
	return nil, false
}

// TestProvenanceIsTheRuntimesToRecord. A widget polices input by origin — a
// resize that is allowed from the keyboard but not the mouse, say — so a
// resolver able to state its own origin could walk straight through that gate.
// Resolvers return an action ONLY; origin comes from the event the runtime saw.
func TestProvenanceIsTheRuntimesToRecord(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	ac.takeAction.Store(true)
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers(forgingResolver{}) })
	h.sync()

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "action dispatched", func() bool { return ac.actions.Load() == 1 })
	h.sync()

	inv := ac.inv()
	if inv.Origin != OriginPointer {
		t.Errorf("origin = %v, want %v: origin is derived from the event the runtime "+
			"saw, never from anything the resolver returns", inv.Origin, OriginPointer)
	}
	if inv.Source == nil {
		t.Error("Source is nil for a pointer-derived action; the originating event is " +
			"part of the provenance a widget may need to inspect")
	}
	if _, ok := inv.Source.(MouseEvent); !ok {
		t.Errorf("Source is %T, want MouseEvent", inv.Source)
	}
}

// TestAnUnhandledActionFallsThroughToRaw. A resolver claiming an event must not
// swallow it when the node ignores the action it produced, or a widget that
// handles only some of its own actions goes deaf to the rest.
func TestAnUnhandledActionFallsThroughToRaw(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	ac.takeAction.Store(false) // resolves, but refuses the action
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 1})
		h.app.requestFocus(h.app.byComp[ac])
	})
	h.sync()

	h.inject(keyEv('j'))
	waitFor(t, "raw fallback delivered", func() bool { return ac.raws.Load() == 1 })
	h.sync()

	if got := ac.actions.Load(); got != 1 {
		t.Errorf("action dispatch count = %d, want 1", got)
	}
	if got := ac.raws.Load(); got != 1 {
		t.Errorf("raw fallback count = %d, want 1: an action the node refused must still "+
			"reach its raw handler on the SAME node", got)
	}
}

// countingResolver records whether it was consulted, so "first match wins" can
// be asserted by a resolver that must NOT run rather than only by one that did.
type countingResolver struct {
	act   Action
	match bool
	calls atomic.Int64 // key events only — what the tests inject
	all   atomic.Int64 // EVERY consultation, including ones the tests do not want
}

// Only keys are counted, so the tally reflects the events the test actually
// injects rather than every notification that happens to pass through.
func (c *countingResolver) Resolve(ev Event) (Action, bool) {
	c.all.Add(1)
	if _, ok := ev.(KeyEvent); !ok {
		return nil, false
	}
	c.calls.Add(1)
	if !c.match {
		return nil, false
	}
	return c.act, true
}

// TestFirstMatchWinsEvenWhenTheActionIsRefused. A later resolver is not a
// fallback for an earlier one's unhandled action: if it were, one event could
// produce two different intents on the same node.
func TestFirstMatchWinsEvenWhenTheActionIsRefused(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	ac.takeAction.Store(false)
	root.Add(ac)

	first := &countingResolver{act: moveAction{Delta: 1}, match: true}
	second := &countingResolver{act: otherAction{}, match: true}

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[ac].ctx.SetActionResolvers(first, second)
		h.app.requestFocus(h.app.byComp[ac])
	})
	h.sync()

	h.inject(keyEv('j'))
	waitFor(t, "raw fallback delivered", func() bool { return ac.raws.Load() == 1 })
	h.sync()

	if got := first.calls.Load(); got != 1 {
		t.Fatalf("the first resolver ran %d times, want 1: without it this proves nothing", got)
	}
	if got := second.calls.Load(); got != 0 {
		t.Errorf("the second resolver ran %d times, want 0: first match wins, so a "+
			"refused action does not hand the event to the next resolver", got)
	}
}

// TestConsumerResolversAreTriedBeforeDefaults, and clearing the consumer layer
// restores the defaults rather than leaving the node with none. A single
// undifferentiated list cannot do the second part at all.
func TestConsumerResolversAreTriedBeforeDefaults(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &defaultingActor{actor: actor{size: Size{W: 20, H: 4}}}
	ac.takeAction.Store(true)
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[ac]) })
	h.sync()

	// Only the default layer, installed during Init.
	h.inject(keyEv('j'))
	waitFor(t, "default resolver produced an action", func() bool { return ac.actions.Load() == 1 })
	h.sync()
	if got := ac.inv().Action.ActionID(); got != "test.other" {
		t.Fatalf("default layer produced %q, want test.other", got)
	}

	// A consumer entry is tried FIRST.
	h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 1}) })
	h.sync()
	h.inject(keyEv('j'))
	waitFor(t, "consumer resolver produced an action", func() bool { return ac.actions.Load() == 2 })
	h.sync()
	if got := ac.inv().Action.ActionID(); got != "test.move" {
		t.Errorf("with a consumer resolver installed the action was %q, want test.move", got)
	}

	// Clearing the consumer layer must leave the defaults working.
	h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers() })
	h.sync()
	h.inject(keyEv('j'))
	waitFor(t, "default resolver again", func() bool { return ac.actions.Load() == 3 })
	h.sync()
	if got := ac.inv().Action.ActionID(); got != "test.other" {
		t.Errorf("after clearing the consumer layer the action was %q, want test.other: "+
			"clearing must restore the defaults, not remove all resolution", got)
	}
}

// defaultingActor installs a default resolver during its own Init, the way a
// widget publishes its own bindings.
type defaultingActor struct{ actor }

func (d *defaultingActor) Init(ctx *Context) {
	d.actor.MultiChild.Init(ctx)
	ctx.SetDefaultActionResolvers(&countingResolver{act: otherAction{}, match: true})
}

// TestSetDefaultActionResolversIsInitOnly. The default layer is a widget's
// published behaviour, so it is fixed once the component is live.
func TestSetDefaultActionResolversIsInitOnly(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var fatal *errs.Fatal
	h.onLoop(func() {
		fatal = fatalFrom(func() {
			h.app.byComp[ac].ctx.SetDefaultActionResolvers(moveResolver{delta: 1})
		})
	})
	h.sync()
	if fatal == nil {
		t.Error("SetDefaultActionResolvers outside Init did not raise errs.Fatal")
	}
	// Positive control: the same call DID succeed during Init for the
	// defaulting actor, which the resolver-order test above relies on.
	root2 := &counter{size: Size{W: 20, H: 4}}
	d := &defaultingActor{actor: actor{size: Size{W: 20, H: 4}}}
	root2.Add(d)
	h2 := startApp(t, root2, 20, 4)
	defer h2.wait()
	h2.sync()
	var n int
	h2.onLoop(func() { n = len(h2.app.byComp[d].ctx.ActionResolvers()) })
	if n != 1 {
		t.Errorf("the Init-time default layer holds %d resolvers, want 1: if it is empty "+
			"the negative case above is not testing a real restriction", n)
	}
}

// TestOneNodesInitCannotSetAnothersDefaults. The Init marker is an identity for
// the same reason the handler marker is: Init is re-entrant, so a parent's Init
// is still running while a child's runs inside it.
func TestOneNodesInitCannotSetAnothersDefaults(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	p := &initImpersonator{size: Size{W: 20, H: 4}}
	p.child = &actor{size: Size{W: 10, H: 2}}
	root.Add(p)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	if !p.ran.Load() {
		t.Fatal("precondition failed: the parent Init never attempted the call")
	}
	if !p.fatal.Load() {
		t.Error("a parent's Init set a CHILD's default resolvers; the marker must name " +
			"which node is initialising, not merely that something is")
	}
	// POSITIVE CONTROL. Without it this passes just as well against a runtime
	// that never marks Init at all, since every call would then be refused.
	if !p.ownOK.Load() {
		t.Error("the parent could not set its OWN defaults during its own Init; the " +
			"refusal above is then not about identity, it is about nothing working")
	}
}

// initImpersonator mounts a child during its own Init and then tries to install
// defaults on that child's Context rather than its own.
type initImpersonator struct {
	MultiChild
	size  Size
	child *actor
	ran   atomic.Bool
	fatal atomic.Bool
	ownOK atomic.Bool
}

func (ii *initImpersonator) Init(ctx *Context) {
	ii.MultiChild.Init(ctx)
	ctx.Mount(ii.child)
	ii.ran.Store(true)
	// Its OWN defaults must still be settable here — the control that makes the
	// refusal below mean "wrong node" rather than "no node".
	if fatalFrom(func() { ctx.SetDefaultActionResolvers(moveResolver{delta: 1}) }) == nil {
		ii.ownOK.Store(true)
	}
	// The child's Init has returned; the parent's has not. A marker that only
	// said "some Init is running" would admit this.
	if cc := ctx.app.byComp[ii.child]; cc != nil {
		if fatalFrom(func() { cc.ctx.SetDefaultActionResolvers(moveResolver{delta: 1}) }) != nil {
			ii.fatal.Store(true)
		}
	}
}
func (ii *initImpersonator) Layout(cs Constraints) Size { return cs.Constrain(ii.size) }
func (ii *initImpersonator) Render(Surface)             {}
func (ii *initImpersonator) HandleEvent(Event) bool     { return false }

// TestActivateReachesTheSameRuleFromEveryProducer. Activation must not depend on
// which producer asked for it: an earlier design reached Activate only from a
// gesture, so a key binding resolving to ActivateAction silently did nothing.
func TestActivateReachesTheSameRuleFromEveryProducer(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &activateOnly{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[ac].ctx.SetActionResolvers(ActionResolverFunc(func(ev Event) (Action, bool) {
			if _, ok := ev.(KeyEvent); ok {
				return ActivateAction{}, true
			}
			return nil, false
		}))
		h.app.requestFocus(h.app.byComp[ac])
	})
	h.sync()

	// From a KEY resolver.
	h.inject(keyEv('\r'))
	waitFor(t, "key activation", func() bool { return ac.activations.Load() == 1 })
	if got := ac.lastOrigin(); got != OriginKey {
		t.Errorf("origin = %v, want %v", got, OriginKey)
	}

	// And programmatically, through the same rule.
	var ok bool
	h.onLoop(func() { ok = h.app.byComp[ac].ctx.DoAction(ActivateAction{}) })
	h.sync()
	if !ok {
		t.Error("DoAction(ActivateAction) returned false; every producer reaches the " +
			"same activation rule")
	}
	if got := ac.activations.Load(); got != 2 {
		t.Errorf("activations = %d, want 2", got)
	}
	if got := ac.lastOrigin(); got != OriginProgrammatic {
		t.Errorf("origin = %v, want %v", got, OriginProgrammatic)
	}
}

// activateOnly implements Activatable but NOT ActionHandler, which is the case
// the Activatable fallback exists for.
type activateOnly struct {
	MultiChild
	size        Size
	activations atomic.Int64
	origin      atomic.Int64
}

func (a *activateOnly) Layout(cs Constraints) Size { return cs.Constrain(a.size) }
func (a *activateOnly) Render(Surface)             {}
func (a *activateOnly) AcceptsFocus() bool         { return true }
func (a *activateOnly) HandleEvent(Event) bool     { return false }
func (a *activateOnly) Activate(origin ActionOrigin) bool {
	a.activations.Add(1)
	a.origin.Store(int64(origin))
	return true
}
func (a *activateOnly) SetArmed(bool)            {} // visual only; nothing drives it yet
func (a *activateOnly) lastOrigin() ActionOrigin { return ActionOrigin(a.origin.Load()) }

// TestAComponentWithoutAnActionHandlerIsUnaffected. Actions are an optional
// capability probed by type assertion, so a component that predates them must
// behave byte-identically.
func TestAComponentWithoutAnActionHandlerIsUnaffected(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	plain := &counter{size: Size{W: 20, H: 4}}
	root.Add(plain)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	// A resolver that matches everything, installed on a node that cannot
	// receive actions at all.
	h.onLoop(func() {
		h.app.byComp[plain].ctx.SetActionResolvers(forgingResolver{})
		h.app.requestFocus(h.app.byComp[plain])
	})
	h.sync()

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "raw delivery", func() bool {
		_, _, m := plain.totals()
		return m > 0
	})
	h.sync()
	if _, _, m := plain.totals(); m != 1 {
		t.Errorf("a component without HandleAction received %d raw mouse events, want 1: "+
			"an unhandled action must fall through untouched", m)
	}
}

// TestUserEventCarriesConsumerInput. Event is sealed, so consumers need a
// sanctioned way in; a UserEvent resolves like any other input and is reported
// with its own origin.
func TestUserEventCarriesConsumerInput(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	ac.takeAction.Store(true)
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[ac].ctx.SetActionResolvers(ActionResolverFunc(func(ev Event) (Action, bool) {
			if u, ok := ev.(UserEvent); ok && u.Tag == "gamepad.left" {
				return moveAction{Delta: -1}, true
			}
			return nil, false
		}))
		h.app.requestFocus(h.app.byComp[ac])
	})
	h.sync()

	h.app.Post(UserEvent{Tag: "gamepad.left"})
	waitFor(t, "user event resolved", func() bool { return ac.actions.Load() == 1 })
	h.sync()

	inv := ac.inv()
	if inv.Origin != OriginUser {
		t.Errorf("origin = %v, want %v", inv.Origin, OriginUser)
	}
	if got := inv.Action.ActionID(); got != "test.move" {
		t.Errorf("action = %q, want test.move", got)
	}
}

// TestDoActionSkipsResolutionAndDoesNotBubble. The caller named the node it
// meant, so neither the resolver chain nor an ancestor should get involved.
func TestDoActionSkipsResolutionAndDoesNotBubble(t *testing.T) {
	root := &actor{size: Size{W: 20, H: 4}}
	root.takeAction.Store(true)
	child := &actor{size: Size{W: 10, H: 2}}
	child.takeAction.Store(false) // refuses, so a bubble would be visible at root
	root.Add(child)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	consulted := &countingResolver{act: otherAction{}, match: true}
	h.onLoop(func() { h.app.byComp[child].ctx.SetActionResolvers(consulted) })
	h.sync()

	var ok bool
	h.onLoop(func() { ok = h.app.byComp[child].ctx.DoAction(moveAction{Delta: 3}) })
	h.sync()

	if ok {
		t.Error("DoAction reported handled although the node refused the action")
	}
	if got := consulted.all.Load(); got != 0 {
		t.Errorf("the resolver chain was consulted %d times, want 0: DoAction names its "+
			"action directly. This counts EVERY consultation, not just matching ones — "+
			"counting only matches would pass against a DoAction that resolved with some "+
			"other event type", got)
	}
	if got := root.actions.Load(); got != 0 {
		t.Errorf("the parent received %d actions, want 0: DoAction does not bubble", got)
	}
	if got := child.actions.Load(); got != 1 {
		t.Errorf("the named node received %d actions, want 1", got)
	}
}

// TestNilResolverIsRejectedAtTheSetter. A nil entry reaching the chain would
// panic in the middle of routing, naming neither the component that supplied it
// nor the call that did.
func TestNilResolverIsRejectedAtTheSetter(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var fatal *errs.Fatal
	h.onLoop(func() {
		fatal = fatalFrom(func() {
			h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 1}, nil)
		})
	})
	h.sync()
	if fatal == nil {
		t.Fatal("a nil resolver was accepted by the setter")
	}
	if !strings.Contains(fatal.Detail, "1") {
		t.Errorf("panic detail %q does not name which entry was nil", fatal.Detail)
	}
	// And the node's chain is unchanged, so a rejected call is not a partial one.
	var n int
	h.onLoop(func() { n = len(h.app.byComp[ac].ctx.ActionResolvers()) })
	if n != 0 {
		t.Errorf("the resolver chain holds %d entries after a rejected call, want 0", n)
	}
}

// TestActionResolversSnapshotCannotMutateTheNode. The accessor hands back the
// chain for inspection; writing through it would edit the node's own layers.
func TestActionResolversSnapshotCannotMutateTheNode(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 1}) })

	var same bool
	h.onLoop(func() {
		c := h.app.byComp[ac].ctx
		snap := c.ActionResolvers()
		snap[0] = nil // must not reach the node
		again := c.ActionResolvers()
		same = again[0] != nil
	})
	h.sync()
	if !same {
		t.Error("writing through the returned slice mutated the node's resolver chain")
	}
}

// --- the two capture seams the action layer closes ---

// actionDragger captures the pointer from its ACTION handler, which is how a
// semantic drag begins: a resolver turns the press into a begin-drag action,
// and the handler for that action takes the pointer.
type actionDragger struct {
	MultiChild
	size Size

	grab     atomic.Bool
	granted  atomic.Int64
	refused  atomic.Int64
	fatal    atomic.Bool
	actions  atomic.Int64
	raws     atomic.Int64
	victim   atomic.Pointer[Context] // set to impersonate another node
	impFatal atomic.Bool
	impRan   atomic.Bool
}

func (ad *actionDragger) Layout(cs Constraints) Size { return cs.Constrain(ad.size) }
func (ad *actionDragger) Render(Surface)             {}
func (ad *actionDragger) AcceptsFocus() bool         { return true }

func (ad *actionDragger) HandleEvent(ev Event) bool {
	if _, ok := ev.(MouseEvent); ok {
		ad.raws.Add(1)
	}
	return false
}

func (ad *actionDragger) HandleAction(inv ActionInvocation) bool {
	ad.actions.Add(1)
	if v := ad.victim.Load(); v != nil {
		ad.impRan.Store(true)
		if fatalFrom(func() { v.CapturePointer() }) != nil {
			ad.impFatal.Store(true)
		}
		return true
	}
	if ad.grab.Load() && ad.ctx != nil {
		if fatalFrom(func() {
			if ad.ctx.CapturePointer() {
				ad.granted.Add(1)
			} else {
				ad.refused.Add(1)
			}
		}) != nil {
			ad.fatal.Store(true)
		}
	}
	return true
}

// TestCaptureCanBeTakenFromAnActionHandler closes the first seam. Without it a
// drag can only begin from a raw event handler, so a widget wanting semantic
// input would have to read raw events anyway just to start its own gesture —
// which is the duplication the action layer exists to remove.
func TestCaptureCanBeTakenFromAnActionHandler(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ad := &actionDragger{size: Size{W: 20, H: 4}}
	ad.grab.Store(true)
	root.Add(ad)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() { h.app.byComp[ad].ctx.SetActionResolvers(anyPointerResolver{}) })
	h.sync()

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "action handler ran", func() bool { return ad.actions.Load() == 1 })
	h.sync()

	if ad.fatal.Load() {
		t.Fatal("CapturePointer from HandleAction raised errs.Fatal; an action handler " +
			"is a runtime-dispatched input phase with a live gesture in hand")
	}
	if got := ad.granted.Load(); got != 1 {
		t.Errorf("captures granted from the action handler = %d, want 1", got)
	}
	var held bool
	h.onLoop(func() { held = h.app.captureOwner == h.app.byComp[ad].id })
	if !held {
		t.Error("the node does not hold the capture it was granted")
	}
}

// TestAnActionHandlerCannotCaptureInAnothersName. Widening the legal phases must
// not widen WHO may be captured for: the identity rule that closed this hole on
// the event path has to hold on the action path too.
func TestAnActionHandlerCannotCaptureInAnothersName(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	victim := &dragger{size: Size{W: 10, H: 4}}
	ad := &actionDragger{size: Size{W: 10, H: 4}}
	root.Add(victim, ad)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		ad.victim.Store(h.app.byComp[victim].ctx)
		h.app.byComp[ad].ctx.SetActionResolvers(anyPointerResolver{})
		h.app.requestFocus(h.app.byComp[ad])
	})
	h.sync()

	h.onLoop(func() {
		h.app.deliverAction(h.app.byComp[ad], ad, ActionInvocation{
			Action: moveAction{Delta: 1}, Origin: OriginProgrammatic,
		})
	})
	h.sync()

	if !ad.impRan.Load() {
		t.Fatal("precondition failed: the impersonating action handler never ran")
	}
	if !ad.impFatal.Load() {
		t.Error("an action handler captured the pointer in another node's name; the " +
			"identity requirement must hold on the action path too")
	}
	var heldAfterImp NodeID
	h.onLoop(func() { heldAfterImp = h.app.captureOwner })
	if heldAfterImp != 0 {
		t.Errorf("capture is held by %d after an impersonated acquisition", heldAfterImp)
	}

	// POSITIVE CONTROL, in this test rather than a neighbouring one. A handler
	// marker set to any WRONG node refuses both calls, so without proving the
	// legitimate acquisition still succeeds, the refusal above says only that
	// something was refused.
	ad.victim.Store(nil)
	ad.grab.Store(true)
	h.onLoop(func() {
		h.app.deliverAction(h.app.byComp[ad], ad, ActionInvocation{
			Action: moveAction{Delta: 1}, Origin: OriginProgrammatic,
		})
	})
	h.sync()
	if ad.fatal.Load() || ad.granted.Load() != 1 {
		t.Errorf("the node could not capture for ITSELF from its own action handler "+
			"(fatal=%v granted=%d); the refusal above is then not about identity",
			ad.fatal.Load(), ad.granted.Load())
	}
}

// TestCapturedDeliveryRunsTheSemanticPath closes the second seam. A drag begun
// as an action must CONTINUE as one: if captured motion arrived only as a raw
// event, every widget would need a second state machine for the captured half
// of its own gesture.
func TestCapturedDeliveryRunsTheSemanticPath(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ad := &actionDragger{size: Size{W: 10, H: 4}}
	ad.grab.Store(true)
	other := &counter{size: Size{W: 10, H: 4}}
	root.Add(ad, other)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	// Resolves presses AND motion, so captured motion has a semantic reading.
	h.onLoop(func() {
		h.app.byComp[ad].ctx.SetActionResolvers(ActionResolverFunc(func(ev Event) (Action, bool) {
			if e, ok := ev.(MouseEvent); ok && (e.Kind == MousePress || e.Kind == MouseMotion) {
				return moveAction{Delta: 1}, true
			}
			return nil, false
		}))
	})
	h.sync()

	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "capture taken from the action handler", func() bool { return ad.granted.Load() == 1 })
	h.sync()
	actionsAfterPress := ad.actions.Load()

	// Motion far outside the owner, over the sibling.
	h.inject(MouseEvent{Kind: MouseMotion, X: 15, Y: 1})
	waitFor(t, "captured motion resolved to an action", func() bool {
		return ad.actions.Load() == actionsAfterPress+1
	})
	h.sync()

	if got := ad.raws.Load(); got != 0 {
		t.Errorf("the owner received %d RAW captured events, want 0: a captured event "+
			"runs the same sequence an uncaptured one does, so a consumed action must "+
			"not also fall through to raw", got)
	}
	if _, _, m := other.totals(); m != 0 {
		t.Errorf("the sibling under the pointer received %d events, want 0", m)
	}
}

// TestCapturedDeliveryStillFallsThroughToRawWhenUnhandled. Running the semantic
// path first must not strand a widget that captures semantically and then reads
// raw motion, which is a legitimate mix.
func TestCapturedDeliveryStillFallsThroughToRawWhenUnhandled(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 10, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	startDrag(t, h, owner, 2, 1)

	// A resolver that claims motion but produces an action nothing handles:
	// dragger has no HandleAction at all.
	h.onLoop(func() {
		h.app.byComp[owner].ctx.SetActionResolvers(ActionResolverFunc(func(ev Event) (Action, bool) {
			if e, ok := ev.(MouseEvent); ok && e.Kind == MouseMotion {
				return moveAction{Delta: 1}, true
			}
			return nil, false
		}))
	})
	h.sync()

	h.inject(MouseEvent{Kind: MouseMotion, X: 15, Y: 1})
	waitFor(t, "raw captured motion delivered", func() bool { return owner.motions.Load() == 1 })
	h.sync()

	if got := owner.motions.Load(); got != 1 {
		t.Errorf("captured raw motions = %d, want 1: an unhandled action must still fall "+
			"through to the owner's raw handler", got)
	}
}

// TestFocusNotificationsDoNotConsultResolvers. A focus change is the runtime
// telling a component something, not the user expressing an intent. Resolving
// one would invent an intent nobody expressed, and would also run every
// resolver on the path each time focus moved.
func TestFocusNotificationsDoNotConsultResolvers(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	a1 := &actor{size: Size{W: 10, H: 4}}
	a2 := &actor{size: Size{W: 10, H: 4}}
	root.Add(a1, a2)

	watcher := &countingResolver{act: otherAction{}, match: true}

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[a1].ctx.SetActionResolvers(watcher)
		h.app.requestFocus(h.app.byComp[a1])
	})
	h.sync()

	// Move focus back and forth: several FocusEvents bubble through a1.
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[a2]) })
	h.sync()
	h.onLoop(func() { h.app.requestFocus(h.app.byComp[a1]) })
	h.sync()

	if got := watcher.all.Load(); got != 0 {
		t.Errorf("the resolver was consulted %d times by focus notifications, want 0", got)
	}

	// POSITIVE CONTROL: the same resolver IS consulted for real input, so the
	// zero above means "not for notifications" rather than "never".
	h.inject(keyEv('j'))
	waitFor(t, "resolver consulted for a key", func() bool { return watcher.all.Load() > 0 })
}

// TestCaptureIsRefusedWhenPointerInputIsDisabled. Only pointer events are gated
// by policy, so without this a node under a disabled ancestor could take the
// capture from a key-driven handler and then hold a gesture whose every event
// the gate discards — a drag nothing can advance and nothing can finish.
func TestCaptureIsRefusedWhenPointerInputIsDisabled(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	wrapper := &policyProbe{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 20, H: 4}}
	wrapper.Add(owner)
	root.Add(wrapper)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Disable the ANCESTOR before any capture exists, so the cancel-on-change
	// path cannot be what produces the result.
	h.onLoop(func() { h.app.byComp[wrapper].ctx.SetPointerPolicy(PointerDisabled) })
	h.sync()

	// Keys are not gated, so the handler still runs and can still ask.
	tryCaptureFromOwnHandler(h, owner)

	if got := owner.granted.Load(); got != 0 {
		t.Error("a node whose pointer input is disabled was granted the capture")
	}
	if got := owner.refused.Load(); got != 1 {
		t.Errorf("refusals = %d, want 1: if the handler never ran, this proves nothing", got)
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture is held by %d after a refused acquisition", held)
	}

	// POSITIVE CONTROL: re-enable and the same request succeeds.
	h.onLoop(func() { h.app.byComp[wrapper].ctx.SetPointerPolicy(PointerEnabled) })
	h.sync()
	tryCaptureFromOwnHandler(h, owner)
	if got := owner.granted.Load(); got != 1 {
		t.Errorf("captures granted after re-enabling = %d, want 1: the refusal above is "+
			"then not about policy", got)
	}
}

// TestPublishedNamesAreStable. An ActionID is the public matching key and the
// origin and policy names are what a trace and a test failure show, so a wrong
// string is a broken contract in the first case and a misleading diagnosis in
// the others. The default arms are included: a value added later without a name
// would otherwise render as a bare number or an empty string.
func TestPublishedNamesAreStable(t *testing.T) {
	if got := (ActivateAction{}).ActionID(); got != "control.activate" {
		t.Errorf("ActivateAction.ActionID() = %q, want %q; this string is the published "+
			"matching key and may not change once released", got, "control.activate")
	}

	origins := map[ActionOrigin]string{
		OriginProgrammatic: "programmatic",
		OriginKey:          "key",
		OriginPointer:      "pointer",
		OriginUser:         "user",
	}
	for o, want := range origins {
		if got := o.String(); got != want {
			t.Errorf("ActionOrigin(%d).String() = %q, want %q", o, got, want)
		}
	}
	if got := ActionOrigin(200).String(); got != "unknown" {
		t.Errorf("an undefined origin rendered as %q, want %q", got, "unknown")
	}

	policies := map[PointerPolicy]string{
		PointerInherit:  "inherit",
		PointerEnabled:  "enabled",
		PointerDisabled: "disabled",
	}
	for p, want := range policies {
		if got := p.String(); got != want {
			t.Errorf("PointerPolicy(%d).String() = %q, want %q", p, got, want)
		}
	}
	if got := PointerPolicy(200).String(); got != "unknown" {
		t.Errorf("an undefined policy rendered as %q, want %q", got, "unknown")
	}

	if got := TraceAction.String(); got != "action" {
		t.Errorf("TraceAction.String() = %q, want %q", got, "action")
	}
}

// TestMalformedResolverAndActionAreRejectedAtTheirSource. Both panics report a
// broken contract at the point it is broken, rather than letting a nil surface
// later inside dispatch where nothing names what produced it.
func TestMalformedResolverAndActionAreRejectedAtTheirSource(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	t.Run("nil action to DoAction is a no-op, not a panic", func(t *testing.T) {
		// Asking the runtime to dispatch nothing is a request to do nothing. A
		// caller legitimately holding a sometimes-nil action should not have to
		// write the guard the runtime is better placed to hold.
		before := ac.actions.Load()
		var got, gotTyped bool
		var fatal, fatalTyped *errs.Fatal
		h.onLoop(func() {
			fatal = fatalFrom(func() { got = h.app.byComp[ac].ctx.DoAction(nil) })
			// A TYPED nil: satisfies Action with a live type descriptor, so an
			// == nil check passes it straight through to a nil receiver.
			var typed *nilAction
			fatalTyped = fatalFrom(func() { gotTyped = h.app.byComp[ac].ctx.DoAction(typed) })
		})
		h.sync()
		if fatal != nil {
			t.Errorf("DoAction(nil) panicked (%v); the contract is return false and do nothing", fatal.Rule)
		}
		if fatalTyped != nil {
			t.Errorf("DoAction(typed nil) panicked (%v); a typed nil is still nothing to dispatch", fatalTyped.Rule)
		}
		if got || gotTyped {
			t.Errorf("DoAction reported handled for a nil action (nil=%v typed=%v)", got, gotTyped)
		}
		if after := ac.actions.Load(); after != before {
			t.Errorf("a nil action reached the handler %d time(s); it must not be dispatched", after-before)
		}
	})

	t.Run("resolver claiming a match with no action", func(t *testing.T) {
		h.onLoop(func() {
			h.app.byComp[ac].ctx.SetActionResolvers(ActionResolverFunc(
				func(ev Event) (Action, bool) {
					if _, ok := ev.(KeyEvent); ok {
						return nil, true // the contract breach
					}
					return nil, false
				}))
		})
		var fatal *errs.Fatal
		h.onLoop(func() {
			fatal = fatalFrom(func() {
				h.app.routeToNode(h.app.byComp[ac], keyEv('j'))
			})
		})
		h.sync()
		if fatal == nil {
			t.Error("a resolver returning (nil, true) did not raise errs.Fatal; the nil " +
				"would otherwise surface inside dispatch with nothing naming its source")
		}
		h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers() })
	})
}

// nilAction exists only so a TYPED nil Action can be constructed. A typed nil
// satisfies the interface with a live type descriptor, which is exactly why an
// == nil check does not catch it.
type nilAction struct{}

func (*nilAction) ActionID() ActionID { return "test.nil" }

// nilResolver is the pointer-receiver equivalent for resolvers.
type nilResolver struct{}

func (*nilResolver) Resolve(Event) (Action, bool) { return moveAction{Delta: 1}, true }

// TestTypedNilsAreRejectedEverywhereTheyCanEnter. A plain == nil check passes
// every one of these: they carry a real type descriptor and only fail when
// something calls through them, by which point the panic names a nil method
// call rather than whoever supplied it.
func TestTypedNilsAreRejectedEverywhereTheyCanEnter(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &actor{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	// Install a known-good layer first, so the atomicity check below has
	// something to preserve.
	h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 7}) })

	cases := []struct {
		name string
		bad  ActionResolver
	}{
		{"nil interface", nil},
		{"typed-nil func", ActionResolverFunc(nil)},
		{"typed-nil pointer", (*nilResolver)(nil)},
	}
	for _, tc := range cases {
		t.Run("setter rejects "+tc.name, func(t *testing.T) {
			var fatal *errs.Fatal
			h.onLoop(func() {
				fatal = fatalFrom(func() {
					h.app.byComp[ac].ctx.SetActionResolvers(moveResolver{delta: 1}, tc.bad)
				})
			})
			h.sync()
			if fatal == nil {
				t.Fatalf("a %s resolver was accepted by the setter", tc.name)
			}
			// ATOMICITY: the previous layer must survive a rejected call. A
			// setter that validated while copying would install the good entry
			// and then panic, leaving the node half-replaced.
			var chain []ActionResolver
			h.onLoop(func() { chain = h.app.byComp[ac].ctx.ActionResolvers() })
			if len(chain) != 1 {
				t.Fatalf("after a rejected call the chain holds %d resolvers, want the "+
					"1 that was there before", len(chain))
			}
			if mr, ok := chain[0].(moveResolver); !ok || mr.delta != 7 {
				t.Errorf("the surviving resolver is %#v, want the original moveResolver{7}", chain[0])
			}
		})
	}

	t.Run("a matching resolver returning a typed-nil action is fatal", func(t *testing.T) {
		h.onLoop(func() {
			h.app.byComp[ac].ctx.SetActionResolvers(ActionResolverFunc(
				func(ev Event) (Action, bool) {
					if _, ok := ev.(KeyEvent); ok {
						var typed *nilAction
						return typed, true // claims a match, supplies nothing
					}
					return nil, false
				}))
		})
		var fatal *errs.Fatal
		h.onLoop(func() {
			fatal = fatalFrom(func() { h.app.routeToNode(h.app.byComp[ac], keyEv('j')) })
		})
		h.sync()
		if fatal == nil {
			t.Error("a resolver returning a TYPED nil with ok==true was accepted; unlike " +
				"DoAction(nil), claiming a match and supplying nothing is a contract breach")
		}
		h.onLoop(func() { h.app.byComp[ac].ctx.SetActionResolvers() })
	})
}

// TestBothActivateActionFormsReachActivatable.
//
// ActivateAction.ActionID has a value receiver, so &ActivateAction{} satisfies
// Action exactly as ActivateAction{} does and reports the same id. A consumer
// resolver returning the pointer form, or a caller passing one to DoAction, is
// doing something entirely legal — so the same published action must not behave
// differently according to how it was allocated.
//
// Table-driven over BOTH producers, because the two reach dispatchAction by
// different routes and an arm added for one would not necessarily serve the
// other.
func TestBothActivateActionFormsReachActivatable(t *testing.T) {
	forms := []struct {
		name string
		act  Action
	}{
		{"value", ActivateAction{}},
		{"pointer", &ActivateAction{}},
	}

	for _, f := range forms {
		t.Run(f.name+" via a key resolver", func(t *testing.T) {
			root := &counter{size: Size{W: 20, H: 4}}
			ac := &activateOnly{size: Size{W: 20, H: 4}}
			root.Add(ac)

			h := startApp(t, root, 20, 4)
			defer h.wait()
			h.sync()
			h.onLoop(func() {
				h.app.byComp[ac].ctx.SetActionResolvers(ActionResolverFunc(
					func(ev Event) (Action, bool) {
						if _, ok := ev.(KeyEvent); ok {
							return f.act, true
						}
						return nil, false
					}))
				h.app.requestFocus(h.app.byComp[ac])
			})
			h.sync()

			h.inject(keyEv('\r'))
			waitFor(t, "activation", func() bool { return ac.activations.Load() == 1 })
			h.sync()

			if got := ac.activations.Load(); got != 1 {
				t.Errorf("activations = %d, want exactly 1", got)
			}
			if got := ac.lastOrigin(); got != OriginKey {
				t.Errorf("origin = %v, want %v", got, OriginKey)
			}
		})

		t.Run(f.name+" via DoAction", func(t *testing.T) {
			root := &counter{size: Size{W: 20, H: 4}}
			ac := &activateOnly{size: Size{W: 20, H: 4}}
			root.Add(ac)

			h := startApp(t, root, 20, 4)
			defer h.wait()
			h.sync()

			var ok bool
			h.onLoop(func() { ok = h.app.byComp[ac].ctx.DoAction(f.act) })
			h.sync()

			if !ok {
				t.Errorf("DoAction(%s form) returned false; both forms are legal Actions "+
					"and must reach the Activatable fallback", f.name)
			}
			if got := ac.activations.Load(); got != 1 {
				t.Errorf("activations = %d, want exactly 1", got)
			}
			if got := ac.lastOrigin(); got != OriginProgrammatic {
				t.Errorf("origin = %v, want %v", got, OriginProgrammatic)
			}
		})
	}
}

// TestATypedNilActivateActionIsStillRejected. Widening the switch to accept the
// pointer form must not widen it to accept a nil one: the nil-like gates at
// DoAction and at a matching resolver are what keep that out, and this pins
// that they still do.
func TestATypedNilActivateActionIsStillRejected(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	ac := &activateOnly{size: Size{W: 20, H: 4}}
	root.Add(ac)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var got bool
	var fatal *errs.Fatal
	h.onLoop(func() {
		var typed *ActivateAction
		fatal = fatalFrom(func() { got = h.app.byComp[ac].ctx.DoAction(typed) })
	})
	h.sync()

	if fatal != nil {
		t.Errorf("DoAction(typed-nil *ActivateAction) panicked (%v); a nil action is a "+
			"no-op, not a contract breach", fatal.Rule)
	}
	if got {
		t.Error("DoAction reported handled for a typed-nil action")
	}
	if n := ac.activations.Load(); n != 0 {
		t.Errorf("a typed-nil action activated the component %d time(s)", n)
	}
}
