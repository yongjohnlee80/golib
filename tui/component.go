package tui

import "iter"

// Keyer is an optional capability interface discovered at runtime via type assertion
// on the outer component value (similar to Focusable).
//
// Keyed containers (such as dynamic lists, table rows, and tab bars) consult Keyer
// to preserve a component across element reordering: matching keys preserve the
// existing mount, allowing repositioning via Container.Move rather than a disruptive
// Remove+Add cycle. As a result, the component retains its NodeID, lifetime Context,
// in-flight background tasks, timer registrations, and active focus state without
// re-running Init.
//
// IDENTITY CONTRACT: The returned key MUST be non-nil, comparable (as it serves as
// an internal map key), and stable across the component's mounted lifetime. A component
// that does not implement Keyer relies on positional identity (its index in the container).
type Keyer interface {
	// Key returns this component's stable identity within a keyed container.
	Key() any
}

// Component is the foundational, mandatory contract of every node in the UI tree.
//
// # Architectural Invariants
//
// All four Component methods execute SOLELY on the App loop goroutine. Because of this
// single-owner guarantee, component structs can hold plain fields, buffers, and state
// pointers without mutexes or atomic primitives. A component author may freely assume
// that no other goroutine is inspecting or mutating the tree while these methods run.
//
// Dynamic Type Comparability: A Component's dynamic Go type MUST be comparable (normatively,
// a component is implemented as a pointer to its state struct, e.g. *MyWidget).
// App.Mount panics if passed a non-comparable component type, as internal tree maps
// (nodes, byComp) key directly off the Component interface value.
type Component interface {
	// Init is called exactly once when the component is mounted into the tree,
	// before any Layout, Render, or HandleEvent calls.
	//
	// The provided Context is valid for the component's entire mounted lifetime;
	// components should retain it in their struct. Registering child components
	// (ctx.Mount), starting timers (ctx.After, ctx.Every), and setting up scoped
	// bus subscriptions (SubscribeScoped) are all legal here.
	//
	// If a component value is unmounted and later remounted elsewhere in the tree,
	// it represents a NEW mount: it receives a fresh NodeID, a new Context, and
	// a fresh Init call. Component authors must design Init to be cleanly re-entrant
	// across successive mounts.
	Init(ctx *Context)

	// Layout receives dimensional constraints from the parent and returns the chosen
	// Size within those bounds (the Flutter box protocol).
	//
	// Sizing Rules:
	//   - The returned Size must satisfy c.MinW <= W <= c.MaxW and c.MinH <= H <= c.MaxH.
	//   - If a component returns a Size outside these bounds, the framework clamps it
	//     and records a ConstraintViolation so that misbehaving widgets cannot break
	//     sibling geometry.
	//   - Containers lay out and place their children here via ctx.LayoutChild and
	//     ctx.PlaceChild. Layout is the ONLY window during which those calls are legal.
	//   - Layout must remain purely geometric: it must NOT mutate tree structure (no
	//     Mount or Unmount) and must NOT post events or mutate application state.
	Layout(c Constraints) Size

	// Render paints the component's visual appearance into the provided Surface.
	//
	// The Surface is pre-clipped to the Rect assigned by the parent during layout,
	// with surface-local (0,0) translated to the component's top-left corner.
	//
	// Delegation Rules:
	//   - Render must only paint the component's OWN visual chrome (borders, text, backgrounds).
	//   - It must NOT render children: the framework automatically traverses the tree
	//     depth-first in document order, creating pre-clipped sub-surfaces for each child.
	//   - Render must be side-effect free outside of painting cells onto the Surface.
	Render(s Surface)

	// HandleEvent receives routed input and program events.
	//
	// Return Values & Event Bubbling:
	//   - Return true to mark the event as consumed, terminating bubbling up the parent chain.
	//   - Return false to allow the event to continue bubbling to parent containers.
	//
	// Execution Environment:
	//   Handlers execute on the loop goroutine and may freely mutate component state,
	//   mark dirty regions (ctx.MarkDirty), request geometry re-layout (ctx.RequestLayout),
	//   mount/unmount child nodes, publish bus events, and dispatch background tasks (ctx.Go).
	//   Handlers must NEVER block; long-running I/O or computations must be offloaded to ctx.Go.
	HandleEvent(ev Event) bool
}

