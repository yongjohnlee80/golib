//go:build linux || darwin

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// hangupGrace is how long Close waits after the hang-up before it kills the
// child's group. Tests shorten it.
var hangupGrace = 2 * time.Second

// PTY is a running program on a pseudo-terminal: the master side of the
// pair, and the child.
type PTY struct {
	master *os.File
	cmd    *exec.Cmd
	pid    int
	grace  time.Duration

	// mu orders signals against the child's reaping: while exited is false
	// the child's pid (its group's id) is still held by the child or its
	// zombie, so a signal to the group cannot reach a stranger.
	mu     sync.Mutex
	exited bool

	done    chan struct{} // closed once the child is reaped
	code    int
	waitErr error

	closeOnce sync.Once
	closeErr  error
}

// Start opens a pseudo-terminal of c's size and starts c on it, as a
// session leader with the terminal as its controlling tty.
func Start(c Cmd) (*PTY, error) {
	master, slaveName, err := open()
	if err != nil {
		return nil, fmt.Errorf("pty: open: %w", err)
	}
	rows, cols := c.size()
	if err := setSize(master, rows, cols); err != nil {
		master.Close()
		return nil, err
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("pty: open %s: %w", slaveName, err)
	}
	defer slave.Close() // the child has its own copies

	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = c.Env
	cmd.Dir = c.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Ctty is a descriptor in the child: its stdin, the terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, fmt.Errorf("pty: start %s: %w", c.Path, err)
	}
	p := &PTY{
		master: master,
		cmd:    cmd,
		pid:    cmd.Process.Pid,
		grace:  hangupGrace,
		done:   make(chan struct{}),
	}
	go p.reap()
	return p, nil
}

// reap waits for the child. It marks the child exited before reaping it, so
// a concurrent Signal never targets a pid the kernel may have reused.
func (p *PTY) reap() {
	awaitExit(p.pid)
	p.mu.Lock()
	p.exited = true
	p.mu.Unlock()
	err := p.cmd.Wait()
	var ee *exec.ExitError
	switch {
	case err == nil:
		p.code = 0
	case errors.As(err, &ee):
		p.code = exitCode(ee.ProcessState)
	default:
		p.code, p.waitErr = -1, err
	}
	close(p.done)
}

// exitCode is the child's exit status as a shell reports it: the code, or
// 128 + the signal that ended it.
func exitCode(ps *os.ProcessState) int {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}

// Read reads the program's output. Once the program and everything else
// holding the terminal have closed it, Read returns io.EOF.
func (p *PTY) Read(b []byte) (int, error) {
	n, err := p.master.Read(b)
	if errors.Is(err, syscall.EIO) {
		// Linux reports a hung-up slave as EIO.
		err = io.EOF
	}
	return n, err
}

// Write sends b to the program as typed input.
func (p *PTY) Write(b []byte) (int, error) {
	return p.master.Write(b)
}

// Resize sets the terminal's size; the child's foreground group gets
// SIGWINCH.
func (p *PTY) Resize(rows, cols int) error {
	if rows <= 0 || cols <= 0 {
		return fmt.Errorf("pty: resize to %dx%d", rows, cols)
	}
	return setSize(p.master, rows, cols)
}

// Signal sends sig to the child's process group. After the child has
// exited it returns os.ErrProcessDone.
func (p *PTY) Signal(sig os.Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("pty: signal %v is not a syscall.Signal", sig)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return os.ErrProcessDone
	}
	return unix.Kill(-p.pid, s)
}

// Pid is the child's process id, which is also its process group's and its
// session's.
func (p *PTY) Pid() int { return p.pid }

// Wait blocks until the child has exited and been reaped, and returns its
// exit status as a shell reports it: the exit code, or 128 + the signal
// that ended it. err is set only when waiting itself failed.
func (p *PTY) Wait() (exit int, err error) {
	<-p.done
	return p.code, p.waitErr
}

// Close hangs up: SIGHUP and SIGCONT to the child's group, then the master
// closes. A child still running after the grace period has its group
// killed. Close returns once the child is reaped, and is idempotent.
func (p *PTY) Close() error {
	p.closeOnce.Do(func() {
		p.Signal(syscall.SIGHUP)
		p.Signal(syscall.SIGCONT)
		p.closeErr = p.master.Close()
		t := time.NewTimer(p.grace)
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
			p.Signal(syscall.SIGKILL)
			<-p.done
		}
	})
	return p.closeErr
}

// setSize applies TIOCSWINSZ through the descriptor without taking it out
// of the runtime poller (File.Fd would make it blocking, and a blocked Read
// would then outlive Close).
func setSize(f *os.File, rows, cols int) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return fmt.Errorf("pty: resize: %w", err)
	}
	ws := &unix.Winsize{Row: uint16(rows), Col: uint16(cols)}
	var ioErr error
	if err := rc.Control(func(fd uintptr) {
		ioErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, ws)
	}); err != nil {
		return fmt.Errorf("pty: resize: %w", err)
	}
	if ioErr != nil {
		return fmt.Errorf("pty: resize: %w", ioErr)
	}
	return nil
}

// openMaster opens /dev/ptmx non-blocking, so the returned file is
// registered with the runtime poller and Close interrupts a Read.
func openMaster() (int, error) {
	return unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
}
