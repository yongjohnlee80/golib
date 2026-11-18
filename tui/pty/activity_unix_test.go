//go:build linux || darwin

package pty

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A program ending while it is asked what it is doing reads idle, whichever side of its end the
// question lands on (running, exiting with its terminal's last holder gone, reaped): never
// unknown, and never an error. The program runs alone, so running is never right either. Which
// path answers depends on where the question lands (the exited mark, the ioctl on a terminal whose
// last holder is gone, the children of a process with no /proc entry left); the cell holds the
// answer, not one path, so no single mutant is named for it.
func TestAProgramEndingWhileAskedIsIdle(t *testing.T) {
	for range 20 {
		p, err := Start(Cmd{Path: "/bin/sh", Args: []string{"-c", "exit 0"}})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _, _ = p.Wait(); close(done) }()
		asked := 0
	loop:
		for {
			if a := p.Activity(); a != ActivityIdle {
				_ = p.Close()
				t.Fatalf("asked %d times, the program ending said %v; want idle", asked, a)
			}
			asked++
			select {
			case <-done:
				break loop
			case <-time.After(time.Millisecond):
			}
		}
		if a := p.Activity(); a != ActivityIdle {
			t.Fatalf("after its end: %v, want idle", a)
		}
		_ = p.Close()
	}
}

// Once a program has ended and closed its terminal, the foreground query still answers, with no
// error and group 0 (measured on Linux and macOS): Activity reads that as no foreground program and
// goes on to the children, so an ended terminal is idle, never unknown. A system whose query fails
// there instead reads unknown, the safe side, and this cell says so.
func TestTheForegroundQueryOnAnEndedTerminalAnswers(t *testing.T) {
	p, err := Start(Cmd{Path: "/bin/sh", Args: []string{"-c", "exit 0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	fg, err := foregroundGroup(p.master)
	if err != nil || fg > 0 {
		t.Fatalf("TIOCGPGRP on an ended terminal: group %d, error %v; measured: group 0 (or none), no error", fg, err)
	}
}

// The foreground query failing on a running program, whatever its error (EINTR, EBADF, even EIO,
// a hung-up terminal's), is unknown, never idle: no error is known to mean the terminal is gone.
// The mutant, named here: every query error read as idle.
func TestAForegroundQueryFailingForAnotherReasonIsUnknown(t *testing.T) {
	p, err := Start(Cmd{Path: "/bin/sh", Args: []string{"-c", "exec sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	real := foregroundGroup
	defer func() { foregroundGroup = real }()
	for errno, want := range map[unix.Errno]Activity{unix.EINTR: ActivityUnknown, unix.EBADF: ActivityUnknown, unix.EIO: ActivityUnknown} {
		foregroundGroup = func(*os.File) (int, error) { return 0, errno }
		if got := p.Activity(); got != want {
			t.Errorf("the query failing with %v on a running program: %v, want %v", errno, got, want)
		}
	}
}