// Focusable opts a component into the framework's tab-order focus management.
// Components that do not implement Focusable are transparent to keyboard focus
// and are skipped during Tab/Shift-Tab navigation.
//
// Event-Driven Focus Notifications:
// Focus transitions are delivered exclusively as FocusEvents through HandleEvent
// (with FocusEvent.Gained set to true or false). There are no synchronous Focus()
// or Blur() callbacks.
type Focusable interface {
	Component
	// AcceptsFocus reports whether the component can accept keyboard focus right
	// now. Returning false temporarily removes the component from tab traversal
	// (e.g. for disabled buttons or read-only form fields).
	AcceptsFocus() bool
}

// Container is the public child-management contract for composite layout widgets
// (such as Flex, Dock, Stack, and composite widget panels).
//
// The framework's internal parent-child tree links are constructed via Context.Mount
// and Context.Unmount. Container represents the caller-facing mutation API layered
// on top of those primitives, providing deterministic child ordering.
type Container interface {
	Component
	// Add mounts child components immediately if the container is already mounted;
	// otherwise, child mounting is deferred until the container's own Init executes.
	Add(children ...Component)

	// Remove unmounts the child (triggering a full unmount cascade on its subtree)
	// and drops all internal references to it.
	Remove(child Component)

	// Move relocates child to index 'to' in document order ("the child ends up
	// at index to") WITHOUT unmounting it: NodeID, lifetime Context, in-flight tasks,
	// timers, hooks, and active focus all survive intact; Init is not re-run.
	// Move supports sibling reordering only; moving across different containers
	// remains a Remove+Add operation (representing a fresh mount).
	Move(child Component, to int)

	// Children enumerates mounted children in document order (which defines focus
	// tab order and painter's z-order).
	Children() iter.Seq[Component]
}

// CursorReporter implements the real hardware terminal cursor positioning rule.
//
// When the currently focused component implements CursorReporter and reports ok=true,
// the runtime translates the local surface coordinates through the laid-out Rect
// ancestor chain to screen-absolute coordinates and activates the terminal cursor.
// If the focused component returns ok=false or does not implement CursorReporter,
// the hardware cursor is hidden.
//
// Rationale & IME Support:
// Modern operating systems anchor Input Method Editor (IME) composition windows
// (essential for CJK, Vietnamese, and accented characters) directly to the physical
// terminal hardware cursor. Drawing a "fake" cursor glyph leaves the OS IME candidate
// popup anchored incorrectly at (0,0). CursorReporter ensures IME candidates appear
// right above or below the active editing cell.
type CursorReporter interface {
	Component
	// Cursor reports the active text insertion point in local (Surface) coordinates.
	// Returning ok=false indicates the cursor should be hidden this frame.
	Cursor() (x, y int, ok bool)
}

// CursorShaper is an optional capability interface implemented by components whose
// hardware cursor shape depends on internal editing state (for example, a modal Vim
// editor reporting CursorShapeBlock in Normal mode and CursorShapeBar in Insert mode).
//
// Consulted only for the focused CursorReporter whose Cursor() is currently active;
// on every other frame path, cursor shape is restored to the terminal default.
type CursorShaper interface {
	CursorShape() CursorShape
}

// FocusScope marks a component subtree as a keyboard traversal boundary.
//
// When TrapsFocus() reports true (e.g. inside a modal dialog or floating popup),
// Tab and Shift-Tab navigation is strictly confined to the focusable descendants
// of this scope. When the trapping scope is unmounted, the runtime automatically
// restores focus to the component that held it prior to entering the trap.
type FocusScope interface {
	Component
	// TrapsFocus reports whether keyboard tab navigation is confined to this subtree.
	TrapsFocus() bool
}
