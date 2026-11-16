//go:build windows

package local_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/vfs/local"
)

// TestDirFSLetsASaveDeleteAFileItHolds: a file held open through DirFS, as a poller reads it, is
// deleted by a save that deletes it, and written anew; the holder still reads what it opened. The
// control: the same hold through os.Open refuses the delete, so the cell can tell the two apart.
// A rename over a held file is refused under both holds: Windows' rename replaces no file another
// process holds, however it is shared (DirFS's doc says so); if that changes, this cell says so.
func TestDirFSLetsASaveDeleteAFileItHolds(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dark.qml")
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	renameOver := func(hold string) {
		t.Helper()
		if err := os.WriteFile(file+".tmp", []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(file+".tmp", file); err == nil {
			t.Errorf("a rename over a file %s holds landed: Windows' rename limit has changed", hold)
		}
		_ = os.Remove(file + ".tmp")
	}

	osHeld, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	renameOver("os.Open")
	if err := os.Remove(file); err == nil {
		t.Fatal("the control: a delete of a file os.Open holds landed; the cell can't tell DirFS from it here")
	}
	osHeld.Close()

	held, err := local.DirFS(dir).Open("dark.qml")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	renameOver("DirFS")
	if err := os.Remove(file); err != nil {
		t.Fatalf("a delete of a file DirFS holds: %v", err)
	}
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatalf("writing the deleted file anew while it is held: %v", err)
	}
	if b, err := io.ReadAll(held); err != nil || string(b) != "v1" {
		t.Errorf("the holder read %q, %v; want what it opened", b, err)
	}
	if b, _ := os.ReadFile(file); string(b) != "v2" {
		t.Errorf("the file holds %q, want v2", b)
	}
}
