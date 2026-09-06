package widget

import "github.com/yongjohnlee80/golib/tui"

// Base provides NodeID/Context plumbing, dirty-marking, and default no-op
// event behavior. Widgets embed it by value.
//
// What Go embedding gives us: method promotion (a widget that doesn't
// override HandleEvent satisfies the event half of tui.Component through
// Base's), shared state plumbing written once, and zero-cost composition
// (embedded by value, no indirection).
//
// What it does NOT give us — documented so nobody designs against
// inheritance that isn't there: there is no virtual dispatch. If Base.Init
// called a method also defined on the outer widget, the Base version would
// run — the embedded struct never sees the outer type. Consequences baked
// into the contract:
//
//   - Base never calls "overridable" methods. Template-method patterns are
//     forbidden; the runtime calls the OUTER component's interface
//     methods directly, so overriding works at the interface boundary — the
//     only boundary Go respects.
//   - Widgets that override Init MUST call b.Base.Init(ctx) first (the
//     compiler can't enforce it; the widget test suite catches a missed
//     chain via nil-Context panics on TestBackend runs — ADR-0007 §5.2).
//   - Capability interfaces (tui.Focusable, tui.Container, tui.FocusScope,
//     tui.CursorReporter) are detected by type assertion on the outer type;
//     Base deliberately implements none of them, so embedding never
//     accidentally advertises a capability.
//
// Base supplies no Layout or Render: there is no sensible default; every
// widget implements both.
type Base struct {
	ctx *tui.Context // set by Init; carries NodeID, App handles, unmount context
}

// Init stores the mount context. Widgets overriding Init must chain to it
// first. Re-entrant across remounts of the same widget value.
func (b *Base) Init(ctx *tui.Context) { b.ctx = ctx }

// Context returns the mount context (nil before the first mount).
func (b *Base) Context() *tui.Context { return b.ctx }

// NodeID returns the widget's node identity for this mount (Context.ID); 0
// before the first mount.
func (b *Base) NodeID() tui.NodeID {
	if b.ctx == nil {
		return 0
	}
	return b.ctx.ID()
}

// MarkDirty requests a repaint. Safe to call before mount (no-op): widget
// setters may run at construction time, before Init.
func (b *Base) MarkDirty() {
	if b.ctx != nil {
		b.ctx.MarkDirty()
	}
}

// RequestLayout signals the widget's size may have changed.
// Safe to call before mount (no-op).
func (b *Base) RequestLayout() {
	if b.ctx != nil {
		b.ctx.RequestLayout()
	}
}

// HandleEvent is the default no-op handler: nothing consumed, bubbling
// continues. Widgets override what they need.
func (b *Base) HandleEvent(ev tui.Event) bool { return false }

// measure is the policy-aware text width every widget MUST use for layout,
// cursor, scroll, wrap-recount, and hit-test math OUTSIDE Render — it routes
// through Context.StringWidth, so the App's width policy (WithWidthPolicy)
// governs geometry exactly as Surface.StringWidth governs
// paint. Before the first mount (ctx nil, e.g. a setter measuring at
// construction) it falls back to the default policy, matching a Surface
// under WidthPolicyDefault.
func (b *Base) measure(s string) int {
	if b.ctx != nil {
		return b.ctx.StringWidth(s)
	}
	return tui.StringWidth(s)
}

// --- unexported plumbing shared by the package's widgets ---

// publish enqueues v on the App bus (enqueue-only;). No-op
// before mount.
func (b *Base) publish(v any) {
	if b.ctx != nil {
		b.ctx.Bus().Publish(v)
	}
}

// focused reports whether this widget's node currently holds focus.
func (b *Base) focused() bool { return b.ctx != nil && b.ctx.Focused() }

// focusSelf asks the focus manager to focus this widget and reports whether
// it took focus. It is the hook behind focusFirst: because Base carries the
// OUTER widget's Context, the promoted method requests focus for the
// embedding widget's node.
func (b *Base) focusSelf() bool {
	if b.ctx == nil {
		return false
	}
	b.ctx.RequestFocus()
	return b.ctx.Focused()
}

// selfFocuser is satisfied (by promotion) by every widget embedding Base.
type selfFocuser interface{ focusSelf() bool }

// childLister lets non-Container widgets expose their children to
// focusFirst's walk (Split, Tabs).
type childLister interface{ listChildren() []tui.Component }

// focusFirst walks c's subtree in document order and focuses the first
// package widget that is Focusable and accepts focus. Used by Float to seed
// focus into a freshly shown modal. Returns whether focus
// landed.
func focusFirst(c tui.Component) bool {
	if f, ok := c.(tui.Focusable); ok && f.AcceptsFocus() {
		if sf, ok := c.(selfFocuser); ok && sf.focusSelf() {
			return true
		}
	}
	switch ct := c.(type) {
	case tui.Container:
		for ch := range ct.Children() {
			if focusFirst(ch) {
				return true
			}
		}
	case childLister:
		for _, ch := range ct.listChildren() {
			if focusFirst(ch) {
				return true
			}
		}
	}
	return false
}

// boundedMax resolves a constraint axis for greedy widgets: the max when
// bounded, else the min (a greedy widget asked for its intrinsic extent on
// an unbounded axis must not answer Unbounded).
func boundedMax(maxV, minV int) int {
	if maxV == tui.Unbounded {
		return minV
	}
	return maxV
}

// subFrame subtracts a frame size from a constraint max, preserving
// Unbounded.
func subFrame(maxV, frame int) int {
	if maxV == tui.Unbounded {
		return tui.Unbounded
	}
	return max(maxV-frame, 0)
}
