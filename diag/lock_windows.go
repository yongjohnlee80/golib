//go:build windows

package diag

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// dirLock is a monitor's ownership of its directory: an exclusive LockFileEx lock, held until
// release and released by the kernel if the process dies, so a crash never leaves a stale owner,
// as flock does on Unix.
type dirLock struct{ f *os.File }

// lockDir takes path's lock, or refuses: another monitor owns the directory.
func lockDir(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("diag: %w", err)
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, fmt.Errorf("diag: another monitor owns %s", path)
		}
		return nil, fmt.Errorf("diag: locking %s: %w", path, err)
	}
	return &dirLock{f: f}, nil
}

// release gives the directory up; the lock file stays, unlocked, for the next monitor. The lock
// is undone explicitly before the close: Windows may unlock a closed handle's lock only later, and
// a Start right after Close must find the directory free.
func (l *dirLock) release() {
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, new(windows.Overlapped))
	l.f.Close()
}
