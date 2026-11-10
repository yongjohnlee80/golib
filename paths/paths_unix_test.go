//go:build !windows

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// On Unix a backslash is part of a name, and a folder whose name ends in one is a real folder of its
// own: a path below it stays below it.
func TestAUnixFolderEndingInABackslashIsThatFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), `r\`)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := ParseLogical("a.md")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Under(OS(root), l)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(string(p), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.md")); err != nil {
		t.Errorf("Under = %q, which is not in the folder: %v", p, err)
	}
	home, err := Expand("~/a.md", root)
	if err != nil || home != p {
		t.Errorf("Expand = %q, %v; want %q", home, err, p)
	}
}
