//go:build windows

package term

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
	xterm "golang.org/x/term"

	"github.com/yongjohnlee80/golib/tui"
)

// Windows terminal plumbing (ADR-0002 §2.4). x/term's MakeRaw sets
// ENABLE_VIRTUAL_TERMINAL_INPUT on stdin — the console then encodes keys (and
// Windows Terminal encodes mouse/paste) as VT sequences into the stdin byte
// stream, so there is exactly one input parser on every platform. MakeRaw
// never touches stdout, so output VT processing is enabled here. Supported
// floor: Windows 10 1809+ (N4); std syscall lacks SetConsoleMode entirely,
// which is why x/sys is the sanctioned dependency (§2.11).

const (
	waitObject0 = 0x00000000
	waitTimeout = 0x00000102
)

func isTerminal(f *os.File) bool { return xterm.IsTerminal(int(f.Fd())) }

func makeRaw(f *os.File) (func() error, error) {
	fd := int(f.Fd())
	st, err := xterm.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return xterm.Restore(fd, st) }, nil
}

// enableOutputVT sets ENABLE_VIRTUAL_TERMINAL_PROCESSING and
// DISABLE_NEWLINE_AUTO_RETURN on the stdout handle, with the documented
// degradation retry: older builds reject DISABLE_NEWLINE_AUTO_RETURN with
// ERROR_INVALID_PARAMETER, so retry with VT processing alone; if even that
// fails, there is no non-VT rendering path — ErrConsoleTooOld (ADR-0002 §2.4).
func enableOutputVT(f *os.File) (func() error, error) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, errors.Join(ErrConsoleTooOld, err)
	}
	saved := mode
	both := mode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(h, both); err != nil {
		vtOnly := mode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if err2 := windows.SetConsoleMode(h, vtOnly); err2 != nil {
			return nil, errors.Join(ErrConsoleTooOld, err, err2)
		}
	}
	return func() error { return windows.SetConsoleMode(h, saved) }, nil
}

func fdSize(f *os.File) (tui.Size, error) {
	w, h, err := xterm.GetSize(int(f.Fd()))
	if err != nil {
		return tui.Size{}, err
	}
	return tui.Size{W: w, H: h}, nil
}

// unblockFile is a no-op on Windows: readFile waits on the console handle
// with a short bounded wait and re-checks done itself (ADR-0002 §2.9).
func unblockFile(*os.File) {}

// readFile waits for console input with a bounded wait, re-checking done
// between waits, then reads the VT byte stream. ReadConsoleInput is never
// mixed with stream reads on the same handle (ADR-0002 §2.8, §4.2).
func (b *Backend) readFile(p []byte) (int, error) {
	h := windows.Handle(b.inFile.Fd())
	for {
		select {
		case <-b.done:
			return 0, io.EOF
		default:
		}
		ev, err := windows.WaitForSingleObject(h, 100)
		if err != nil {
			return 0, err
		}
		switch ev {
		case waitObject0:
			return b.inFile.Read(p)
		case waitTimeout:
			// re-check done and wait again
		default:
			return 0, errors.New("term: unexpected console wait result")
		}
	}
}
