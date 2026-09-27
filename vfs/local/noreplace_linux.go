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

// isNoReplaceUnsupported: errnos that only mean the kernel or filesystem lacks the flag. EINVAL is not
// among them — it also means "a directory moved into itself" — so commitFailed probes it.
func isNoReplaceUnsupported(err error) bool {
	return errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP)
}
