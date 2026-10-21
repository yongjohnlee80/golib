package tui

import (
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// PointerPressResolver opts a container into resolving a press before its content
// receives it. Resolution is pure; the returned action runs on this component.
// The nearest matching ancestor of the hit target wins, within the active scope.
// Count previews that owner's press ordinal and is committed only on a match;
// the resolver never receives a producer-asserted click count.
type PointerPressResolver interface {
	ResolvePointerPress(MouseEvent) (Action, bool)
}

// CaptureEscapeResolver resolves the cancel key (the tui.CancelCapture command, Esc by default) on
// the pointer-capture owner before focused content sees it. It is consulted only while capture is
// held, and only for an event the runtime matched as that command, so a resolver does not test the
// key itself: a rebound cancel key reaches it too. No other keyboard event is redirected, and
// resolution must not mutate component state.
type CaptureEscapeResolver interface {
	ResolveCaptureEscape(KeyEvent) (Action, bool)
}

// InInputScope reports whether this live node belongs to the active input scope: inside it, or in
// a layer whose input owner is (SetChildInputOwner).
// Visibility is separate: a hidden window can be restored through scoped input.
// Application-owned operations need not use this input-only restriction.
func (c *Context) InInputScope() bool {
	if !c.node.mounted {
		return false
	}
	return c.app.inInputScope(c.node, c.app.confinement())
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
	for n := range pathTo(target, limit) {
		if n.visible() && effectivePointerPolicy(n) != PointerDisabled {
			if r, ok := n.comp.(PointerPressResolver); ok {
				local := e.localTo(n.absRect)
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
	}
	return false
}

func (a *App) interceptCaptureEscape(e KeyEvent) bool {
	if a.captureOwner == 0 {
		return false
	}
	if _, ok := keyCommand(a.cfg.keymap, e, []keymap.Command{CancelCapture}); !ok {
		return false
	}
	n := a.nodes[a.captureOwner]
	if n == nil || !n.mounted || !n.visible() || effectivePointerPolicy(n) == PointerDisabled {
		return false
	}
	if scope := a.confinement(); scope != nil && !a.inInputScope(n, scope) {
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
