//go:build !windows

package term

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	xterm "golang.org/x/term"

	"github.com/yongjohnlee80/golib/tui"
)

// Unix terminal plumbing: x/term's MakeRaw/Restore/GetSize
// replace per-GOOS ioctl code with the canonical implementation.
// MakeRaw clears OPOST (irrelevant: the emitter cursor-addresses everything)
// and ISIG — Ctrl+C arrives as byte 0x03 and is a KeyEvent, not a signal.

func isTerminal(f *os.File) bool { return xterm.IsTerminal(int(f.Fd())) }

func makeRaw(f *os.File) (func() error, error) {
	fd := int(f.Fd())
	st, err := xterm.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return xterm.Restore(fd, st) }, nil
}

// enableOutputVT is a no-op on unix: VT processing is unconditional.
func enableOutputVT(*os.File) (func() error, error) { return nil, nil }

func fdSize(f *os.File) (tui.Size, error) {
	w, h, err := xterm.GetSize(int(f.Fd()))
	if err != nil {
		return tui.Size{}, err
	}
	return tui.Size{W: w, H: h}, nil
}

// waitWritable blocks until fd is writable, so writeAll can resume after an
// EAGAIN on a non-blocking output description instead of dropping the frame's
// tail. EINTR retries; the 1s poll timeout just re-arms (a
// paused terminal is not an error).
func waitWritable(fd int) error {
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
	for {
		n, err := unix.Poll(pfd, 1000)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		if n > 0 {
			return nil
		}
	}
}

// unblockFile unblocks a pending read during Stop: a read deadline in the
// past is valid for pollable ttys. makePollable guarantees
// pollability at Start, so the deadline lands.
func unblockFile(f *os.File) { _ = f.SetReadDeadline(time.Now()) }

// makePollable hands the input fd to the runtime poller so the
// read-deadline unblock works. A tty inherited on stdin arrives in
// BLOCKING mode: SetReadDeadline is a silent no-op there
// (os.ErrNoDeadline), Stop's unblock never lands, the pump stays
// parked in read(2), and teardown hangs on wg.Wait.
//
// Three cases, in order:
//  1. f already takes deadlines (fresh os.OpenFile ttys, pipes) —
//     returned as is.
//  2. f is this process's controlling terminal: read through a
//     private non-blocking /dev/tty description instead. No
//     shared-description side effects; cleanup closes it.
//  3. Fallback: duplicate f's fd (the dup is ours to close — wrapping
//     the caller's own fd in a second *os.File would let the backend
//     side close/finalize a descriptor it never owned), flip
//     O_NONBLOCK, and re-wrap the dup so os.NewFile registers it with
//     the poller. O_NONBLOCK lives on the file description, which the
//     dup shares — cleanup restores the exact F_GETFL word found here
//     (the caller's fd may have been nonblocking on purpose) and then
//     closes the dup. While the flag is flipped, writes through a
//     shared stdout could observe EAGAIN; the /dev/tty path keeps
//     that case exotic.
//
// Ordering contract: call after makeRaw (raw mode is per-device, so it
// covers every description of the terminal), and never call Fd() on
// the returned file — os.File.Fd() reverts the fd to blocking mode,
// which resurrects the hang.
func makePollable(f *os.File) (*os.File, func() error) {
	if f.SetReadDeadline(time.Time{}) == nil {
		return f, nil
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
		if sameDevice(f, tty) && tty.SetReadDeadline(time.Time{}) == nil {
			return tty, tty.Close
		}
		_ = tty.Close()
	}
	fd := int(f.Fd())
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return f, nil
	}
	// F_DUPFD_CLOEXEC: atomic close-on-exec duplication — a separate
	// Dup + CloseOnExec would leave a fork/exec race window.
	dup, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return f, nil
	}
	if err := syscall.SetNonblock(dup, true); err != nil {
		_ = syscall.Close(dup)
		return f, nil
	}
	nf := os.NewFile(uintptr(dup), f.Name())
	if nf == nil || nf.SetReadDeadline(time.Time{}) != nil {
		_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags)
		if nf != nil {
			_ = nf.Close()
		} else {
			_ = syscall.Close(dup)
		}
		return f, nil
	}
	return nf, func() error {
		_, ferr := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags)
		return errors.Join(ferr, nf.Close())
	}
}

// sameDevice reports whether a and b refer to the same device
// (st_rdev equality).
func sameDevice(a, b *os.File) bool {
	sa, err := a.Stat()
	if err != nil {
		return false
	}
	sb, err := b.Stat()
	if err != nil {
		return false
	}
	ra, aok := sa.Sys().(*syscall.Stat_t)
	rb, bok := sb.Sys().(*syscall.Stat_t)
	return aok && bok && ra.Rdev == rb.Rdev
}

// readFile reads from the real terminal fd; Stop unblocks it via the read
// deadline, which the pump classifies as a clean exit once done is closed.
func (b *Backend) readFile(p []byte) (int, error) { return b.inFile.Read(p) }
