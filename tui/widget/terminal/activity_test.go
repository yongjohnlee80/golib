package terminal

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// reportingProc is a resultProc that says what it is doing (ActivityReporter).
type reportingProc struct {
	*resultProc
	says TerminalActivity
}

func (p *reportingProc) Activity() TerminalActivity { return p.says }

// A process that says what it is doing is asked; one that is no pseudo-terminal and says nothing
// is unknown, never a guess; a tab not there, or never started, is idle. The mutants, named here:
// a silent process read as idle, and the reporter not asked.
func TestATerminalAsksItsProcessOrSaysUnknown(t *testing.T) {
	tabs := NewTerminalTabs()
	quiet, says := newResultProc(), &reportingProc{resultProc: newResultProc(), says: TerminalRunning}
	defer close(quiet.release)
	defer close(says.release)
	a := addTestTerminal(t, tabs, "quiet", quiet)
	b, err := tabs.Add("says", WithStarter(func(pty.Cmd) (TerminalProcess, error) { return says, nil }))
	if err != nil {
		t.Fatal(err)
	}
	idle, _ := tabs.Add("unstarted")
	h := widgettest.StartApp(t, tabs, 40, 6)
	defer h.Stop()
	h.OnLoop(func() {
		for _, id := range []TerminalTabID{a, b} {
			if err := tabs.Start(id); err != nil {
				t.Error(err)
			}
		}
	})
	h.OnLoop(func() {
		for id, want := range map[TerminalTabID]TerminalActivity{a: TerminalActivityUnknown, b: TerminalRunning, idle: TerminalIdle, "no-such-tab": TerminalIdle} {
			if got := tabs.Activity(id); got != want {
				t.Errorf("tab %q: %v, want %v", id, got, want)
			}
		}
	})
}
