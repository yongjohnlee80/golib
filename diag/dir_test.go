//go:build unix

package diag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStartRefusesADirectoryOthersControl: Start refuses a directory in which another user could
// swap its files: a symlink in its place, or one group or others can write. A 0700 or a 0755
// directory of the user's own is used.
func TestStartRefusesADirectoryOthersControl(t *testing.T) {
	for _, mode := range []os.FileMode{0o700, 0o755} {
		d := shortDir(t)
		if err := os.Chmod(d, mode); err != nil {
			t.Fatal(err)
		}
		m, err := Start(context.Background(), d)
		if err != nil {
			t.Errorf("a %v directory of the user's own: %v", mode, err)
			continue
		}
		m.Close()
	}
	for _, mode := range []os.FileMode{0o770, 0o777, 0o702} {
		d := shortDir(t)
		if err := os.Chmod(d, mode); err != nil {
			t.Fatal(err)
		}
		if m, err := Start(context.Background(), d); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			if m != nil {
				m.Close()
			}
			t.Errorf("a %v directory: %v, want it refused as writable by others", mode, err)
		}
	}
	real := shortDir(t)
	link := filepath.Join(shortDir(t), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if m, err := Start(context.Background(), link); err == nil || !strings.Contains(err.Error(), "symlink") {
		if m != nil {
			m.Close()
		}
		t.Errorf("a symlink to a directory: %v, want it refused", err)
	}
}

// TestStartWritesThroughNoPlantedLink: a samples or lock file planted in the directory as a
// symlink to another file is refused, and the file it names is untouched.
func TestStartWritesThroughNoPlantedLink(t *testing.T) {
	for _, name := range []string{samplesName, lockName} {
		d := shortDir(t)
		victim := filepath.Join(shortDir(t), "victim")
		if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, filepath.Join(d, name)); err != nil {
			t.Fatal(err)
		}
		if m, err := Start(context.Background(), d); err == nil {
			m.Close()
			t.Errorf("%s planted as a symlink: Start wrote through it", name)
		}
		if b, _ := os.ReadFile(victim); string(b) != "keep" {
			t.Errorf("%s planted as a symlink: the file it names holds %q", name, b)
		}
	}
}

// TestTheSocketIsMovedIntoPlaceFinished: the socket reaches its path already 0600, bound in a
// private directory that is gone once Start returns.
func TestTheSocketIsMovedIntoPlaceFinished(t *testing.T) {
	d := shortDir(t)
	m, err := Start(context.Background(), d, WithSocket())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if fi, err := os.Lstat(m.Socket()); err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the socket at its path: %v %v, want a 0600 socket", fi, err)
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".s") {
			t.Errorf("the private directory the socket was bound in is left: %s", e.Name())
		}
	}
}
