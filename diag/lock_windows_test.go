//go:build windows

package diag

import (
	"path/filepath"
	"strings"
	"testing"
)

// A directory's lock on Windows: taken, refused to a second taker while held, and free again the
// moment it is released, every time.
func TestTheWindowsLockIsFreeTheMomentItIsReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), lockName)
	for i := range 20 {
		l, err := lockDir(path)
		if err != nil {
			t.Fatalf("round %d: taking a free lock: %v", i, err)
		}
		if l2, err := lockDir(path); err == nil || !strings.Contains(err.Error(), "another monitor owns") {
			if l2 != nil {
				l2.release()
			}
			t.Fatalf("round %d: a held lock taken again: %v", i, err)
		}
		l.release()
	}
}
