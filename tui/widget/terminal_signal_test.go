package widget

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/pty"
)

// signalProc is a resultProc that can be signalled, keeping each signal it is sent.
type signalProc struct {
	*resultProc
	mu  sync.Mutex
	got []os.Signal
}

func (p *signalProc) Signal(sig os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, sig)
	return nil
}

func (p *signalProc) signals() []os.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]os.Signal(nil), p.got...)
}

// vtText is the screen's text, its rows joined by newlines.
func vtText(term *Terminal) string {
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

// stopped reports whether pid is stopped (state T in /proc/pid/stat).
func stopped(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] == 'T'
}

// pidOf is the process whose command line holds marker, 0 for none.
func pidOf(marker string) int {
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		if b, err := os.ReadFile("/proc/" + d.Name() + "/cmdline"); err == nil && strings.Contains(string(b), marker) {
			return pid
		}
	}
	return 0
}

// A program that stopped itself, as a CLI does on its own Ctrl+Z with no shell to fg it, resumes on
// the Terminal's SIGCONT: the signal reaches its process group through the real pty.
func TestATerminalsSIGCONTResumesAProgramThatStoppedItself(t *testing.T) {
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skip("no /proc to see the program stop")
	}
	marker := "sigcell-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	term := NewTerminal(WithCommand("/bin/sh", "-c", "echo before; kill -STOP $$; echo resumed; exec sleep 30", marker))
	h := startAppInternal(t, term, 30, 6)
	defer h.stopInternal()
	h.onLoopInternal(func() {
		if err := term.Start(); err != nil {
			t.Fatal(err)
		}
	})
	var pid int
	if !waitInternal(h, func() bool { pid = pidOf(marker); return pid > 0 && stopped(pid) }) {
		t.Fatalf("the program never stopped itself (pid %d): the cell observes nothing", pid)
	}
	if !waitInternal(h, func() bool { return strings.Contains(vtText(term), "before") }) {
		t.Fatalf("the program's output never showed; the screen:\n%s", vtText(term))
	}
	if strings.Contains(vtText(term), "resumed") {
		t.Fatal("resumed before any SIGCONT: the cell observes nothing")
	}
	var serr error
	h.onLoopInternal(func() { serr = term.Signal(syscall.SIGCONT) })
	if serr != nil {
		t.Fatalf("Signal(SIGCONT): %v", serr)
	}
	if !waitInternal(h, func() bool { return strings.Contains(vtText(term), "resumed") }) {
		t.Fatalf("the program did not resume; the screen:\n%s", vtText(term))
	}
}

// A Terminal with no program running refuses a signal, before its start and after its exit, and
// sends its old process nothing.
func TestASignalWithNoProgramRunningIsRefused(t *testing.T) {
	p := &signalProc{resultProc: newResultProc()}
	close(p.release)
	term, h, _ := mountTerminal(t, p)
	p.end() // the program exits
	if !waitInternal(h, func() bool { return !term.Running() }) {
		t.Fatal("the program never ended")
	}
	var err error
	h.onLoopInternal(func() { err = term.Signal(syscall.SIGCONT) })
	if !errors.Is(err, ErrTerminalNotRunning) {
		t.Fatalf("Signal after the exit: %v, want ErrTerminalNotRunning", err)
	}
	if got := p.signals(); len(got) != 0 {
		t.Errorf("the exited program was sent %v", got)
	}
	fresh := NewTerminal()
	if err := fresh.Signal(syscall.SIGCONT); !errors.Is(err, ErrTerminalNotRunning) {
		t.Errorf("Signal before any start: %v, want ErrTerminalNotRunning", err)
	}
}

// A process that cannot be signalled says so; one that can is sent the signal, once.
func TestASignalReachesOnlyAProcessThatCanTakeIt(t *testing.T) {
	plain := newResultProc()
	close(plain.release)
	term, h, _ := mountTerminal(t, plain)
	var err error
	h.onLoopInternal(func() { err = term.Signal(syscall.SIGCONT) })
	if !errors.Is(err, ErrTerminalSignalUnsupported) {
		t.Fatalf("a process with no Signal: %v, want ErrTerminalSignalUnsupported", err)
	}

	sp := &signalProc{resultProc: newResultProc()}
	close(sp.release)
	term2, h2, _ := mountTerminal(t, sp)
	h2.onLoopInternal(func() { err = term2.Signal(syscall.SIGCONT) })
	if err != nil || len(sp.signals()) != 1 || sp.signals()[0] != syscall.SIGCONT {
		t.Fatalf("a signalling process: err %v, sent %v; want SIGCONT once", err, sp.signals())
	}
}

// TerminalTabs' Signal reaches the tab it names, not the active one, and a removed tab refuses it.
func TestATabsSignalReachesTheTabItNames(t *testing.T) {
	tabs := NewTerminalTabs()
	add := func(label string, p *signalProc) TerminalTabID {
		id, err := tabs.Add(label, WithStarter(func(pty.Cmd) (TerminalProcess, error) { return p, nil }))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := &signalProc{resultProc: newResultProc()}, &signalProc{resultProc: newResultProc()}
	close(a.release)
	close(b.release)
	ida, idb := add("a", a), add("b", b)
	h := startAppInternal(t, tabs, 40, 8)
	defer h.stopInternal()
	var err error
	h.onLoopInternal(func() {
		for _, id := range []TerminalTabID{ida, idb} {
			if err := tabs.Start(id); err != nil {
				t.Fatal(err)
			}
		}
		if err := tabs.Select(ida); err != nil {
			t.Fatal(err)
		}
		err = tabs.Signal(idb, syscall.SIGCONT)
	})
	if err != nil || len(b.signals()) != 1 || len(a.signals()) != 0 {
		t.Fatalf("Signal(b) with a active: err %v, a got %v, b got %v; want b alone, once", err, a.signals(), b.signals())
	}
	var r *TerminalTabRemovalResult
	h.onLoopInternal(func() { r, err = tabs.Remove(idb) })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	h.onLoopInternal(func() { err = tabs.Signal(idb, syscall.SIGCONT) })
	if err == nil || len(b.signals()) != 1 {
		t.Fatalf("Signal on a removed tab: err %v, b got %v; want it refused", err, b.signals())
	}
}
