//go:build !linux && !darwin

package pty

import (
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/errs"
)

// PTY is a running program on a pseudo-terminal. On this platform Start
// always fails, so no PTY exists.
type PTY struct{}

// Start returns an error matching errs.ErrUnsupported: pseudo-terminals
// are supported on Linux and macOS (Windows ConPTY is a later ADR).
func Start(Cmd) (*PTY, error) {
	return nil, fmt.Errorf("pty: %w on this platform", errs.ErrUnsupported)
}

func (*PTY) Read([]byte) (int, error)  { return 0, errs.ErrUnsupported }
func (*PTY) Write([]byte) (int, error) { return 0, errs.ErrUnsupported }
func (*PTY) Resize(int, int) error     { return errs.ErrUnsupported }
func (*PTY) Signal(os.Signal) error    { return errs.ErrUnsupported }
func (*PTY) Pid() int                  { return 0 }
func (*PTY) Wait() (int, error)        { return -1, errs.ErrUnsupported }
func (*PTY) Close() error              { return nil }
