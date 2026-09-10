package widget

import "github.com/yongjohnlee80/golib/tui"

// Base provides NodeID and Context plumbing, dirty-marking, layout invalidation,
// and default event behavior for all components in package widget. Widgets embed it
// by value.
//
// # Architectural Model: Embedding vs Virtual Dispatch
//
// Widgets embed Base by value:
//
//	type MyWidget struct {
//	    widget.Base
//	    // ... widget-specific fields ...
//	}
//
// What Go struct embedding provides:
//  1. Method Promotion: Methods on Base ([Base.Context], [Base.NodeID], [Base.MarkDirty],
//     [Base.RequestLayout], and the default [Base.HandleEvent]) are promoted directly onto
//     the outer struct. A widget that does not override HandleEvent automatically satisfies
//     the event handling portion of [tui.Component].
//  2. Zero-Cost Composition: Embedding by value incurs zero pointer indirection and keeps
//     widget memory flat and cache-friendly.
//  3. Shared Runtime Plumbing: Context storage and node identity are implemented once rather
//     than re-implemented across dozens of components.
//
// What Go struct embedding does NOT provide (and why it matters):
// Go embedding is NOT object-oriented inheritance. There is NO virtual dispatch:
//
//	   ┌────────────────────────────────────────────────────────────┐
//	   │                        tui.Runtime                         │
//	   └─────────────────────────────┬──────────────────────────────┘
//	                                 │ Calls interface methods
//	                                 │ (Component, Focusable, etc.)
//	                                 ▼
//	                  ┌──────────────────────────────┐
//	                  │       Outer Component        │
//	                  │         (*TextInput)         │
//	                  │                              │
//	                  │  - Init(ctx)                 │
//	                  │  - Layout(c)                 │
//	                  │  - Render(s)                 │
//	                  │  - HandleEvent(ev)           │
//	                  │  - AcceptsFocus()            │
//	                  │                              │
//	                  │   ┌──────────────────────┐   │
//	                  │   │    Embedded Base     │   │
//	                  │   │                      │   │
//	                  │   │  - ctx *Context      │   │
//	                  │   │  - MarkDirty()       │   │
//	                  │   │  - RequestLayout()   │   │
//	                  │   └──────────────────────┘   │
//	                  └──────────────────────────────┘
//
// If Base were to call a method also defined on the outer widget, the Base version would run
// because the embedded struct has no pointer to or knowledge of the outer type.
// From this invariant, three binding rules govern the package contract:
//
//   - No Template Methods: Base never calls "overridable" methods. Template-method patterns
//     are forbidden; the runtime calls the OUTER component's interface methods directly, so
//     polymorphism functions at the interface boundary—the only boundary Go respects.
//   - Mandatory Chaining on Init: Any widget that overrides Init MUST call b.Base.Init(ctx)
//     first. The compiler cannot enforce this chain, but omitting it leaves b.ctx nil,
//     causing immediate panics in unit test suites.
//   - Capability Cleanliness: Base deliberately implements NO capability interfaces
//     ([tui.Focusable], [tui.Container], [tui.FocusScope], [tui.CursorReporter],
//     [tui.CursorShaper]). The runtime discovers capabilities via type assertions on the
//     outer type; if Base implemented them with dummy no-ops, every embedding widget would
//     accidentally advertise capabilities it does not support.
//
// Base supplies no Layout or Render: there is no sensible default geometry or paint routine;
// every concrete widget must implement both.
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
