package tui

import "iter"

// This file holds the structural component capabilities. Input interpretation
// is optional too, but lives with its contracts: ActionHandler, Activatable and
// ActivationAvailability are documented in action.go; pointer capture is
// runtime-owned state exposed through Context in capture.go. Keeping those
// separate avoids presenting semantic actions as a third transport lane.

// Focusable opts a component into the framework's tab-order focus management.
// Components that do not implement Focusable are transparent to keyboard focus
// and are skipped during Tab/Shift-Tab navigation.
//
// TWO QUESTIONS, TWO ANSWERS — do not answer the first with the second:
//
//   - Is this component FOCUSABLE BY DESIGN — is it a control at all? That is
//     answered by IMPLEMENTING Focusable. A component that is never a focus
//     stop — a frame, a view that only arranges its parts, decoration — does
//     not implement it; its absence says so. (App.HoldsFocusable reads this:
//     forceActiveFocus on a subtree with nothing focusable by design is the
//     caller's mistake, and is refused.)
//   - Does it accept focus NOW? That is AcceptsFocus. It may change: a disabled
//     button is focusable by design and accepts no focus until enabled again.
//
// So the three legitimate shapes are:
//
//	no AcceptsFocus at all             never a stop (Frame, a panel's shell)
//	AcceptsFocus() bool { return true }  always a stop (Editor, TextInput)
//	AcceptsFocus() bool { return x }     a stop when x is (Button.enabled)
//
// and one shape is wrong: `AcceptsFocus() bool { return false }`, a constant
// false. It claims the capability and denies it forever, so the component
// reads as a control that is merely unavailable — and a tool asking "is
// anything in here focusable?" is told yes. The audit in internal/audit
// refuses it. When whether an INSTANCE is a control is a choice made at
// construction (a Box built WithFocusable, a Float WithModal), implement
// FocusDesigner as well, and answer that choice with FocusableByDesign.
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

// FocusDesigner is a Focusable whose focusability is chosen PER INSTANCE, at
// construction — a Box built WithFocusable, say — so implementing Focusable
// does not by itself say the instance was designed to take focus.
//
// "Focusable by design" is what a component IS, as against AcceptsFocus, which
// is what it accepts NOW: a disabled button is focusable by design and accepts
// no focus. A type that is never focusable does not implement Focusable at all
// — absence of the capability says so. A type that always may implements
// Focusable alone. Only a type where it is a per-instance choice needs this.
type FocusDesigner interface {
	Focusable
	// FocusableByDesign reports whether this instance was built to take focus.
	FocusableByDesign() bool
}

// InitialFocusProvider lets a component nominate where focus should land inside
// its own subtree, instead of accepting the runtime's document-order choice.
//
// The runtime's own repair rule is "the first focusable in document order", and
// it knows nothing about the component: a dialog's first stop is its body's
// first field, then its buttons, whatever order the card mounts them in.
// Without this seam a dialog could only get its choice honoured at the moment
// it opened, by reaching for focus itself — and would lose it again on the next
// repair, because every later repair falls back to document order.
//
// A nomination says where focus STARTS and where it goes when it has to move.
// It never takes the keyboard off a control that can still hold it: while the
// focused node is legal, a repair leaves it where it is.
//
// A nominee is VALIDATED before use, never trusted: it must be mounted,
// currently accept focus, and lie inside BOTH the provider's own subtree and the
// active focus scope. A nominee failing any check is ignored and the repair
// falls back to document order, because a provider that nominates something
// ineligible must not be able to leave focus nowhere.
//
// It is consulted ONCE per repair. The nominee is used as given and never
// re-consulted, so a nomination cannot recurse or chain into a loop.
type InitialFocusProvider interface {
	Component
	// InitialFocus returns the component focus should move to, and whether there
	// is a nomination at all. Returning (nil, true) is treated as no
	// nomination — a claim the provider cannot supply is not a nomination.
	InitialFocus() (Component, bool)
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

// FocusScope marks a component subtree as an input boundary.
//
// When TrapsFocus() reports true (e.g. inside a modal dialog or floating popup),
// the scope confines THREE things, not only traversal:
//
//   - Tab and Shift-Tab are limited to the focusable descendants of this scope.
//     A nested trapping scope is excluded from an enclosing scope's ring, so a
//     trap confines traversal both in and out.
//   - Programmatic focus is limited the same way: [Context.RequestFocus] on a
//     node outside the active scope is refused, or an outside component could
//     dissolve the confinement that a dialog exists to have.
//   - Key, paste and pointer routing are ceilinged at the scope, so an event
//     never reaches a component behind it.
//
// TWO WAYS IN, and knowing which one applies is the difference between a dialog
// that works and a dialog that paints and is inert:
//
//   - NESTED — the new scope's node lies inside the active one, as a custom
//     trapping scope mounted within a dialog's own content does. Allowed on
//     ancestry.
//   - STACKED — the new scope is a later layer of a common [FocusLayerHost],
//     as a second dialog attached to one overlay host is. Allowed because
//     the host declares its children to be layers; only the TOPMOST layer may be
//     entered. Two trapping scopes that merely sit beside each other in an
//     ordinary container are neither, and cannot reach each other at all.
//
// A dropdown opened inside a dialog is NOT automatically the nested case, and
// the shipped one is not: widget.Select projects its popup onto the overlay
// host, so it is a later LAYER of that host and takes the stacked route. Which
// route a composite takes is decided by where its popup mounts, not by how the
// screen looks.
//
// Pointer input deliberately does not raise a layer: clicking a dialog behind
// another does not move focus into it.
//
// ON THE WAY OUT, when the trapping scope is unmounted, the runtime restores
// focus to the component that held it prior to entering the trap. That restore
// is the only route back to a dialog underneath — Tab cannot reach it, because
// the trap confines traversal in both directions.
type FocusScope interface {
	Component
	// TrapsFocus reports whether this subtree is an input boundary.
	//
	// True confines three things to the subtree, not only traversal: Tab and
	// Shift-Tab; programmatic focus, so RequestFocus on a node outside it is
	// refused; and key, paste and pointer routing, which are ceilinged at the
	// scope so nothing behind it can be reached. A scope may still be entered
	// from outside by the two documented routes — nested, or a later layer of a
	// common FocusLayerHost — and focus returns to where it came from when the
	// scope unmounts.
	TrapsFocus() bool
}

// FocusLayerHost marks a component whose immediate children are STACKED LAYERS
// rather than ordinary siblings — later children are in front of earlier ones,
// on purpose and as a property of the container.
//
// It exists because focus confinement has to tell two arrangements apart that
// look identical in the tree. Two trapping scopes side by side in a [Flex] are
// unrelated: neither is "in front", and neither may take the keyboard from the
// other. Two trapping scopes on a [Stack] are layers: the later one is the
// dialog the user is looking at, and focus must be able to reach it. Document
// order alone cannot distinguish those, and treating it as if it could lets
// declaration order decide which unrelated panel may steal focus.
//
// Implement it only where later-is-in-front is genuinely what the container
// means. [Stack] does, and so does anything embedding it, such as
// widget.OverlayHost.
//
// It governs programmatic focus only. Pointer input deliberately does not raise
// a layer: clicking a dialog that sits behind another must not pull the keyboard
// out of the one in front.
type FocusLayerHost interface {
	Component
	// HostsFocusLayers reports whether this component's immediate children are
	// stacked layers, later in front of earlier.
	HostsFocusLayers() bool
}
