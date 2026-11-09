//go:build windows

package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Windows' local disk as an fs.FS: "." lists the drives, the system's among them; a folder is
// listed, statted and a file read through its drive path; a name the source would not write is
// refused, not opened.
func TestDrivesFSReadsWindowsLocalDisk(t *testing.T) {
	fsys := LocalFiles().FS
	sys := filepath.VolumeName(os.Getenv("SystemRoot"))
	ds, err := fs.ReadDir(fsys, ".")
	if err != nil || !slices.ContainsFunc(ds, func(d fs.DirEntry) bool { return d.Name() == sys && d.IsDir() }) {
		t.Fatalf("ReadDir(.) = %v, %v; want the system drive %s among the folders", ds, err, sys)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, ok := LocalFiles().fsPath(dir)
	if !ok {
		t.Fatalf("fsPath(%q) refused the temporary folder", dir)
	}
	if es, err := fs.ReadDir(fsys, p); err != nil || len(es) != 1 || es[0].Name() != "a.txt" {
		t.Fatalf("ReadDir(%q) = %v, %v; want a.txt", p, es, err)
	}
	if b, err := fs.ReadFile(fsys, p+"/a.txt"); err != nil || string(b) != "hi" {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
	if fi, err := fs.Stat(fsys, sys); err != nil || !fi.IsDir() {
		t.Fatalf("Stat(%s) = %v, %v; want a folder", sys, fi, err)
	}
	for _, name := range []string{p + "/a.txt:stream", "Users", "c:/Windows", "../x"} {
		if _, err := fsys.Open(name); err == nil {
			t.Errorf("Open(%q) opened a name the source never writes", name)
		}
	}
}
