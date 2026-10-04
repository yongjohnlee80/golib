package pty

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// open returns the master and the slave's path: TIOCPTYGRANT and
// TIOCPTYUNLK are grantpt and unlockpt, TIOCPTYGNAME is ptsname.
func open() (*os.File, string, error) {
	fd, err := openMaster()
	if err != nil {
		return nil, "", err
	}
	if err := ioctl(fd, unix.TIOCPTYGRANT, 0); err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("grant: %w", err)
	}
	if err := ioctl(fd, unix.TIOCPTYUNLK, 0); err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("unlock: %w", err)
	}
	var name [128]byte // the size TIOCPTYGNAME encodes
	if err := ioctl(fd, unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("slave name: %w", err)
	}
	if i := bytes.IndexByte(name[:], 0); i >= 0 {
		return os.NewFile(uintptr(fd), "/dev/ptmx"), string(name[:i]), nil
	}
	unix.Close(fd)
	return nil, "", fmt.Errorf("slave name: not terminated")
}

func ioctl(fd int, req uint, arg uintptr) error {
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), arg); e != 0 {
		return e
	}
	return nil
}

// awaitExit cannot wait without reaping on macOS (x/sys has no waitid), so
// it reports false and the child is marked exited once Wait reaps it.
// Between the reap and the mark a Signal could reach a reused pid only if a
// new process took the pid and made it a group id in that instant.
func awaitExit(int) bool { return false }
