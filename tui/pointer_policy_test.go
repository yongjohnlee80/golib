package tui

import (
	"sync/atomic"
	"testing"
)

// "This widget does not take mouse input" is easy to implement as a half
// measure: gate one path and leave the other open, or check the flag on the
// node the consumer set it on and never reach it in time. These tests target
// both halves and the inheritance that makes the check reachable at all.

// policyProbe counts raw pointer events and resolved actions separately, so a
// gate that closes only one path is visible rather than merely quieter.
type policyProbe struct {
	MultiChild
	size    Size
	raws    atomic.Int64
	actions atomic.Int64
	keys    atomic.Int64
}

func (p *policyProbe) Layout(cs Constraints) Size {
	if p.ctx != nil {
		for _, ch := range p.All() {
			sz := p.ctx.LayoutChild(ch, Loose(Size{W: cs.MaxW, H: cs.MaxH}))
			p.ctx.PlaceChild(ch, Rect{W: sz.W, H: sz.H})
		}
	}
	return cs.Constrain(p.size)
}
func (p *policyProbe) Render(Surface)     {}
func (p *policyProbe) AcceptsFocus() bool { return true }

func (p *policyProbe) HandleEvent(ev Event) bool {
	switch ev.(type) {
	case MouseEvent:
		p.raws.Add(1)
	case KeyEvent:
		p.keys.Add(1)
	}
	return false
}

func (p *policyProbe) HandleAction(inv ActionInvocation) bool {
	p.actions.Add(1)
	return true
}

// anyPointerResolver resolves any press, so the semantic path is live whenever
// the policy gate lets an event through.
type anyPointerResolver struct{}

func (anyPointerResolver) Resolve(ev Event) (Action, bool) {
	if e, ok := ev.(MouseEvent); ok && e.Kind == MousePress {
		return moveAction{Delta: 1}, true
	}
	return nil, false
}

// TestPointerDisabledGatesBothPathsAtOnce. Gating only raw delivery would leave
// a resolver-driven widget fully interactive with the mouse "off"; gating only
// the semantic path would leave a raw-reading one the same way.
func TestPointerDisabledGatesBothPathsAtOnce(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	p := &policyProbe{size: Size{W: 20, H: 4}}
	root.Add(p)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		c := h.app.byComp[p].ctx
		c.SetActionResolvers(anyPointerResolver{})
		h.app.requestFocus(h.app.byComp[p])
	})
	h.sync()

	// Positive control FIRST: with the mouse enabled, both paths are live, so a
	// later zero means the gate closed them rather than that they never worked.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "enabled press resolved", func() bool { return p.actions.Load() == 1 })
	h.sync()

	h.onLoop(func() { h.app.byComp[p].ctx.SetPointerPolicy(PointerDisabled) })
	h.sync()

	rawsBefore, actionsBefore := p.raws.Load(), p.actions.Load()
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	h.inject(MouseEvent{Kind: MouseMotion, X: 3, Y: 1})

	// A key sentinel proves the events above were processed; "nothing arrived"
	// is otherwise equally consistent with nothing having been dispatched.
	h.inject(keyEv('j'))
	waitFor(t, "key sentinel delivered", func() bool { return p.keys.Load() == 1 })
	h.sync()

	if got := p.raws.Load() - rawsBefore; got != 0 {
		t.Errorf("a disabled node received %d raw pointer events, want 0", got)
	}
	if got := p.actions.Load() - actionsBefore; got != 0 {
		t.Errorf("a disabled node received %d pointer-derived actions, want 0: the gate "+
			"is applied before BOTH paths", got)
	}
	if got := p.keys.Load(); got != 1 {
		t.Errorf("keys delivered = %d, want 1: pointer policy must not silence the "+
			"keyboard", got)
	}
}

