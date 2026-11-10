//go:build windows

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// text is what has been read so far.
func (o *output) text() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
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
	// a program that prints and waits, with no one reading its output; started before the stub is
	// installed, so a Start that fails leaves no cleanup waiting on a stub nothing will call
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/c", "for /L %i in (1,1,2000) do @echo line %i & ping -n 60 127.0.0.1 >nul"}})
	if err != nil {
		t.Fatal(err)
	}
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
		// Close calls the stub (below), so it has been entered; it is released and joined before
		// the hook goes back
		<-entered
		close(release)
		<-finished
		closePseudoConsole = orig
	})
	done := make(chan error, 1)
	go func() { done <- p.Close() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Close did not return while closing the console blocked: it waited on the console, not on its grace")
	}
	waitExit(t, p, 5*time.Second)
}

// helperRole is how the test binary, started again on a console, knows what to be: "leaver"
// starts a "late" child of its own and exits 5 at once; "late" says it runs, waits until the file
// helperRelease names exists (the test's release, never a timer), then prints and exits.
const (
	helperRole    = "GOLIB_PTY_HELPER"
	helperRelease = "GOLIB_PTY_RELEASE"
)

func TestMain(m *testing.M) {
	switch os.Getenv(helperRole) {
	case "leaver":
		late := exec.Command(os.Args[0])
		late.Env = append(os.Environ(), helperRole+"=late")
		late.Stdout, late.Stderr = os.Stdout, os.Stderr // the console, which it shares
		if err := late.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Println("leaver-marker")
		os.Exit(5)
	case "late":
		fmt.Println("late-ready")
		for {
			if _, err := os.Stat(os.Getenv(helperRelease)); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond) // a poll for the release, which decides nothing by itself
		}
		fmt.Println("late-marker")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Once every program of the console has exited, Read ends with io.EOF without a Close: a reader
// that reads to the end, then waits for the code, sees the program end.
func TestReadEndsOnceTheConsolesProgramsHaveExited(t *testing.T) {
	p, err := Start(Cmd{Path: "cmd.exe", Args: []string{"/c", "echo ended-marker & exit 7"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	o := readAll(p)
	select {
	case <-o.done:
	case <-time.After(20 * time.Second):
		t.Fatal("Read did not end once the program had exited")
	}
	if !strings.Contains(o.text(), "ended-marker") {
		t.Errorf("the output before the end: %q", o.text())
	}
	if code := waitExit(t, p, 5*time.Second); code != 7 {
		t.Errorf("exit code %d, want 7", code)
	}
}

// A child the program left running keeps the console: Read doesn't end while it runs, however long
// after the program's exit, its output arrives before the end, and Wait's code is the program's.
// The child is held by the test, not a timer: it runs until the test, having seen the program's
// exit and Read still open, releases it.
func TestAChildLeftRunningKeepsTheConsoleUntilItEnds(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release")
	env := append(os.Environ(), helperRole+"=leaver", helperRelease+"="+release)
	p, err := Start(Cmd{Path: os.Args[0], Env: env, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	defer os.WriteFile(release, nil, 0o600) // a failed cell never leaves the child waiting
	o := readAll(p)
	o.waitFor(t, "late-ready") // the child runs, held
	if code := waitExit(t, p, 20*time.Second); code != 5 {
		t.Errorf("exit code %d, want the program's 5", code)
	}
	select {
	case <-o.done:
		t.Fatalf("Read ended when the program exited, its child still held: %q", o.text())
	default:
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-o.done:
	case <-time.After(20 * time.Second):
		t.Fatal("Read did not end once the child had exited too")
	}
	if !strings.Contains(o.text(), "late-marker") {
		t.Errorf("the child's output did not arrive before the end: %q", o.text())
	}
}
