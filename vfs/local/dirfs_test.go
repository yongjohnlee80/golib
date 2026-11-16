package local_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

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
	if err := fstest.TestFS(local.DirFS(dir), "main.qml", "themes/dark.qml", "ui/Card.qml"); err != nil {
		t.Fatal(err)
	}
}
