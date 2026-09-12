package tui

import "time"

// Event is the marker interface every concrete event type implements. The
// set is closed within the package: the unexported method keeps third-party
// types out so the runtime's type switches stay exhaustive.
type Event interface{ isEvent() }

// --- lifecycle / terminal ---

// ResizeEvent reports a new terminal size. The App's intake stage coalesces
// resize storms latest-wins; backends emit ordered and un-coalesced.
type ResizeEvent struct{ W, H int }

func (ResizeEvent) isEvent() {}

// PasteEvent is one bracketed paste, with CR and CRLF normalized to \n.
type PasteEvent struct{ Text string }

func (PasteEvent) isEvent() {}

// FocusEvent covers both component focus and
// terminal focus in/out (Terminal=true, delivered to the focused component
// and published on the Bus).
type FocusEvent struct {
	Gained   bool
	Terminal bool
}

func (FocusEvent) isEvent() {}

// --- addressed deliveries. These go to exactly one node and do NOT bubble:
//     the addressee asked for the work, so an ancestor seeing its result would
//     be seeing someone else's mail. ---

// TimerID identifies one timer registration.
type TimerID uint64

// TickEvent is an addressed timer firing, delivered directly to Owner.
type TickEvent struct {
	Owner NodeID
	Timer TimerID
	At    time.Time
}

func (TickEvent) isEvent() {}
