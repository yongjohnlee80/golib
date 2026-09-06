//go:build windows

package term

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
	xterm "golang.org/x/term"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// waitWritable is a no-op on Windows: console handles write synchronously
// (blocking) so os.File.Write never returns EAGAIN and writeAll's loop only
// ever handles ordinary short writes.
func waitWritable(int) error { return nil }

// Windows terminal plumbing. x/term's MakeRaw sets
// ENABLE_VIRTUAL_TERMINAL_INPUT on stdin — the console then encodes keys (and
// Windows Terminal encodes mouse/paste) as VT sequences into the stdin byte
// stream, so there is exactly one input parser on every platform. MakeRaw
// never touches stdout, so output VT processing is enabled here. Supported
// floor: Windows 10 1809+ (N4); std syscall lacks SetConsoleMode entirely,
// which is why x/sys is the sanctioned dependency.

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
// fails, there is no non-VT rendering path — ErrConsoleTooOld.
func enableOutputVT(f *os.File) (func() error, error) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, errs.WrapCause(ErrConsoleTooOld, err, "term: reading the console mode")
	}
	saved := mode
	both := mode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(h, both); err != nil {
		vtOnly := mode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if err2 := windows.SetConsoleMode(h, vtOnly); err2 != nil {
			// err2 is the cause that matters: err is the EXPECTED rejection on an
			// older build (the documented degradation above), so the retry failing
			// is what leaves no rendering path. err is kept as detail rather than
			// as a second identity — joining all three would make errors.Is answer
			// true for a routine rejection as loudly as for the real failure.
			return nil, errs.WrapCause(ErrConsoleTooOld, err2,
				"term: enabling VT processing alone, after the combined-flags attempt was rejected with %v", err)
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
// with a short bounded wait and re-checks done itself.
func unblockFile(*os.File) {}

// makePollable is a no-op on Windows for the same reason — the bounded
// console wait never parks the pump in an unbreakable read.
func makePollable(f *os.File) (*os.File, func() error) { return f, nil }

// readFile waits for console input with a bounded wait, re-checking done
// between waits, then reads the VT byte stream. ReadConsoleInput is never
// mixed with stream reads on the same handle.
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
			return 0, fmt.Errorf("term: console wait returned the undocumented result %d (%w)", ev, errs.ErrFatal)
		}
	}
}
