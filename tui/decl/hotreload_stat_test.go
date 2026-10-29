package decl

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// countingFS is an fs.FS over dir that counts the files it reads.
type countingFS struct {
	fs.FS
	reads int
}

func (c *countingFS) ReadFile(name string) ([]byte, error) {
	c.reads++
	return fs.ReadFile(c.FS, name)
}

func (c *countingFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(c.FS, name) }

// TestAFollowedFileIsReadOnlyWhenItChanged: a file whose size and modification time hold is not
// read again; a new size is; an edit that keeps both is caught by the next full rehash.
func TestAFollowedFileIsReadOnlyWhenItChanged(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.qml")
	if err := os.WriteFile(file, []byte("Item {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfs := &countingFS{FS: os.DirFS(dir)}
	h := &fileHashes{polls: 1} // not a full-rehash poll
	first, err := h.sum(cfs, "main.qml", "k")
	if err != nil || cfs.reads != 1 {
		t.Fatalf("first sum: %v, %d reads", err, cfs.reads)
	}
	if again, _ := h.sum(cfs, "main.qml", "k"); again != first || cfs.reads != 1 {
		t.Fatalf("an unchanged file was read again (%d reads)", cfs.reads)
	}

	if err := os.WriteFile(file, []byte("Item { id: x }"), 0o600); err != nil {
		t.Fatal(err)
	}
	if grown, _ := h.sum(cfs, "main.qml", "k"); grown == first || cfs.reads != 2 {
		t.Fatalf("a file of a new size was not read again (%d reads)", cfs.reads)
	}

	// the same size, and the old modification time put back: only the full rehash sees it
	info, _ := os.Stat(file)
	if err := os.WriteFile(file, []byte("Item { id: y }"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	stale, _ := h.sum(cfs, "main.qml", "k")
	h.polls = rehashEvery
	fresh, _ := h.sum(cfs, "main.qml", "k")
	if fresh == stale {
		t.Fatal("the full rehash did not see a same-size edit with its modification time kept")
	}
}

// TestAFileSystemWithoutModTimesIsReadEveryPoll: an in-memory or embedded file system reports no
// modification time, so nothing tells an edit apart: every poll reads it, as before.
func TestAFileSystemWithoutModTimesIsReadEveryPoll(t *testing.T) {
	m := fstest.MapFS{"main.qml": {Data: []byte("Item {}")}}
	cfs := &countingFS{FS: m}
	h := &fileHashes{polls: 1}
	for range 3 {
		if _, err := h.sum(cfs, "main.qml", "k"); err != nil {
			t.Fatal(err)
		}
	}
	if cfs.reads != 3 {
		t.Fatalf("%d reads of a file with no modification time over 3 polls, want 3", cfs.reads)
	}
}
