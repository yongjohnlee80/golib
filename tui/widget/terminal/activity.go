package terminal

import "github.com/yongjohnlee80/golib/tui/pty"

// TerminalActivity is what a terminal's program is doing, for a host deciding whether closing it
// would lose work: idle at its prompt, running something, or unknown.
type TerminalActivity uint8

const (
	// TerminalActivityUnknown: nothing honest can be said; a host about to close treats it as
	// running, and asks.
	TerminalActivityUnknown TerminalActivity = iota
	// TerminalIdle: the program runs alone at its prompt, or there is no program (never started,
	// or exited): closing loses nothing it is doing.
	TerminalIdle
	// TerminalRunning: something the program started is alive, in the foreground, in the
	// background, or stopped (Ctrl+Z): closing ends it.
	TerminalRunning
)

// String names the activity as the constants do.
func (a TerminalActivity) String() string {
	switch a {
	case TerminalIdle:
		return "idle"
	case TerminalRunning:
		return "running"
	}
	return "unknown"
}

// ActivityReporter is a TerminalProcess that says what it is doing, for a process that is no
// pseudo-terminal of the system's (a host's own, say): the terminal asks it instead.
type ActivityReporter interface {
	Activity() TerminalActivity
}

// Activity is what the terminal's program is doing: TerminalIdle with none running; the system's
// answer for a pseudo-terminal it started (pty.PTY's Activity: one ioctl and a look at the
// program's children, no blocking read); the process's own for an ActivityReporter; else
// TerminalActivityUnknown. It belongs to the UI loop, where it is cheap enough to ask on a press.
func (t *Terminal) Activity() TerminalActivity {
	if !t.Running() || t.proc == nil {
		return TerminalIdle
	}
	switch p := t.proc.(type) {
	case ActivityReporter:
		return p.Activity()
	case *pty.PTY:
		switch p.Activity() {
		case pty.ActivityIdle:
			return TerminalIdle
		case pty.ActivityRunning:
			return TerminalRunning
		}
	}
	return TerminalActivityUnknown
}

// Activity is what tab id's program is doing (Terminal.Activity): TerminalIdle for a tab that is
// not there, or is closing already, since a close of it loses nothing more. It belongs to the UI
// loop.
func (t *TerminalTabs) Activity(id TerminalTabID) TerminalActivity {
	e, err := t.find(id)
	if err != nil || e.closing != nil {
		return TerminalIdle
	}
	return e.terminal.Activity()
}
