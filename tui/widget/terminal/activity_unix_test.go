//go:build linux || darwin

package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// WHAT A SHELL IS DOING (Terminal.Activity, pty's Activity): a real /bin/sh on a real pty, asked
// on the loop as a host asks when its tab's × is pressed. The cells wait on the drawn prompt or on
// the answer itself, never a sleep.

// activityWait is how long a cell waits for a shell's state: a process to start or end.
const activityWait = 20 * time.Second

// shellOn starts an interactive /bin/sh with job control, its prompt "ready$ ", and waits for
// the prompt.
func shellOn(t *testing.T) (*widgettest.Harness, *Terminal) {
	t.Helper()
	term := NewTerminal(WithCommand("/bin/sh", "-i", "-m"), WithEnv("PS1=ready$ ", "ENV=", "HISTFILE=/dev/null"))
	h := widgettest.StartApp(t, term, 60, 10)
	t.Cleanup(h.Stop)
	var started TerminalActivity
	h.OnLoop(func() {
		started = term.Activity()
		if err := term.Start(); err != nil {
			t.Fatal(err)
		}
	})
	if started != TerminalIdle {
		t.Fatalf("a terminal never started: %v, want idle", started)
	}
	waitScreen(t, h, term, "the shell's prompt", func(s string) bool { return strings.Contains(s, "ready$") })
	return h, term
}

// waitScreen waits for the drawn screen to satisfy ok.
func waitScreen(t *testing.T, h *widgettest.Harness, term *Terminal, what string, ok func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(activityWait)
	for {
		var s string
		h.OnLoop(func() { s = vtText(term) })
		if ok(s) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never showed; the screen:\n%s", what, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitActivity waits for the terminal's answer to be want, asked on the loop.
func waitActivity(t *testing.T, h *widgettest.Harness, term *Terminal, what string, want TerminalActivity) {
	t.Helper()
	deadline := time.Now().Add(activityWait)
	for {
		var got TerminalActivity
		h.OnLoop(func() { got = term.Activity() })
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			var s string
			h.OnLoop(func() { s = vtText(term) })
			t.Fatalf("%s: the terminal says %v, want %v; the screen:\n%s", what, got, want, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func send(t *testing.T, h *widgettest.Harness, term *Terminal, text string) {
	t.Helper()
	var err error
	h.OnLoop(func() { err = term.SendText(text) })
	if err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
}

// At its prompt the shell is idle; a program in the foreground is running, and idle again once
// Ctrl+C ends it. (The foreground test answers this without the process table; a foreground
// program is the shell's child too, so the children look alone would answer the same, and no
// mutant of the foreground test alone is named.)
func TestAShellAtItsPromptIsIdleAndAForegroundProgramRunning(t *testing.T) {
	t.Parallel()
	h, term := shellOn(t)
	waitActivity(t, h, term, "the shell at its prompt", TerminalIdle)
	send(t, h, term, "sleep 30\n")
	waitActivity(t, h, term, "sleep in the foreground", TerminalRunning)
	interruptUntilIdle(t, h, term)
}

// suspendUntilPrompt sends Ctrl+Z until the shell shows its prompt again, the foreground job
// stopped. As with Ctrl+C (interruptUntilIdle), one sent before the shell has handed its child the
// terminal reaches only the shell, which ignores it, so it is sent again.
func suspendUntilPrompt(t *testing.T, h *widgettest.Harness, term *Terminal) {
	t.Helper()
	deadline := time.Now().Add(activityWait)
	for {
		send(t, h, term, "\x1a")
		retry := time.Now().Add(time.Second)
		for time.Now().Before(retry) {
			var s string
			h.OnLoop(func() { s = vtText(term) })
			if strings.Count(s, "ready$") >= 2 {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			var s string
			h.OnLoop(func() { s = vtText(term) })
			t.Fatalf("Ctrl+Z never stopped the foreground program; the screen:\n%s", s)
		}
	}
}

// interruptUntilIdle sends Ctrl+C until the shell is idle at its prompt. A Ctrl+C sent in the
// moment between the shell's fork and its handing the child the terminal (tcsetpgrp) reaches the
// shell's own group, which an interactive shell ignores, and the child runs on: so it is sent
// again, as a user would press it again. One reaching the child once it holds the terminal ends it.
func interruptUntilIdle(t *testing.T, h *widgettest.Harness, term *Terminal) {
	t.Helper()
	deadline := time.Now().Add(activityWait)
	for {
		send(t, h, term, "\x03")
		retry := time.Now().Add(time.Second)
		for time.Now().Before(retry) {
			var got TerminalActivity
			h.OnLoop(func() { got = term.Activity() })
			if got == TerminalIdle {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			var s string
			h.OnLoop(func() { s = vtText(term) })
			t.Fatalf("Ctrl+C never ended the foreground program; the screen:\n%s", s)
		}
	}
}

// A job in the background, the shell back at its prompt, is running: the foreground test sees the
// shell alone, and the job is the shell's child; idle once the job is killed, before the shell has
// reaped it (a zombie child is not live). The mutants, named here: the children look left out (the
// background job reads idle), and a zombie counted as live (the killed job reads running until the
// shell waits for it).
func TestAJobInTheBackgroundIsRunning(t *testing.T) {
	t.Parallel()
	h, term := shellOn(t)
	send(t, h, term, "sleep 30 &\n")
	waitScreen(t, h, term, "the prompt after the job started", func(s string) bool { return strings.Count(s, "ready$") >= 2 })
	waitActivity(t, h, term, "sleep in the background", TerminalRunning)
	send(t, h, term, "kill %1\n")
	waitActivity(t, h, term, "the background job killed", TerminalIdle)
}

// A job stopped with Ctrl+Z, the shell back at its prompt, is running: closing would end it. Idle
// once it is killed.
func TestAStoppedJobIsRunning(t *testing.T) {
	t.Parallel()
	h, term := shellOn(t)
	send(t, h, term, "sleep 30\n")
	waitActivity(t, h, term, "sleep in the foreground", TerminalRunning)
	suspendUntilPrompt(t, h, term)
	waitActivity(t, h, term, "the sleep stopped, the shell at its prompt", TerminalRunning)
	send(t, h, term, "kill -9 %1\n")
	waitActivity(t, h, term, "the stopped job killed", TerminalIdle)
}

// The shell exited: idle, its program gone.
func TestAnExitedShellIsIdle(t *testing.T) {
	t.Parallel()
	h, term := shellOn(t)
	send(t, h, term, "exit\n")
	deadline := time.Now().Add(activityWait)
	for {
		var running bool
		h.OnLoop(func() { running = term.Running() })
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the shell never exited")
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitActivity(t, h, term, "the shell exited", TerminalIdle)
}
