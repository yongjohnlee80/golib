//go:build !windows

package term

import (
	"os"
	"syscall"
	"time"

	xterm "golang.org/x/term"

	"github.com/yongjohnlee80/golib/tui"
)

// Unix terminal plumbing (ADR-0002 §2.4): x/term's MakeRaw/Restore/GetSize
// replace per-GOOS ioctl code with the canonical implementation (§2.11).
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

// unblockFile unblocks a pending read during Stop: a read deadline in the
// past is valid for pollable ttys (ADR-0002 §2.9). makePollable guarantees
// pollability at Start, so the deadline lands.
func unblockFile(f *os.File) { _ = f.SetReadDeadline(time.Now()) }

// makePollable hands the input fd to the runtime poller so the §2.9
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
//  3. Fallback: flip O_NONBLOCK on f's own description and re-wrap it
//     so os.NewFile registers with the poller; cleanup restores the
//     flag — the description may be shared with the parent shell.
//     (While the flag is on, writes through a shared stdout could
//     observe EAGAIN; the /dev/tty path keeps that case exotic.)
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
	if err := syscall.SetNonblock(fd, true); err != nil {
		return f, nil
	}
	nf := os.NewFile(uintptr(fd), f.Name())
	if nf == nil || nf.SetReadDeadline(time.Time{}) != nil {
		_ = syscall.SetNonblock(fd, false)
		return f, nil
	}
	return nf, func() error { return syscall.SetNonblock(fd, false) }
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
