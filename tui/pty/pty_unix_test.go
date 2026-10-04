//go:build linux || darwin

package pty

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The test binary doubles as the child: TestMain runs a helper mode when
// PTY_HELPER is set, so the cells need no shell for what Go can report.
func TestMain(m *testing.M) {
	if mode := os.Getenv("PTY_HELPER"); mode != "" {
		helper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(mode string) {
	switch {
	case mode == "leader":
		pid := os.Getpid()
		sid, _ := unix.Getsid(0)
		fg, _ := unix.IoctlGetInt(0, unix.TIOCGPGRP)
		fmt.Printf("pid=%d sid=%d fg=%d\n", pid, sid, fg)
	case strings.HasPrefix(mode, "exit="):
		n, _ := strconv.Atoi(strings.TrimPrefix(mode, "exit="))
		os.Exit(n)
	case mode == "ignorehup":
		signal.Ignore(syscall.SIGHUP)
		fmt.Println("ready")
		time.Sleep(time.Minute)
	case mode == "sleep":
		fmt.Println("ready")
		time.Sleep(time.Minute)
	}
}

func startHelper(t *testing.T, mode string) *PTY {
	t.Helper()
	p, err := Start(Cmd{
		Path: os.Args[0],
		Args: []string{"-test.run=^$"},
		Env:  append(os.Environ(), "PTY_HELPER="+mode),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

// reader collects a PTY's output on a goroutine, so cells can wait for
// text with a deadline.
type reader struct {
	lines chan string
}

func readLines(p *PTY) *reader {
	r := &reader{lines: make(chan string, 64)}
	go func() {
		defer close(r.lines)
		sc := bufio.NewScanner(p)
		for sc.Scan() {
			r.lines <- strings.TrimRight(sc.Text(), "\r")
		}
	}()
	return r
}

// await returns the first line containing want.
func (r *reader) await(t *testing.T, want string) string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-r.lines:
			if !ok {
				t.Fatalf("output ended before %q", want)
			}
			if strings.Contains(l, want) {
				return l
			}
		case <-deadline:
			t.Fatalf("no %q within 10s", want)
		}
	}
}

func TestEchoRoundTrips(t *testing.T) {
	p, err := Start(Cmd{Path: "/bin/sh", Args: []string{"-c", `read x; echo "got:$x"`}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := readLines(p)
	if _, err := p.Write([]byte("abc\n")); err != nil {
		t.Fatal(err)
	}
	if l := r.await(t, "got:"); l != "got:abc" {
		t.Fatalf("line = %q", l)
	}
	if code, err := p.Wait(); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
}

func TestSizeAndResize(t *testing.T) {
	p, err := Start(Cmd{
		Path: "/bin/sh", Args: []string{"-c", `stty size; read x; stty size`},
		Rows: 33, Cols: 101,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := readLines(p)
	r.await(t, "33 101")
	if err := p.Resize(40, 120); err != nil {
		t.Fatal(err)
	}
	p.Write([]byte("\n"))
	r.await(t, "40 120")
	if err := p.Resize(0, 10); err == nil {
		t.Fatal("Resize(0, 10) succeeded")
	}
}

func TestDefaultSize(t *testing.T) {
	p, err := Start(Cmd{Path: "stty", Args: []string{"size"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	readLines(p).await(t, "24 80")
}

func TestChildIsSessionLeaderWithControllingTTY(t *testing.T) {
	p := startHelper(t, "leader")
	l := readLines(p).await(t, "pid=")
	want := fmt.Sprintf("pid=%d sid=%d fg=%d", p.Pid(), p.Pid(), p.Pid())
	if l != want {
		t.Fatalf("helper reported %q, want %q", l, want)
	}
}

func TestWaitReportsExitCode(t *testing.T) {
	p := startHelper(t, "exit=7")
	if code, err := p.Wait(); code != 7 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	// After the exit the group is gone: Signal says so, and Read ends.
	if err := p.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Signal after exit = %v", err)
	}
	if _, err := io.ReadAll(p); err != nil {
		t.Fatalf("ReadAll after exit = %v", err)
	}
}

func TestSignalReachesTheChild(t *testing.T) {
	p := startHelper(t, "sleep")
	readLines(p).await(t, "ready")
	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code, _ := p.Wait(); code != 128+int(syscall.SIGTERM) {
		t.Fatalf("Wait = %d, want %d", code, 128+int(syscall.SIGTERM))
	}
	if err := p.Signal(os.Interrupt); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Signal after exit = %v", err)
	}
}

type fakeSignal struct{}

func (fakeSignal) String() string { return "fake" }
func (fakeSignal) Signal()        {}

func TestSignalRejectsForeignSignal(t *testing.T) {
	p := startHelper(t, "sleep")
	if err := p.Signal(fakeSignal{}); err == nil {
		t.Fatal("Signal(fake) succeeded")
	}
}

func TestCloseHangsUpTheChild(t *testing.T) {
	p := startHelper(t, "sleep")
	readLines(p).await(t, "ready")
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if code, _ := p.Wait(); code != 128+int(syscall.SIGHUP) {
		t.Fatalf("Wait = %d, want SIGHUP's %d", code, 128+int(syscall.SIGHUP))
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Close took %v for a child that ends on SIGHUP", d)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

func TestCloseKillsAChildIgnoringHangup(t *testing.T) {
	old := hangupGrace
	hangupGrace = 200 * time.Millisecond
	defer func() { hangupGrace = old }()

	p := startHelper(t, "ignorehup")
	readLines(p).await(t, "ready")
	start := time.Now()
	closed := make(chan struct{})
	go func() {
		p.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		p.Signal(syscall.SIGKILL)
		t.Fatal("Close still blocked 10s after the grace period")
	}
	if code, _ := p.Wait(); code != 128+int(syscall.SIGKILL) {
		t.Fatalf("Wait = %d, want SIGKILL's %d", code, 128+int(syscall.SIGKILL))
	}
	if d := time.Since(start); d < 200*time.Millisecond {
		t.Fatalf("Close killed after %v, before the grace period", d)
	}
}

// An interactive bash puts a background job in its own process group; the
// hang-up reaches that job through bash's forwarding. The grace is long here:
// the cell is about the forwarding, and a loaded runner (the coverage job runs
// every package instrumented at once) must not turn it into a race with the
// SIGKILL, which ends bash without forwarding anything.
func TestCloseEndsInteractiveBashAndItsJob(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	old := hangupGrace
	hangupGrace = 20 * time.Second
	defer func() { hangupGrace = old }()
	// +H: no history expansion, which macOS's bash 3.2 applies to the $! below.
	p, err := Start(Cmd{Path: bash, Args: []string{"--norc", "--noprofile", "+H", "-i"},
		Env: append(os.Environ(), "PS1=$ ", "TERM=xterm-256color")})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var out bytes.Buffer
	var outMu sync.Mutex
	go func() {
		b := make([]byte, 1024)
		for {
			n, err := p.Read(b)
			outMu.Lock()
			out.Write(b[:n])
			outMu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	// The ids go to a file, so no echoed command line can be mistaken for
	// the answer.
	ids := filepath.Join(t.TempDir(), "job")
	fmt.Fprintf(p, "set -m; sleep 1000 & echo $! $(ps -o pgid= -p $!) > %s\n", ids)
	var job, pgid int
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(ids)
		if _, err := fmt.Sscan(string(b), &job, &pgid); err == nil {
			break
		}
		if time.Now().After(deadline) {
			outMu.Lock()
			defer outMu.Unlock()
			t.Fatalf("no job ids within 10s; the terminal showed %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pgid == p.Pid() {
		t.Fatalf("job %d shares bash's group %d; the cell needs job control", job, pgid)
	}
	start := time.Now()
	p.Close()
	took := time.Since(start)
	if code, _ := p.Wait(); code != 128+int(syscall.SIGHUP) {
		t.Fatalf("bash ended with %d after %v, want SIGHUP's %d (137 is the grace's SIGKILL: bash never took the hang-up)",
			code, took, 128+int(syscall.SIGHUP))
	}
	deadline = time.Now().Add(10 * time.Second)
	for unix.Kill(job, 0) == nil {
		if zombie(job) {
			break // ended, awaiting its new parent's reap
		}
		if time.Now().After(deadline) {
			unix.Kill(job, syscall.SIGKILL)
			t.Fatalf("job %d still running after the hang-up", job)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// zombie reports whether pid has exited but not been reaped.
func zombie(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && bytes.HasPrefix(bytes.TrimSpace(out), []byte("Z"))
}

func TestStartFailsForAMissingProgram(t *testing.T) {
	if _, err := Start(Cmd{Path: "/nonexistent/program"}); err == nil {
		t.Fatal("Start succeeded")
	}
}

func TestStartFailsForAMissingDir(t *testing.T) {
	if _, err := Start(Cmd{Path: "/bin/sh", Dir: "/nonexistent/dir"}); err == nil {
		t.Fatal("Start succeeded")
	}
}

func TestCloseUnblocksARead(t *testing.T) {
	p := startHelper(t, "sleep")
	readLines(p).await(t, "ready") // the scanner goroutine is now parked in Read
	done := make(chan struct{})
	go func() {
		io.Copy(io.Discard, p)
		close(done)
	}()
	p.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a Read outlived Close")
	}
}
