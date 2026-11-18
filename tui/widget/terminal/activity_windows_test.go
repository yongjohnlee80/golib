//go:build windows

package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// WHAT A SHELL IS DOING, ON WINDOWS (Terminal.Activity, pty's Activity): a real cmd.exe on a real
// pseudo-console, whose job holds it and whatever it starts. The cells wait on the drawn prompt or
// on the answer itself, never a sleep.

// activityWait is how long a cell waits for a shell's state: a process to start or end.
const activityWait = 30 * time.Second

// cmdOn starts an interactive cmd.exe, its prompt "ready>", and waits for the prompt.
func cmdOn(t *testing.T) (*widgettest.Harness, *Terminal) {
	t.Helper()
	term := NewTerminal(WithCommand("cmd.exe", "/k", "prompt ready$G"))
	h := widgettest.StartApp(t, term, 80, 12)
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
	waitScreenWin(t, h, term, "cmd's prompt", func(s string) bool { return strings.Contains(s, "ready>") })
	return h, term
}

// winText is the screen's text, its rows joined by newlines.
func winText(term *Terminal) string {
	scr := term.Screen()
	rows, cols := scr.Size()
	var b strings.Builder
	for r := range rows {
		for c := range cols {
			b.WriteString(scr.Cell(r, c).Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func waitScreenWin(t *testing.T, h *widgettest.Harness, term *Terminal, what string, ok func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(activityWait)
	for {
		var s string
		h.OnLoop(func() { s = winText(term) })
		if ok(s) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never showed; the screen:\n%s", what, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitActivityWin(t *testing.T, h *widgettest.Harness, term *Terminal, what string, want TerminalActivity) {
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
			h.OnLoop(func() { s = winText(term) })
			t.Fatalf("%s: the terminal says %v, want %v; the screen:\n%s", what, got, want, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func sendWin(t *testing.T, h *widgettest.Harness, term *Terminal, text string) {
	t.Helper()
	var err error
	h.OnLoop(func() { err = term.SendText(text) })
	if err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
}

// cmd.exe at its prompt is idle: the job holds it alone (the pseudo-console's host is not in the
// job). A program it starts is running while it runs, and idle once it has ended. The mutants,
// named here: the shell counted twice (idle read as running: the job's count compared with 0), and
// the count not read (running read as idle).
func TestCmdAtItsPromptIsIdleAndAChildRunning(t *testing.T) {
	h, term := cmdOn(t)
	waitActivityWin(t, h, term, "cmd at its prompt", TerminalIdle)
	sendWin(t, h, term, "ping -n 30 127.0.0.1\r")
	waitActivityWin(t, h, term, "ping running", TerminalRunning)
	sendWin(t, h, term, "\x03")
	waitActivityWin(t, h, term, "ping ended by Ctrl+C, cmd at its prompt", TerminalIdle)
	sendWin(t, h, term, "ping -n 2 127.0.0.1\r")
	waitActivityWin(t, h, term, "a short ping running", TerminalRunning)
	waitActivityWin(t, h, term, "the short ping ended by itself", TerminalIdle)
}

// cmd.exe exited is idle, its program gone, and asking again after its job is closed (the
// terminal's Stop) is idle too, never unknown.
func TestAnExitedCmdIsIdle(t *testing.T) {
	h, term := cmdOn(t)
	sendWin(t, h, term, "exit\r")
	deadline := time.Now().Add(activityWait)
	for {
		var running bool
		h.OnLoop(func() { running = term.Running() })
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cmd never exited")
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitActivityWin(t, h, term, "cmd exited", TerminalIdle)
}

// Windows PowerShell, AutoDoc's shell on Windows when pwsh is not installed, at its prompt is idle:
// the job holds it alone. A program it starts (ping) is running, and idle again after Ctrl+C. A
// cmdlet runs inside PowerShell's own process, so a cmdlet at work (Start-Sleep) reads idle, as a
// Unix shell's builtin does: a known limit (pty's Activity).
func TestPowerShellAtItsPromptIsIdleAndAProgramItStartsRunning(t *testing.T) {
	term := NewTerminal(WithCommand("powershell.exe", "-NoLogo", "-NoProfile"))
	h := widgettest.StartApp(t, term, 100, 12)
	t.Cleanup(h.Stop)
	h.OnLoop(func() {
		if err := term.Start(); err != nil {
			t.Fatal(err)
		}
	})
	waitScreenWin(t, h, term, "PowerShell's prompt", func(s string) bool { return strings.Contains(s, "PS ") && strings.Contains(s, ">") })
	waitActivityWin(t, h, term, "PowerShell at its prompt", TerminalIdle)
	sendWin(t, h, term, "ping -n 30 127.0.0.1\r")
	waitActivityWin(t, h, term, "ping running under PowerShell", TerminalRunning)
	sendWin(t, h, term, "\x03")
	waitActivityWin(t, h, term, "ping ended by Ctrl+C, PowerShell at its prompt", TerminalIdle)
}
