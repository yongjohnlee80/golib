//go:build darwin

package local

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameNoReplace is renameatx_np(2) with RENAME_EXCL: atomic, and EEXIST if to exists.
func (p *parent) renameNoReplace(from string, dst *parent, to string) error {
	return unix.RenameatxNp(p.fd, from, dst.fd, to, unix.RENAME_EXCL)
}

// isNoReplaceUnsupported: the filesystem lacks the flag.
func isNoReplaceUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL)
}
