package tui

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
