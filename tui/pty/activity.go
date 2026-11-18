package pty

// Activity is what a pseudo-terminal's program is doing, as far as the system can tell: idle (the
// program alone, at its prompt), running something (a program in the foreground, or a job it
// started, running or stopped, that ending the terminal would end too), or unknown (the system
// gives no honest answer, and a caller that would lose work treats it as running).
//
// Two known limits. A program that execs another (a shell's exec vim) is that program, alone:
// idle. A program with a child that never ends by design (a shell's prompt helper, an agent it
// started) is always running, so a caller asks before every close: the safe side.
type Activity uint8

const (
	// ActivityUnknown: the system gives no answer to rely on.
	ActivityUnknown Activity = iota
	// ActivityIdle: the program runs alone, nothing started by it lives on, or the program has
	// exited.
	ActivityIdle
	// ActivityRunning: something the program started is alive, in the foreground or not.
	ActivityRunning
)

// String names the activity as the constants do.
func (a Activity) String() string {
	switch a {
	case ActivityIdle:
		return "idle"
	case ActivityRunning:
		return "running"
	}
	return "unknown"
}
