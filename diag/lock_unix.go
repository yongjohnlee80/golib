//go:build unix

package diag

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// dirLock is a monitor's ownership of its directory: an exclusive flock, held until release and
// released by the kernel if the process dies, so a crash never leaves a stale owner.
type dirLock struct{ f *os.File }

// lockDir takes path's lock, or refuses: another monitor owns the directory.
func lockDir(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|noFollow, 0o600)
	if err != nil {
		return nil, fmt.Errorf("diag: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("diag: another monitor owns %s", path)
		}
		return nil, fmt.Errorf("diag: locking %s: %w", path, err)
	}
	return &dirLock{f: f}, nil
}

// release gives the directory up; the lock file stays, unlocked, for the next monitor.
func (l *dirLock) release() { l.f.Close() }
