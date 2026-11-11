//go:build linux || (darwin && cgo) || windows

package local

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/vfs"
)

// A watch under RootPerOperation keeps only its own hold on the directory it watches (its handle,
// or its inotify or FSEvents watch), nothing on the root: while it runs and reports, the root, the
// watched directory's parent, can be renamed and removed on every OS (Windows refuses either while
// a root holds the directory), and on Linux no descriptor names it (the mutant: the root held).
func TestAWatchUnderRootPerOperationLeavesItsRootFree(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.MkdirAll(filepath.Join(parent, "w"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := newPerOp(t, parent)
	events, _ := startWatch(t, f, "w")
	mustWrite(t, f, "w/a.md", "1")
	await(t, events, "w/a.md", vfs.OpCreate)
	if held := descriptorsUnder(t, parent); len(held) != 0 {
		t.Fatalf("while the watch runs the process holds %v", held)
	}
	moved := parent + "-moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatalf("the watched directory's parent can't be renamed while the watch runs: %v", err)
	}
	if err := os.RemoveAll(moved); err != nil {
		t.Fatalf("the watched directory's parent can't be removed while the watch runs: %v", err)
	}
}

// The watched directory's own removal still ends a RootPerOperation watch as it ends any: a final
// OpOverflow{""}, then the close.
func TestARootPerOperationWatchsDirectoryGoneEndsIt(t *testing.T) {
	parent := t.TempDir()
	f := newPerOp(t, parent)
	if err := f.MkdirAll(bg, "w/sub"); err != nil {
		t.Fatal(err)
	}
	events, _ := startWatch(t, f, "w")
	if err := os.RemoveAll(filepath.Join(parent, "w")); err != nil {
		t.Fatal(err)
	}
	seen := drainClosed(t, events)
	if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
		t.Fatalf("last event = %v, want OpOverflow{\"\"}", seen)
	}
}
