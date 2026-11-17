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

// TestAFileSystemWithoutModTimesIsReadOnlyWhenItsSizeChanges: an in-memory file system reports no
// modification time, so between full rehashes its files are judged by their size: an idle poll
// reads nothing, a new size is read at once, and an edit that keeps the size is seen at the next
// full rehash, as a disk's edit within its time resolution is.
func TestAFileSystemWithoutModTimesIsReadOnlyWhenItsSizeChanges(t *testing.T) {
	m := fstest.MapFS{"main.qml": {Data: []byte("Item {}")}}
	cfs := &countingFS{FS: m}
	h := &fileHashes{polls: 1}
	first, _ := h.sum(cfs, "main.qml", "k")
	for range 3 {
		if again, _ := h.sum(cfs, "main.qml", "k"); again != first {
			t.Fatal("an unchanged file's hash moved")
		}
	}
	if cfs.reads != 1 {
		t.Fatalf("%d reads of an unchanged file with no modification time over 4 polls, want 1", cfs.reads)
	}
	m["main.qml"] = &fstest.MapFile{Data: []byte("Item { id: x }")}
	if grown, _ := h.sum(cfs, "main.qml", "k"); grown == first || cfs.reads != 2 {
		t.Fatalf("a new size was not read at once (%d reads)", cfs.reads)
	}
	m["main.qml"] = &fstest.MapFile{Data: []byte("Item { id: y }")}
	stale, _ := h.sum(cfs, "main.qml", "k")
	h.polls = rehashEvery
	if fresh, _ := h.sum(cfs, "main.qml", "k"); fresh == stale {
		t.Fatal("the full rehash did not see a same-size edit")
	}
}

// touchingFS counts every way a poll can touch its files: opens, reads, listings and stats.
type touchingFS struct {
	fs.FS
	touches int
}

func (c *touchingFS) Open(name string) (fs.File, error) { c.touches++; return c.FS.Open(name) }
func (c *touchingFS) ReadFile(name string) ([]byte, error) {
	c.touches++
	return fs.ReadFile(c.FS, name)
}
func (c *touchingFS) ReadDir(name string) ([]fs.DirEntry, error) {
	c.touches++
	return fs.ReadDir(c.FS, name)
}
func (c *touchingFS) Stat(name string) (fs.FileInfo, error) { c.touches++; return fs.Stat(c.FS, name) }

// TestAStaticSourceIsNeverTouchedByThePoll: a Program follows a Static source (an embedded
// module) and another: over many polls, rehashes among them, the Static one's files are never
// opened, listed, read or stat'd, while an edit to the other still changes the snapshot.
func TestAStaticSourceIsNeverTouchedByThePoll(t *testing.T) {
	embedded := &touchingFS{FS: fstest.MapFS{"ui/Card.qml": {Data: []byte("Text {}")}, "ui/Ask.qml": {Data: []byte("Text {}")}}}
	user := fstest.MapFS{"themes/dark.qml": {Data: []byte(`Theme { bg: "blue" }`)}}
	c := programConfig{watched: []watchedDir{{fsys: Static(embedded), dir: "ui"}, {fsys: user, dir: "themes"}}}
	h := &fileHashes{}
	first := snapshotOf(c, h)
	for range 3 * rehashEvery {
		if !snapshotOf(c, h).equal(first) {
			t.Fatal("an idle poll changed the snapshot")
		}
	}
	if embedded.touches != 0 {
		t.Fatalf("the Static source was touched %d times over %d polls, want none", embedded.touches, 3*rehashEvery+1)
	}
	user["themes/dark.qml"] = &fstest.MapFile{Data: []byte(`Theme { bg: "green" }`)}
	if snapshotOf(c, h).equal(first) {
		t.Fatal("an edit to the followed source did not change the snapshot")
	}
	if embedded.touches != 0 {
		t.Fatalf("the Static source was touched %d times, want none", embedded.touches)
	}
}

// TestAnIdlePollReadsNoBytesOfAStampedFile: a followed file on disk that does not change is
// stat'd, never read, by the polls between full rehashes; each full rehash reads it once.
func TestAnIdlePollReadsNoBytesOfAStampedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "themes", "dark.qml"), []byte(`Theme { bg: "blue" }`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfs := &countingFS{FS: os.DirFS(dir)}
	c := programConfig{watched: []watchedDir{{fsys: cfs, dir: "themes"}}}
	h := &fileHashes{}
	for range 2 * rehashEvery {
		snapshotOf(c, h)
	}
	if cfs.reads != 2 { // polls 0 and rehashEvery are full rehashes
		t.Fatalf("%d reads of an unchanged file over %d polls, want the 2 full rehashes'", cfs.reads, 2*rehashEvery)
	}
}
