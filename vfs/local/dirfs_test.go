package local_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/vfs/local"
)

// TestDirFSIsAnFS: DirFS reads, lists and stats a tree as an io/fs.FS should (fstest.TestFS).
func TestDirFSIsAnFS(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"main.qml": "Window {}\n", "themes/dark.qml": "Theme {}\n", "ui/Card.qml": "Text {}\n"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the folders' times fixed once their files are in: NTFS updates a folder's time in its
	// parent's listing lazily, so a listing and a stat can differ by a fraction of a millisecond
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, d := range []string{"themes", "ui"} {
		if err := os.Chtimes(filepath.Join(dir, d), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := fstest.TestFS(local.DirFS(dir), "main.qml", "themes/dark.qml", "ui/Card.qml"); err != nil {
		t.Fatal(err)
	}
}
