//go:build linux || darwin

package pty

import (
	"os"

	"github.com/yongjohnlee80/golib/tui/internal/fdctl"
	"golang.org/x/sys/unix"
)

// foregroundGroup is the terminal's foreground process group, read from the master (TIOCGPGRP)
// through fdctl, so the master keeps its non-blocking mode. A variable, so a cell can make it fail.
var foregroundGroup = func(master *os.File) (int, error) {
	fg := 0
	err := fdctl.Control(master, func(fd int) error {
		var err error
		fg, err = unix.IoctlGetInt(fd, unix.TIOCGPGRP)
		return err
	})
	return fg, err
}

// Activity is what the program is doing (see Activity), asked of the system on the caller's
// goroutine: one ioctl on the master and one look at the program's children (children), with no
// blocking read. It may run on a UI loop: the look at the children stops at the first one.
//
//   - The program has exited: idle. A job it left running lost its terminal with it.
//   - The terminal's foreground group is not the program's own: a program it started holds the
//     terminal: running.
//   - Otherwise the program is at its prompt; it has children (a job in the background, or one
//     stopped with Ctrl+Z, which the foreground test cannot see): running; none: idle.
//   - The query failing: idle only when the program is marked exited meanwhile; unknown for any
//     error otherwise. A look at the children that fails: unknown.
func (p *PTY) Activity() Activity {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return ActivityIdle
	}
	fg, err := foregroundGroup(p.master)
	if err != nil {
		// idle, the answer that closes without asking, only for what is known: the program marked
		// exited meanwhile. No error says the terminal is gone (measured on Linux and macOS: once
		// the program has ended and closed its terminal, the query still answers, with group 0), so
		// any error (EINTR, a closed master's, one not foreseen) is unknown.
		p.mu.Lock()
		exited = p.exited
		p.mu.Unlock()
		if exited {
			return ActivityIdle
		}
		return ActivityUnknown
	}
	if fg > 0 && fg != p.pid {
		return ActivityRunning
	}
	found, err := hasChildren(p.pid)
	switch {
	case err != nil:
		return ActivityUnknown
	case found:
		return ActivityRunning
	}
	return ActivityIdle
}
