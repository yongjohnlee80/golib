package widget

// SHARED INTERACTIVE STATE.
//
// Every interactive widget answers the same question before it paints: which of
// its looks applies right now. Written per widget that becomes a different
// if/else chain each time, and they disagree — one checks disabled before
// focus, another after, and a disabled control that happens to hold focus paints
// as focused in one widget and as unavailable in the next.
//
// WidgetState is that answer, resolved once by a single precedence rule so the
// whole toolkit agrees.

// WidgetState is the look a widget should paint, after precedence is applied.
// Exactly one applies at a time.
type WidgetState uint8

const (
	// WidgetStateNormal: interactive, not focused, not pressed.
	WidgetStateNormal WidgetState = iota
	// WidgetStateFocused: holds the keyboard focus.
	WidgetStateFocused
	// WidgetStateArmed: pressed and not yet released — the runtime drives this
	// through Activatable.SetArmed.
	WidgetStateArmed
	// WidgetStateDisabled: not interactive. Nothing activates it.
	WidgetStateDisabled
)

// String names the state for traces and test failures.
func (w WidgetState) String() string {
	switch w {
	case WidgetStateNormal:
		return "normal"
	case WidgetStateFocused:
		return "focused"
	case WidgetStateArmed:
		return "armed"
	case WidgetStateDisabled:
		return "disabled"
	}
	return "unknown"
}

// resolveWidgetState applies the one precedence rule: disabled, then armed,
// then focused, then normal.
//
// DISABLED WINS OUTRIGHT. A disabled control can still hold focus — focus
// traversal and interactivity are separate concerns — and painting it as
// focused would advertise an interaction that cannot happen.
//
// ARMED BEATS FOCUSED because arming is transient and is the more specific
// thing to say: a control is only ever armed while it is also focused, so
// preferring focused would make the pressed look unreachable.
func resolveWidgetState(disabled, armed, focused bool) WidgetState {
	switch {
	case disabled:
		return WidgetStateDisabled
	case armed:
		return WidgetStateArmed
	case focused:
		return WidgetStateFocused
	}
	return WidgetStateNormal
}
