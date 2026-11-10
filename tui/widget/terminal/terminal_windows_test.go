//go:build windows

package terminal

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/pty"
)

// A real program on a Windows pseudo-console ends its tab when it exits: the reader reaches the end
// with no Stop, and the exit and its code are posted, as on Unix.
func TestATerminalsProgramExitingOnWindowsEndsItsTab(t *testing.T) {
	p, err := pty.Start(pty.Cmd{Path: "cmd.exe", Args: []string{"/c", "echo widget-marker & exit 3"}, Rows: 4, Cols: 20})
	if err != nil {
		t.Fatal(err)
	}
	term, h, _ := mountTerminal(t, p)
	pumpsGone(t, term)
	h.Sync()
	var running, exited bool
	var code int
	h.OnLoop(func() { running, exited, code = term.Running(), term.exited, term.exitCode })
	if running || !exited || code != 3 {
		t.Errorf("after the program exited: running %v, exited %v, code %d; want an exit with 3", running, exited, code)
	}
}
