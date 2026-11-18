//go:build linux || darwin

package pty

import (
	"testing"
	"time"
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
