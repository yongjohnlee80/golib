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
//   - [selfFocuser]: Promoted via [Base], allows a widget to request focus using its outer [tui.Context].
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
//	├── YES: Does it satisfy [selfFocuser]?
//	│        └── YES: sf.focusSelf() -> SUCCESS (focus landed)
//	└── NO:
//	    ├── Does root implement [tui.Container]?
//	    │   └── YES: iterate ct.Children() -> recurse focusFirst(child)
//	    └── Does root implement [childLister]?
//	        └── YES: iterate ct.listChildren() -> recurse focusFirst(child)
//
// # Architectural Invariants
//
//  1. Zoom and Active-State Respect: [childLister] implementations ([Split], [Tabs]) must only
//     return children that are currently visible and active (e.g. honoring Split zoom or active tab),
//     preventing focus from landing in hidden panes.
//  2. Promoted Focus Dispatch: Any widget embedding [Base] automatically satisfies [selfFocuser]
//     via method promotion on [Base.focusSelf].
//  3. Document Order Determinism: [focusFirst] evaluates children strictly in visual/document order,
//     ensuring consistent initial keyboard focus without arbitrary jumpiness.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. Capability queries and focus delegation must run
//     on the application event loop goroutine.

// selfFocuser is satisfied (by promotion) by every widget embedding Base.
type selfFocuser interface {
	focusSelf() bool
}

// childLister lets non-Container widgets expose their children to
// focusFirst's walk (Split, Tabs).
type childLister interface {
	listChildren() []tui.Component
}

// firstFocusable is the first component of c's subtree, in document order,
// that is Focusable and accepts focus; nil for none, and for a nil c.
func firstFocusable(c tui.Component) tui.Component {
	if f, ok := c.(tui.Focusable); ok && f.AcceptsFocus() {
		return c
	}
	switch ct := c.(type) {
	case tui.Container:
		for ch := range ct.Children() {
			if f := firstFocusable(ch); f != nil {
				return f
			}
		}
	case childLister:
		for _, ch := range ct.listChildren() {
			if f := firstFocusable(ch); f != nil {
				return f
			}
		}
	}
	return nil
}

// focusFirst walks c's subtree in document order and focuses the first
// package widget that is Focusable and accepts focus. Used by Float to seed
// focus into a freshly shown modal, and by Split to restore focus after zoom changes.
// Returns whether focus landed.
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
