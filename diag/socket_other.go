//go:build !unix && !windows

package diag

import (
	"errors"
	"syscall"
)

// restrictBindDir has nothing to do where unix sockets are not offered: Listen fails first.
func restrictBindDir(string) error { return nil }

func restrictSocket(string) error { return nil }

func connRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
