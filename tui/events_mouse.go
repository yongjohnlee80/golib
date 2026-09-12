package tui

// MouseEvent coordinates are LOCAL to the receiving component at every
// routing hop: each level rewrites them into its own child's coordinate space,
// so a component reads a position relative to itself and never has to know
// where it sits on screen.
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

// --- mouse. Positions arrive SGR-encoded, which is what makes coordinates
//     beyond column 223 representable at all. ---

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
