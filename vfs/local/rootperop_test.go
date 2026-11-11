//go:build linux || darwin || windows

package local

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/vfstest"
)

// The driver behaves the same with RootPerOperation: the whole conformance suite.
func TestConformanceRootPerOperation(t *testing.T) {
	vfstest.TestFS(t, func(t *testing.T) vfs.FS {
		f, err := New(t.TempDir(), RootPerOperation())
		if err != nil {
			t.Fatal(err)
		}
		return f
	})
}

// newPerOp is newLocal with RootPerOperation, over dir.
func newPerOp(t *testing.T, dir string) *FS {
	t.Helper()
	f, err := New(dir, RootPerOperation())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// descriptorsUnder is this process's descriptors whose target is dir or under it, where the OS
// lists them (Linux's /proc/self/fd); nil elsewhere, where a rename of dir says it instead.
func descriptorsUnder(t *testing.T, dir string) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		return nil
	}
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
		if err == nil && (target == dir || strings.HasPrefix(target, dir+string(filepath.Separator))) {
			out = append(out, fd.Name()+" → "+target)
		}
	}
	return out
}

// With RootPerOperation the root is held by nothing between operations: after a write, a read, a
// listing, a stat and a remove, no descriptor names it (Linux), and it can be renamed (every OS:
// Windows refuses to rename a directory an open handle holds). Once moved, it is not reached; a
// directory put at its path is (the mutant: the root held from New).
func TestARootPerOperationIsHeldByNothingBetweenOperations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := newPerOp(t, dir)
	mustWrite(t, f, "sub/a.txt", "one")
	if got := readAll(t, f, "sub/a.txt"); got != "one" {
		t.Fatalf("read %q", got)
	}
	if _, err := f.ReadDir(bg, "sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat(bg, "sub/a.txt"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, "b.txt", "two")
	if err := f.Remove(bg, "b.txt"); err != nil {
		t.Fatal(err)
	}
	if held := descriptorsUnder(t, dir); len(held) != 0 {
		t.Fatalf("between operations the process holds %v", held)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatalf("the root can't be renamed between operations: %v", err)
	}
	if _, err := f.Stat(bg, "sub/a.txt"); err == nil {
		t.Fatal("the moved root was still reached")
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, "c.txt", "three")
	if b, err := os.ReadFile(filepath.Join(dir, "c.txt")); err != nil || string(b) != "three" {
		t.Fatalf("the directory put at the root's path: %q, %v", b, err)
	}
}
