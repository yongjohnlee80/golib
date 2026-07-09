//go:build !windows

package term

import (
	"os"
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
// past is valid for pollable ttys (ADR-0002 §2.9).
func unblockFile(f *os.File) { _ = f.SetReadDeadline(time.Now()) }

// readFile reads from the real terminal fd; Stop unblocks it via the read
// deadline, which the pump classifies as a clean exit once done is closed.
func (b *Backend) readFile(p []byte) (int, error) { return b.inFile.Read(p) }
