//go:build linux

package term

// Regression gate for the blocking-stdin shutdown hang (db-tui finding
// #1, 2026-07-10): a tty inherited on stdin arrives in BLOCKING mode,
// where SetReadDeadline is a silent no-op — Stop's §2.9 unblock never
// landed, the pump stayed parked in read(2), and teardown hung on
// wg.Wait. The lifecycle tests miss this because they fake the tty
// pair with pipes, which are poller-registered and deadline-capable.
// This test uses a real pty with the slave opened the way a shell
// hands stdin to a child: a plain blocking fd.

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openBlockingPTY opens a real pty pair: master pollable (for
// draining), slave as a raw blocking fd with no poller registration —
// the exact shape of shell-inherited stdin.
func openBlockingPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		m.Close()
		t.Fatalf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		m.Close()
		t.Fatalf("ptsname: %v", err)
	}
	sfd, err := syscall.Open(fmt.Sprintf("/dev/pts/%d", n), syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Fatalf("open pts/%d: %v", n, err)
	}
	s := os.NewFile(uintptr(sfd), "pty-slave")
	t.Cleanup(func() {
		m.Close()
		s.Close()
	})
	return m, s
}

func TestStopUnblocksBlockingTTYReader(t *testing.T) {
	master, slave := openBlockingPTY(t)

	// Drain backend output (probe + teardown sequences) so writes to
	// the slave can never fill the pty buffer.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := master.Read(buf); err != nil {
				return
			}
		}
	}()

	b, err := Open(WithTTY(slave, slave),
		WithProbeTimeout(50*time.Millisecond), WithoutAltScreen())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The pump is parked in read(2) on the slave; nothing will arrive.
	// Stop must unblock and join it promptly.
	done := make(chan error, 1)
	go func() { done <- b.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop hung: blocked tty reader was never unblocked")
	}

	// The fallback path flips O_NONBLOCK on the caller's description;
	// teardown must restore it — the description may be shared with a
	// parent shell.
	fl, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("F_GETFL: %v", err)
	}
	if fl&unix.O_NONBLOCK != 0 {
		t.Error("teardown left the caller's fd in non-blocking mode")
	}

	// Ownership: the fallback wraps a DUP of the caller's fd, never the
	// fd itself — after Stop (and any backend-side finalization) the
	// caller's descriptor must still be usable. Teardown restored the
	// pty's original termios (canonical mode), so the write must carry
	// a newline for the read to complete; the watchdog turns a closed
	// or mangled fd into a failure instead of a suite timeout.
	runtime.GC() // provoke any wrongly-owning finalizer now, not later
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := slave.Read(buf)
		readDone <- err
	}()
	if _, err := master.WriteString("z\n"); err != nil {
		t.Fatalf("master write: %v", err)
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("caller's fd unusable after Stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("caller's fd unusable after Stop: read never completed")
	}
}

// TestStopPreservesOriginallyNonblockingFd covers the fallback on a
// caller fd that was ALREADY non-blocking (but not poller-registered,
// so deadlines still fail): teardown must restore the exact F_GETFL
// word it found, not force the flag off.
func TestStopPreservesOriginallyNonblockingFd(t *testing.T) {
	master, slave := openBlockingPTY(t)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := master.Read(buf); err != nil {
				return
			}
		}
	}()

	// Flip O_NONBLOCK behind the *os.File's back: the file was wrapped
	// while blocking, so it stays deadline-incapable — the fallback
	// path runs — but the description's flag word now includes
	// O_NONBLOCK on purpose.
	if err := syscall.SetNonblock(int(slave.Fd()), true); err != nil {
		t.Fatalf("SetNonblock: %v", err)
	}

	b, err := Open(WithTTY(slave, slave),
		WithProbeTimeout(50*time.Millisecond), WithoutAltScreen())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- b.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop hung")
	}

	fl, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("F_GETFL: %v", err)
	}
	if fl&unix.O_NONBLOCK == 0 {
		t.Error("teardown cleared O_NONBLOCK that the caller had set on purpose")
	}
}
