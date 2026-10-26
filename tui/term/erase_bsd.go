//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package term

import "golang.org/x/sys/unix"

const ioctlGetTermios = unix.TIOCGETA