// TestPointerPolicyIsInherited is the structural half. A composite widget's
// interactive parts are its own children, and children are hit-tested FIRST, so
// a policy consulted only on the node it was set on is one the runtime never
// reaches — the child would resolve and capture under its own default before
// anything asked the wrapper whose mouse was just turned off.
func TestPointerPolicyIsInherited(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	wrapper := &policyProbe{size: Size{W: 20, H: 4}}
	handle := &policyProbe{size: Size{W: 20, H: 4}} // hit-tested before its parent
	wrapper.Add(handle)
	root.Add(wrapper)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	h.onLoop(func() {
		h.app.byComp[handle].ctx.SetActionResolvers(anyPointerResolver{})
		h.app.requestFocus(h.app.byComp[handle])
	})
	h.sync()

	// Control: the child is interactive before the parent is disabled.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "child press resolved", func() bool { return handle.actions.Load() == 1 })
	h.sync()

	// Disable the PARENT only.
	h.onLoop(func() { h.app.byComp[wrapper].ctx.SetPointerPolicy(PointerDisabled) })
	h.sync()

	var eff PointerPolicy
	h.onLoop(func() { eff = h.app.byComp[handle].ctx.EffectivePointerPolicy() })
	if eff != PointerDisabled {
		t.Errorf("the child's effective policy is %v, want %v: policy is inherited", eff, PointerDisabled)
	}

	before := handle.actions.Load() + handle.raws.Load()
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1})
	h.inject(keyEv('j'))
	waitFor(t, "key sentinel delivered", func() bool { return handle.keys.Load() == 1 })
	h.sync()

	if got := handle.actions.Load() + handle.raws.Load() - before; got != 0 {
		t.Errorf("the child of a disabled parent received %d pointer deliveries, want 0", got)
	}
}

// TestEffectivePolicyDefaultsToEnabledAndNeverReturnsInherit. A tree that has
// said nothing about the mouse takes the mouse, which is what every component
// written before policy existed already assumes.
func TestEffectivePolicyDefaultsToEnabledAndNeverReturnsInherit(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	p := &policyProbe{size: Size{W: 20, H: 4}}
	root.Add(p)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var own, eff PointerPolicy
	h.onLoop(func() {
		own = h.app.byComp[p].pointerPolicy
		eff = h.app.byComp[p].ctx.EffectivePointerPolicy()
	})
	if own != PointerInherit {
		t.Errorf("a node's own default policy = %v, want %v", own, PointerInherit)
	}
	if eff != PointerEnabled {
		t.Errorf("effective policy with nothing set = %v, want %v", eff, PointerEnabled)
	}

	// An explicit Enabled under a Disabled ancestor wins for its own subtree,
	// which is what makes the policy a decision rather than a one-way switch.
	child := &policyProbe{size: Size{W: 10, H: 2}}
	h.onLoop(func() {
		p.Add(child)
	})
	h.sync()
	h.onLoop(func() {
		h.app.byComp[p].ctx.SetPointerPolicy(PointerDisabled)
		h.app.byComp[child].ctx.SetPointerPolicy(PointerEnabled)
	})
	var childEff PointerPolicy
	h.onLoop(func() { childEff = h.app.byComp[child].ctx.EffectivePointerPolicy() })
	if childEff != PointerEnabled {
		t.Errorf("a child that states Enabled under a Disabled parent has effective "+
			"policy %v, want %v", childEff, PointerEnabled)
	}
}

// TestDisablingDuringACaptureCancelsIt. Otherwise the owner keeps a gesture
// that no further pointer event is allowed to finish, because the events that
// would finish it are exactly the ones now being refused.
func TestDisablingDuringACaptureCancelsIt(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 20, H: 4}}
	root.Add(owner)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	startDrag(t, h, owner, 2, 1)

	h.onLoop(func() { h.app.byComp[owner].ctx.SetPointerPolicy(PointerDisabled) })
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := CaptureLostReason(owner.lastReason.Load()); got != CaptureLostCancelled {
		t.Errorf("loss reason = %v, want %v: from the owner's side a policy change is "+
			"the program deciding the gesture is over", got, CaptureLostCancelled)
	}
	var held NodeID
	h.onLoop(func() { held = h.app.captureOwner })
	if held != 0 {
		t.Errorf("capture still held by %d after the mouse was disabled", held)
	}
}

// TestDisablingAnAncestorDuringACaptureCancelsIt. The same rule has to hold
// through inheritance, which is the case a per-node check would miss — and the
// realistic one, since the capture owner is usually a child of the widget whose
// mouse a consumer turns off.
func TestDisablingAnAncestorDuringACaptureCancelsIt(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	wrapper := &policyProbe{size: Size{W: 20, H: 4}}
	owner := &dragger{size: Size{W: 20, H: 4}}
	wrapper.Add(owner)
	root.Add(wrapper)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()
	startDrag(t, h, owner, 2, 1)

	h.onLoop(func() { h.app.byComp[wrapper].ctx.SetPointerPolicy(PointerDisabled) })
	waitFor(t, "loss delivered", func() bool { return owner.losses.Load() == 1 })
	h.sync()

	if got := CaptureLostReason(owner.lastReason.Load()); got != CaptureLostCancelled {
		t.Errorf("loss reason = %v, want %v", got, CaptureLostCancelled)
	}
}
