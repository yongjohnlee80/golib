//go:build linux || darwin

package local

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// isRenameInvalid: an EINVAL from a no-replace rename, which commitFailed probes to tell an
// unsupported flag from a move into itself.
func isRenameInvalid(err error) bool { return errors.Is(err, unix.EINVAL) }

// readFlags open a file for reading. O_NONBLOCK: opening a FIFO for reading waits for a writer, and
// with it the open returns at once, to be refused after. A regular file reads the same either way.
const readFlags = os.O_RDONLY | unix.O_NONBLOCK
