package tui

import "time"

// Event is the marker interface every concrete event type implements. The
// set is closed within the package: the unexported method keeps third-party
// types out so the runtime's type switches stay exhaustive.
type Event interface{ isEvent() }

// NodeID identifies a mounted component. 0 = none; assigned monotonically
// at mount; never reused for the App's lifetime.
type NodeID uint64

// --- keyboard. The fields below carry kitty-protocol information, which the
//     backend negotiates at startup; they are zero on terminals that do not
//     answer.
// REFERENCE: https://sw.kovidgoyal.net/kitty/keyboard-protocol/ ---

// KeyKind distinguishes press, repeat, and release key actions.
type KeyKind uint8

const (
	KeyPress   KeyKind = iota
	KeyRepeat          // kitty flag 2 terminals only
	KeyRelease         // kitty flag 2 terminals only; never synthesized elsewhere
)

// Mods is the modifier bitmask, in kitty modifier order.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
	ModHyper
	ModMeta
	ModCapsLock
	ModNumLock
)

// KeyEvent is one key action. Code is the key's Unicode codepoint or a
// tui.Key* constant, allocated in the Unicode private-use plane so a
// functional key can never collide with a real character.
// REFERENCE: tui/keys.go Base/Shifted are the kitty "alternate keys" (base-layout
// and shifted codepoints; 0 when unreported) enabling layout-independent
// shortcut matching. Text is the associated text ("" for non-text keys); on
// legacy terminals Kind is always KeyPress and Base/Shifted are 0.
type KeyEvent struct {
	Kind    KeyKind
	Code    rune
	Base    rune
	Shifted rune
	Mods    Mods
	Text    string
}

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

// --- lifecycle / terminal ---

// ResizeEvent reports a new terminal size. The App's intake stage coalesces
// resize storms latest-wins; backends emit ordered and un-coalesced.
type ResizeEvent struct{ W, H int }

// PasteEvent is one bracketed paste, with CR and CRLF normalized to \n.
type PasteEvent struct{ Text string }

// FocusEvent covers both component focus and
// terminal focus in/out (Terminal=true, delivered to the focused component
// and published on the Bus).
type FocusEvent struct {
	Gained   bool
	Terminal bool
}

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

// TaskID identifies one App.Go task. Monotonic per App; never reused
// (staleness checks).
type TaskID uint64

// TaskResult is the terminal outcome of an App.Go task, addressed to the
// owning component. Err wraps the task's panic when it panicked.
type TaskResult struct {
	Owner NodeID
	ID    TaskID
	Value any
	Err   error
}

// TaskProgress is an intermediate, addressed update from a still-running
// task that has not finished. Routed exactly like TaskResult.
type TaskProgress struct {
	Owner NodeID
	ID    TaskID
	Value any
}

func (KeyEvent) isEvent()     {}
func (MouseEvent) isEvent()   {}
func (ResizeEvent) isEvent()  {}
func (PasteEvent) isEvent()   {}
func (FocusEvent) isEvent()   {}
func (TickEvent) isEvent()    {}
func (TaskResult) isEvent()   {}
func (TaskProgress) isEvent() {}
