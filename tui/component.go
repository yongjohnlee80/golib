package tui

import "iter"

// Component is the single mandatory contract of every node in the tree
// (ADR-0001 §2.3, ADR-0004 §2.1). All four methods are invoked ONLY on the
// App loop goroutine (ADR-0005 §2.3).
//
// IDENTITY CONTRACT (ADR-0004 §2.4, rev 1): a Component's dynamic type MUST
// be comparable — normatively, a component is a POINTER to its state struct.
// Mount panics on non-comparable component types.
type Component interface {
	// Init is called exactly once, at mount, before any
	// Layout/Render/HandleEvent. ctx is valid for the component's whole
	// mounted lifetime; retain it. Mounting children (ctx.Mount) and
	// subscribing (SubscribeScoped, ADR-0005 §2.7) are legal here. A
	// component mounted again later is a NEW mount (new NodeID, fresh
	// Init) — authors must make Init re-entrant across remounts of the
	// same Go value.
	Init(ctx *Context)

	// Layout receives constraints from the parent and returns the size
	// the component chooses within them (ADR-0004 §2.7). Containers lay
	// out and place their children here via
	// ctx.LayoutChild/ctx.PlaceChild — the ONLY window in which those
	// calls are legal. Layout must not mutate tree structure (no
	// Mount/Unmount) and must not post events.
	Layout(c Constraints) Size

	// Render paints the component's own chrome into s — a Surface
	// pre-clipped to the Rect the parent placed it in (ADR-0003). It must
	// NOT render children: the framework walks the tree and hands each
	// child its own sub-Surface (ADR-0004 §2.4). Render must be
	// effect-free besides painting.
	Render(s Surface)

	// HandleEvent receives routed events (ADR-0004 §2.5). Return true to
	// consume: bubbling stops. Handlers run on the loop goroutine and may
	// freely mutate component state, call ctx.MarkDirty()/RequestLayout(),
	// mount/unmount children, post events, and start tasks. A handler
	// that blocks stalls the UI (ADR-0005 §3); long work goes through
	// ctx.Go.
	HandleEvent(ev Event) bool
}

// Focusable opts a component into the focus system (ADR-0004 §2.6).
// Components that are not Focusable are skipped by traversal and can never
// hold focus. Focus notification is event-only: FocusEvent through
// HandleEvent — there are no Focus()/Blur() methods (ADR-0004 §2.6.1).
type Focusable interface {
	Component
	// AcceptsFocus reports whether the component can take focus right
	// now; false = temporarily unfocusable (e.g. disabled input).
	AcceptsFocus() bool
}

// Container is the public child-management surface for composite widgets
// (Flex, Dock, Stack, and ADR-0007's widget set). The framework's own
// parent/child links are built by Context.Mount/Unmount (ADR-0004 §2.4);
// Container is the user-facing mutation API layered on top of them, plus
// enumeration for traversal-order documentation, devtools, and tests. It is
// a capability interface in the golib small-seam style (archetype
// logger.Logger; cf. server.Drainer's type-asserted upgrade,
// server/registry.go:12-19).
type Container interface {
	Component
	// Add mounts children immediately if the container is mounted;
	// otherwise mounting is deferred to the container's Init.
	Add(children ...Component)
	// Remove unmounts child (cascade, ADR-0004 §2.4) and forgets it.
	Remove(child Component)
	// Children enumerates in document order == focus order == paint order.
	Children() iter.Seq[Component]
}

// CursorReporter implements the IME real-cursor rule (ADR-0004 G6). When the
// focused component reports ok=true, the runtime parks the REAL terminal
// cursor at the reported position, translated to absolute coordinates
// through the node's laid-out Rect chain, and shows it. ok=false or no
// implementation → hardware cursor hidden. Rationale: OS IMEs anchor the
// composition window to the hardware cursor — a fake drawn cursor puts CJK
// candidate windows at (0,0)
// (https://github.com/xtermjs/xterm.js/issues/5734). Also aids screen
// readers.
type CursorReporter interface {
	Component
	// Cursor reports the insertion point in local (Surface) coordinates.
	Cursor() (x, y int, ok bool)
}

// FocusScope marks a subtree as a traversal boundary (ADR-0004 §2.6.3). A
// trapping scope (modal, floating window) confines Tab/Shift-Tab to its
// subtree; when it unmounts, focus restores to the previously focused node.
type FocusScope interface {
	Component
	TrapsFocus() bool
}
