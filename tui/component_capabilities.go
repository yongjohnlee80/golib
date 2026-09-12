package tui

import "iter"

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
