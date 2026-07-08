package tui

import "time"

// Event is the marker interface every concrete event type implements
// (ADR-0001 §2.3, ADR-0005 §2.5). The set is closed within the package: the
// unexported method keeps third-party types out so the runtime's type
// switches stay exhaustive.
type Event interface{ isEvent() }

// NodeID identifies a mounted component. 0 = none; assigned monotonically at
// mount; never reused for the App's lifetime (ADR-0004 §2.4).
type NodeID uint64

// --- keyboard (kitty protocol fields — ADR-0002 negotiates flags 1+2;
//     https://sw.kovidgoyal.net/kitty/keyboard-protocol/) ---

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
// tui.Key* constant (private-use plane, ADR-0002's table — shipped with the
// tui/term decoder). Base/Shifted are the kitty "alternate keys" (base-layout
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

// --- mouse (SGR encoding — ADR-0002) ---

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
// routing hop (ADR-0004 §2.5 rewrites them per level).
type MouseEvent struct {
	Kind   MouseKind
	Button MouseButton
	X, Y   int
	Mods   Mods
}

// --- lifecycle / terminal ---

// ResizeEvent reports a new terminal size. The App's intake stage coalesces
// resize storms latest-wins (ADR-0005 §2.4); backends emit ordered and
// un-coalesced (ADR-0002 §2.8).
type ResizeEvent struct{ W, H int }

// PasteEvent is one bracketed paste, with CR and CRLF normalized to \n
// (ADR-0002 §2.7).
type PasteEvent struct{ Text string }

// FocusEvent covers both component focus (routed per ADR-0004 §2.6) and
// terminal focus in/out (Terminal=true, delivered to the focused component
// and published on the Bus).
type FocusEvent struct {
	Gained   bool
	Terminal bool
}

// --- addressed deliveries (no bubbling — ADR-0004 §2.5) ---

// TimerID identifies one timer registration (ADR-0005 §2.6).
type TimerID uint64

// TickEvent is an addressed timer firing, delivered directly to Owner.
type TickEvent struct {
	Owner NodeID
	Timer TimerID
	At    time.Time
}

// TaskID identifies one App.Go task. Monotonic per App; never reused
// (staleness checks, ADR-0005 §2.8).
type TaskID uint64

// TaskResult is the terminal outcome of an App.Go task, addressed to the
// owning component. Err wraps the task's panic when it panicked
// (ADR-0005 §2.8).
type TaskResult struct {
	Owner NodeID
	ID    TaskID
	Value any
	Err   error
}

// TaskProgress is an intermediate, addressed update from a still-running
// task (ADR-0005 §2.8 streaming). Routed exactly like TaskResult.
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
