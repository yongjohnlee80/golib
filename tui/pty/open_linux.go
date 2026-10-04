package pty

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// open returns the master and the slave's path: TIOCSPTLCK unlocks the
// slave, TIOCGPTN names it.
func open() (*os.File, string, error) {
	fd, err := openMaster()
	if err != nil {
		return nil, "", err
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("unlock: %w", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		unix.Close(fd)
		return nil, "", fmt.Errorf("slave number: %w", err)
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), fmt.Sprintf("/dev/pts/%d", n), nil
}

// awaitExit blocks until pid has exited, without reaping it (WNOWAIT): the
// zombie keeps the pid, and the group id, from reuse until Wait reaps it.
func awaitExit(pid int) {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err != unix.EINTR {
			return
		}
	}
}
