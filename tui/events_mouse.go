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

	// SubX and SubY are the pointer's position inside cell (X, Y), each in [0, 1): where in the
	// cell, for a component that draws in pixels (a native view). Zero from a backend that knows
	// only whole cells. Routing translates X and Y to each node; these need no translation, so
	// with App.CellPixels a component recovers its own pixels: (X + SubX) * CellPixels().W.
	SubX, SubY float32

	// Source is a PointerResolver backend's own record of where the event happened. With
	// Source.Ref set, X, Y, SubX and SubY are filled in when the App dispatches the event (see
	// PointerResolver), and Source is cleared then. Every other producer leaves it zero.
	Source PointerSource
}

// localTo is e with its position stated in r's frame: a screen-absolute event made local to the
// node whose absolute rectangle r is, as that node's component receives it.
func (e MouseEvent) localTo(r Rect) MouseEvent {
	e.X, e.Y = e.X-r.X, e.Y-r.Y
	return e
}

// PointerSource is a backend's record of a pointer event's position, carried inside the event so
// it lives exactly as long as the event does: coalesced, dropped or dispatched, nothing else
// holds it. tui never reads it; it hands it back to the backend's ResolvePointer.
type PointerSource struct {
	// X and Y are the position in the backend's own units (a native window's device pixels).
	X, Y float32
	// Ref is the backend's own value for the picture the user saw (a native window's frame). It
	// must be comparable, a pointer, since events are compared as interface values. Non-nil
	// marks the event for resolution.
	Ref any
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
