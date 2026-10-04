// Package pty starts a program on a pseudo-terminal.
//
// Start opens a terminal pair, makes the child a session leader with the
// terminal as its controlling tty, and returns the master side as a *PTY:
// Read is the program's output, Write is its keyboard. Resize sets the
// terminal's size (the child gets SIGWINCH), Signal reaches the child's
// process group, and Wait reports how the child ended.
//
// Close behaves like closing a terminal window: it hangs up. It sends SIGHUP
// (and SIGCONT, so a stopped job sees it) to the child's process group, and
// an interactive shell forwards the hang-up to the jobs it started in other
// process groups. The master closes once the child has exited; a child
// still running after a grace period (two seconds) loses its terminal and
// has its group killed. Close returns once the child is reaped — so it can
// block for the grace period, and a UI calls it off its loop.
// A program that detached itself (setsid, nohup, disown, a daemon) keeps
// running, as it would after any terminal closed.
//
// Linux and macOS are supported, through the standard library and
// golang.org/x/sys only. Elsewhere Start returns an error matching
// errs.ErrUnsupported.
package pty
