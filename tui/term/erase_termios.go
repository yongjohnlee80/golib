//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package term

import (
	"os"

	"golang.org/x/sys/unix"

	"github.com/yongjohnlee80/golib/tui/internal/fdctl"
)

// eraseByte is the byte the terminal's Backspace sends, as the tty's erase character says (stty
// erase): DEL (0x7f) on most terminals, ^H (0x08) on those that send it. Read once, before raw mode
// changes the tty; 0 when the tty cannot say.
func eraseByte(f *os.File) byte {
	var b byte
	_ = fdctl.Control(f, func(fd int) error {
		if t, err := unix.IoctlGetTermios(fd, ioctlGetTermios); err == nil {
			b = t.Cc[unix.VERASE]
		}
		return nil
	})
	return b
}
