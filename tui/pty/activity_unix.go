//go:build linux || darwin

package pty

import (
	"github.com/yongjohnlee80/golib/tui/internal/fdctl"
	"golang.org/x/sys/unix"
)

// Activity is what the program is doing (see Activity), asked of the system on the caller's
// goroutine: one ioctl on the master and one look at the program's children (children), with no
// blocking read. It may run on a UI loop.
//
//   - The program has exited: idle. A job it left running lost its terminal with it.
//   - The terminal's foreground group is not the program's own: a program it started holds the
//     terminal: running.
//   - Otherwise the program is at its prompt; it has children (a job in the background, or one
//     stopped with Ctrl+Z, which the foreground test cannot see): running; none: idle.
//   - The terminal gone under the call (its last holder closed it, as when the program has just
//     exited): idle, the program having nothing left to lose; a look at the children that fails
//     for another reason: unknown.
func (p *PTY) Activity() Activity {
	p.mu.Lock()
	exited := p.exited
	p.mu.Unlock()
	if exited {
		return ActivityIdle
	}
	fg := 0
	if err := fdctl.Control(p.master, func(fd int) error {
		var err error
		fg, err = unix.IoctlGetInt(fd, unix.TIOCGPGRP)
		return err
	}); err != nil {
		return ActivityIdle // the terminal is gone: nothing on it is left to lose
	}
	if fg > 0 && fg != p.pid {
		return ActivityRunning
	}
	n, err := children(p.pid)
	switch {
	case err != nil:
		return ActivityUnknown
	case n > 0:
		return ActivityRunning
	}
	return ActivityIdle
}
