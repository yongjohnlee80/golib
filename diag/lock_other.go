//go:build !unix

package diag

import (
	"errors"
	"fmt"
	"os"
)

// dirLock is a monitor's ownership of its directory where flock is not offered: a lock file
// created exclusively and removed on release. A crash leaves it behind, and the next Start names
// it to delete.
type dirLock struct{ path string }

// lockDir creates path, or refuses: another monitor owns the directory, or one ended without
// releasing it.
func lockDir(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("diag: another monitor owns %s, or one ended without releasing it: delete the file if none runs", path)
		}
		return nil, fmt.Errorf("diag: %w", err)
	}
	f.Close()
	return &dirLock{path: path}, nil
}

// release gives the directory up.
func (l *dirLock) release() { os.Remove(l.path) }
