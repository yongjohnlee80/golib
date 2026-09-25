package widget

import "github.com/yongjohnlee80/golib/tui"

// Package widget internal capability interfaces and focus delegation protocols.
//
// # Architectural Model: Structural Discovery vs Direct Inheritance
//
// In golib/tui, components declare capabilities through interface satisfaction
// rather than class hierarchies. While public framework capabilities ([tui.Focusable],
// [tui.Container], [tui.FocusScope], [tui.CursorReporter], [tui.CursorShaper]) live in package tui,
// composite widgets within package widget coordinate focus seeding and child enumeration
// through internal capability contracts defined here:
//
//   - [childLister]: Implemented by composite widgets that manage children without implementing
//     the public [tui.Container] interface (e.g. [Split], [Tabs]).
//
// # Recursive Focus Seeding ([focusFirst])
//
// When modal dialogs ([Float]) or multi-pane views ([Split]) activate or switch tabs, focus must
// seed deterministically to the first focusable descendant in document order:
//
//	┌────────────────────────────────────────────────────────────┐
//	│ focusFirst(root)                                           │
//	└─────────────┬──────────────────────────────────────────────┘
//	              │
//	              ▼
//	Is root [tui.Focusable] && AcceptsFocus()?
//	├── YES: tui.Context.FocusComponent(root) -> SUCCESS (focus landed)
//	└── NO:
//	    ├── Does root implement [childLister]?
//	    │   └── YES: iterate ct.listChildren() -> recurse
//	    ├── Does root implement [tui.Container]?
//	    │   └── YES: iterate ct.Children() -> recurse
//	    └── Otherwise: its mounted children (tui.Context.Children) -> recurse
//
// # Architectural Invariants
//
//  1. Zoom and Active-State Respect: [childLister] implementations ([Split], [Tabs]) must only
//     return children that are currently visible and active (e.g. honoring Split zoom or active tab),
//     preventing focus from landing in hidden panes.
//  2. One Focus Path: the first focusable found is focused through the runtime
//     (tui.Context.FocusComponent), whatever type it is.
//  3. Document Order Determinism: [focusFirst] evaluates children strictly in visual/document order,
//     ensuring consistent initial keyboard focus without arbitrary jumpiness.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. Capability queries and focus delegation must run
//     on the application event loop goroutine.

// childLister lets non-Container widgets expose their children to
// focusFirst's walk (Split, Tabs).
type childLister interface {
	listChildren() []tui.Component
}

// firstFocusable is the first component of c's subtree, in document order,
// that is Focusable and accepts focus; nil for none, and for a nil c. ctx is
// any mounted widget's context: a composite that is neither a Container nor a
// childLister is walked through its mounted children (Context.Children), so a
// control inside an adapter's node — a ComboBox's select — is found where Tab
// finds it.
func firstFocusable(ctx *tui.Context, c tui.Component) tui.Component {
	if c == nil {
		return nil
	}
	if f, ok := c.(tui.Focusable); ok && f.AcceptsFocus() {
		return c
	}
	for _, ch := range childrenOf(ctx, c) {
		if f := firstFocusable(ctx, ch); f != nil {
			return f
		}
	}
	return nil
}

// childrenOf is c's children for the focus walk: a childLister's own list
// (which honours zoom and the active tab), a Container's, else the tree's.
func childrenOf(ctx *tui.Context, c tui.Component) []tui.Component {
	switch ct := c.(type) {
	case childLister:
		return ct.listChildren()
	case tui.Container:
		var out []tui.Component
		for ch := range ct.Children() {
			out = append(out, ch)
		}
		return out
	}
	if ctx == nil {
		return nil
	}
	return ctx.Children(c)
}

// focusFirst walks c's subtree in document order and focuses the first
// component that accepts focus, reporting whether one did. ctx is as for
// firstFocusable.
func focusFirst(ctx *tui.Context, c tui.Component) bool {
	f := firstFocusable(ctx, c)
	return f != nil && ctx != nil && ctx.FocusComponent(f)
}
