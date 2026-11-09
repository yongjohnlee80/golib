//go:build unix

package diag

import (
	"os"
	"testing"
)

// userOnly fails unless only the user can open the socket at path: mode 0600.
func userOnly(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the socket's mode: %v, %v; want 0600", fi.Mode().Perm(), err)
	}
}
