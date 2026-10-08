package tui

import (
	"github.com/yongjohnlee80/golib/errs"
	"time"
)

// PointerPressResolver opts a container into resolving a press before its content
// receives it. Resolution is pure; the returned action runs on this component.
// The nearest matching ancestor of the hit target wins, within the active scope.
// Count previews that owner's press ordinal and is committed only on a match;
// the resolver never receives a producer-asserted click count.
type PointerPressResolver interface {
	ResolvePointerPress(MouseEvent) (Action, bool)
}

// CaptureEscapeResolver resolves bare Escape on the pointer-capture owner before
// focused content sees it. It is consulted only while capture is held. No other
// keyboard event is redirected, and resolution must not mutate component state.
type CaptureEscapeResolver interface {
	ResolveCaptureEscape(KeyEvent) (Action, bool)
}

// InInputScope reports whether this live node belongs to the active input scope.
// Visibility is separate: a hidden window can be restored through scoped input.
// Application-owned operations need not use this input-only restriction.
func (c *Context) InInputScope() bool {
	if !c.node.mounted {
		return false
	}
	scope := c.app.confinement()
	return scope == nil || withinScope(c.node, scope)
}

// Visible reports current placement and visibility through every ancestor. It
// sees a hidden ancestor immediately, before a layout resets descendant flags.
func (c *Context) Visible() bool {
	if !c.node.mounted {
		return false
	}
	for n := c.node; n != nil; n = n.parent {
		if !n.visible() {
			return false
		}
	}
	return true
}

func (a *App) interceptPointerPress(target, limit *node, e MouseEvent) bool {
	if e.Kind != MousePress {
		return false
	}
	now := time.Now()
	for n := target; n != nil; n = n.parent {
		if n.visible() && effectivePointerPolicy(n) != PointerDisabled {
			if r, ok := n.comp.(PointerPressResolver); ok {
				local := e
				local.X, local.Y = e.X-n.absRect.X, e.Y-n.absRect.Y
				local.Count = a.previewPressOrdinal(e, n, now)
				if action, matched := r.ResolvePointerPress(local); matched {
					if isNilLike(action) {
						panic(errs.Fatal{Op: "tui: PointerPressResolver", Rule: "matched press returned a nil action"})
					}
					local.Count = a.pressOrdinalAt(e, n, now)
					if !a.dispatchAction(n, ActionInvocation{Action: action, Origin: OriginPointer, Source: local}) {
						a.trace(TraceEvent{Kind: TraceAction, Node: n.id, Detail: "reserved pointer press refused"})
					}
					return true // a reservation is never replayed into content
				}
			}
		}
		if n == limit {
			break
		}
	}
	return false
}

func (a *App) interceptCaptureEscape(e KeyEvent) bool {
	if a.captureOwner == 0 || e.Kind == KeyRelease || e.Code != KeyEscape || e.Mods.Chord() != 0 {
		return false
	}
	n := a.nodes[a.captureOwner]
	if n == nil || !n.mounted || !n.visible() || effectivePointerPolicy(n) == PointerDisabled {
		return false
	}
	if scope := a.confinement(); scope != nil && !withinScope(n, scope) {
		return false
	}
	r, ok := n.comp.(CaptureEscapeResolver)
	if !ok {
		return false
	}
	action, matched := r.ResolveCaptureEscape(e)
	if !matched {
		return false
	}
	if isNilLike(action) {
		panic(errs.Fatal{Op: "tui: CaptureEscapeResolver", Rule: "matched Escape returned a nil action"})
	}
	if a.dispatchAction(n, ActionInvocation{Action: action, Origin: OriginKey, Source: e}) {
		a.traceRouted(e, n, n.id)
	} else {
		a.trace(TraceEvent{Kind: TraceAction, Node: n.id, Detail: "reserved capture Escape refused"})
		a.traceRouted(e, n, 0)
	}
	return true
}
