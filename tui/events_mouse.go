package tui

// MouseEvent is the backend-neutral pointer event. Backends normalize their
// device protocols into press, motion, release and wheel events; the App owns
// click counts, hit-testing, semantic resolution and pointer capture.
//
// Coordinates are LOCAL to the receiving component at every routing hop: each
// level rewrites them into that node's coordinate space, so a component reads
// a position relative to itself and never has to know where it sits on screen.
// Captured motion and release may therefore carry coordinates outside the
// owner's rectangle; they are deliberately not clamped, because the distance
// beyond an edge is meaningful to a drag.
//
// A backend that can no longer promise a matching release must emit
// FocusEvent{Gained: false, Terminal: true}. The App converts that into capture
// loss and sends PointerCaptureLostEvent to the owner. Components must end the
// gesture on either release or capture loss.
type MouseEvent struct {
	Kind   MouseKind
	Button MouseButton
	X, Y   int
	Mods   Mods

	// Count is the press ordinal: 1 for a single press, 2 for the second press of
	// a double-click, 3 for a triple. It is 0 on every non-press kind, so nothing
	// can read a count off motion, wheel or release and believe it.
	//
	// Producers do NOT set this. It is synthesised once in App.dispatch from
	// timing and position, because a click count is behaviour rather than decode
	// shape. A decoder that guessed at counts would have to hold timing state
	// it has no business owning, and two backends would disagree about what a
	// double-click is.
	Count int
}

func (MouseEvent) isEvent() {}

// MouseKind distinguishes press, release, motion, and wheel actions.
type MouseKind uint8

const (
	MousePress MouseKind = iota
	MouseRelease
	MouseMotion
	MouseWheel
)

// MouseButton identifies the button (or wheel direction) of a MouseEvent.
type MouseButton uint8

const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	WheelUp
	WheelDown
	WheelLeft
	WheelRight
)
