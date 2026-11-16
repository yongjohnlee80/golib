//go:build windows

package local_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/vfs/local"
)

// TestDirFSLetsASaveReplaceOrDeleteAFileItHolds: a file held open through DirFS, as a poller reads
// it, is replaced by a save that renames over it, and deleted and written anew; the holder still reads
// what it opened. The control: the same hold through os.Open refuses the rename, so the cell can
// tell the two apart on this machine.
func TestDirFSLetsASaveReplaceOrDeleteAFileItHolds(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dark.qml")
	save := func(text string) error {
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return os.Rename(tmp, file)
	}
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	osHeld, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := save("v2"); err == nil {
		t.Fatal("the control: a rename over a file os.Open holds landed; the cell can't tell DirFS from it here")
	}
	osHeld.Close()
	_ = os.Remove(file + ".tmp")

	held, err := local.DirFS(dir).Open("dark.qml")
	if err != nil {
		t.Fatal(err)
	}
	if err := save("v3"); err != nil {
		t.Fatalf("a rename over a file DirFS holds: %v", err)
	}
	if b, err := io.ReadAll(held); err != nil || string(b) != "v1" {
		t.Errorf("the holder read %q, %v; want what it opened", b, err)
	}
	held.Close()
	if b, _ := os.ReadFile(file); string(b) != "v3" {
		t.Errorf("after the save the file holds %q, want v3", b)
	}

	held, err = local.DirFS(dir).Open("dark.qml")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := os.Remove(file); err != nil {
		t.Fatalf("a delete of a file DirFS holds: %v", err)
	}
	// a save that deletes, then writes the file anew, as some editors do
	if err := os.WriteFile(file, []byte("v4"), 0o644); err != nil {
		t.Errorf("writing the deleted file anew while it is held: %v", err)
	}
}
