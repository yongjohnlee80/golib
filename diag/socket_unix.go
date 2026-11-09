//go:build unix

package diag

import (
	"errors"
	"os"
	"syscall"
)

// restrictBindDir has nothing to do: os.MkdirTemp makes the directory 0700.
func restrictBindDir(string) error { return nil }

// restrictSocket makes the bound socket 0600: only the user connects.
func restrictSocket(path string) error { return os.Chmod(path, 0o600) }

// connRefused is whether a dial found a socket nothing serves.
func connRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
