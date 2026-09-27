//go:build linux

package local

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameNoReplace is renameat2(2) with RENAME_NOREPLACE: atomic, and EEXIST if to exists.
func (p *parent) renameNoReplace(from string, dst *parent, to string) error {
	return unix.Renameat2(p.fd, from, dst.fd, to, unix.RENAME_NOREPLACE)
}

// isNoReplaceUnsupported: the kernel or filesystem lacks the flag. (A directory moved into itself also
// gives EINVAL; rename refuses that case before the syscall.)
func isNoReplaceUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP)
}
