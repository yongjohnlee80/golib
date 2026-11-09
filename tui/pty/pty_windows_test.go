//go:build windows

package pty

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// output collects what a PTY prints, so a cell can wait for a marker in it. The console writes VT
// sequences around the text, so a cell looks for its marker anywhere in the output.
type output struct {
	mu   sync.Mutex
	b    strings.Builder
	done chan struct{}
}

func readAll(p *PTY) *output {
	o := &output{done: make(chan struct{})}
	go func() {
		defer close(o.done)
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			o.mu.Lock()
			o.b.Write(buf[:n])
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return o
}

func (o *output) waitFor(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		got := o.b.String()
		o.mu.Unlock()
		if strings.Contains(got, marker) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	t.Fatalf("%q never appeared in the console's output:\n%q", marker, o.b.String())
}

// waitExit fails if the child doesn't exit within d.
func waitExit(t *testing.T, p *PTY, d time.Duration) int {
	t.Helper()
	got := make(chan int, 1)
	go func() { code, _ := p.Wait(); got <- code }()
	select {
	case code := <-got:
		return code
	case <-time.After(d):
		t.Fatalf("the child (pid %d) did not exit within %v", p.Pid(), d)
		return 0
	}
}

// A program's output reaches Read, its exit code reaches Wait, and Read ends with io.EOF once the
// console is closed.
func TestAProgramsOutputAndExitCodeReachTheCaller(t *testing.T) {
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/c", "echo console-out-marker & exit 7"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	o := readAll(p)
	o.waitFor(t, "console-out-marker")
	if code := waitExit(t, p, 20*time.Second); code != 7 {
		t.Errorf("exit code %d, want 7", code)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	select {
	case <-o.done:
	case <-time.After(10 * time.Second):
		t.Fatal("Read did not end once the console was closed")
	}
}

// What Write sends is typed into the program, and Resize is accepted while it runs.
func TestTypedInputReachesTheProgramAndTheConsoleResizes(t *testing.T) {
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/q", "/k"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	o := readAll(p)
	if err := p.Resize(30, 100); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if _, err := io.WriteString(p, "echo typed-input-marker\r"); err != nil {
		t.Fatal(err)
	}
	o.waitFor(t, "typed-input-marker")
	if _, err := io.WriteString(p, "exit 3\r"); err != nil {
		t.Fatal(err)
	}
	if code := waitExit(t, p, 20*time.Second); code != 3 {
		t.Errorf("exit code %d, want 3", code)
	}
}

// Close ends a program that ignores the hang-up, and the programs it started: they are in its job.
func TestCloseEndsTheProgramAndItsChildren(t *testing.T) {
	hangupGrace = 500 * time.Millisecond
	defer func() { hangupGrace = 2 * time.Second }()
	// cmd starts a second cmd, which waits on ping: a tree two deep
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/c", "cmd /c ping -n 60 127.0.0.1 >nul"}})
	if err != nil {
		t.Fatal(err)
	}
	readAll(p)
	time.Sleep(500 * time.Millisecond) // the tree is started
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Close took %v: the tree did not end at the grace", d)
	}
	if err := p.Signal(os.Kill); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("a signal after Close gave %v, want os.ErrProcessDone", err)
	}
}

// os.Kill ends the program; a signal Windows has no form of is refused as unsupported.
func TestKillEndsTheProgramAndOtherSignalsAreUnsupported(t *testing.T) {
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/q", "/k"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	readAll(p)
	if err := p.Signal(syscall.Signal(0x1)); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("an unknown signal gave %v, want errors.ErrUnsupported", err)
	}
	if err := p.Signal(os.Kill); err != nil {
		t.Fatalf("os.Kill: %v", err)
	}
	waitExit(t, p, 10*time.Second)
}

// Close returns within its bound even where closing the console waits for output nobody reads (a
// Windows before 11 24H2): the grace period and the job's end don't wait on the console's close.
func TestCloseReturnsWhenClosingTheConsoleBlocks(t *testing.T) {
	hangupGrace = 300 * time.Millisecond
	defer func() { hangupGrace = 2 * time.Second }()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	orig := closePseudoConsole
	// blocks until released, as an older Windows's close does until its output is read; then closes
	// the real console, so the test leaks no HPCON
	closePseudoConsole = func(h windows.Handle) {
		close(entered)
		<-release
		orig(h)
		close(finished)
	}
	t.Cleanup(func() {
		// the stub was entered before the hook goes back, and it has finished before the test ends
		<-entered
		close(release)
		<-finished
		closePseudoConsole = orig
	})
	// a program that prints and waits, with no one reading its output
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/c", "for /L %i in (1,1,2000) do @echo line %i & ping -n 60 127.0.0.1 >nul"}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Close() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Close did not return while closing the console blocked: it waited on the console, not on its grace")
	}
	waitExit(t, p, 5*time.Second)
}
