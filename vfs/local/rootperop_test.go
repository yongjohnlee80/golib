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
	if err := f.MkdirAll(bg, "sub"); err != nil {
		t.Fatal(err)
	}
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

// replaceRootAt, at the hook's point, moves the root at dir aside (to dir-old) and puts a
// directory of files in its place, once: what a user's move and replacement do mid-operation.
// Windows may refuse the move while the operation holds the root's handle: then nothing is
// replaced, which confines the operation as well, and replaced reports false.
func replaceRootAt(t *testing.T, point, dir string, files map[string]string) (replaced *bool) {
	t.Helper()
	replaced = new(bool)
	t.Cleanup(func() { testHook = nil })
	testHook = func(p string) error {
		if p != point {
			return nil
		}
		testHook = nil
		if err := os.Rename(dir, dir+"-old"); err != nil {
			t.Logf("the root can't be moved while the operation holds it (%v): nothing replaced", err)
			return nil
		}
		for name, body := range files {
			full := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		*replaced = true
		return nil
	}
	return replaced
}

// rootDir is a fresh root directory, root, under a temporary folder of t's.
func rootDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A write under RootPerOperation goes through one root from its condition to its readback: the
// root replaced between its temporary file and its commit, the write lands in the root it began in
// (now moved aside), and the file it answers is the one it wrote, never the replacement's (the
// mutant: the readback through a root opened again).
func TestAWriteKeepsItsRootWhenTheRootIsReplaced(t *testing.T) {
	dir := rootDir(t)
	f := newPerOp(t, dir)
	replaced := replaceRootAt(t, "precommit", dir, map[string]string{"a.md": "replacement"})
	fi, err := f.WriteFile(bg, "a.md", strings.NewReader("new"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size != 3 {
		t.Errorf("the write answered a file of %d bytes, want its own 3", fi.Size)
	}
	if !*replaced {
		return
	}
	if b, err := os.ReadFile(filepath.Join(dir+"-old", "a.md")); err != nil || string(b) != "new" {
		t.Errorf("the root the write began in holds %q, %v; want the write", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "replacement" {
		t.Errorf("the replacement holds %q; want it untouched", b)
	}
}

// A rename across directories under RootPerOperation pins both parents through one root: the root
// replaced between the pins and the commit, the file moves within the root it began in, and the
// replacement is untouched (the mutant: the destination pinned through a root opened again).
func TestARenameKeepsItsRootWhenTheRootIsReplaced(t *testing.T) {
	dir := rootDir(t)
	f := newPerOp(t, dir)
	for _, d := range []string{"from", "to"} {
		if err := f.MkdirAll(bg, d); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, f, "from/a.md", "moved")
	replaced := replaceRootAt(t, "prerename", dir, map[string]string{"from/a.md": "replacement", "to/keep": ""})
	if err := f.Rename(bg, "from/a.md", "to/a.md"); err != nil {
		t.Fatal(err)
	}
	if !*replaced {
		return
	}
	if b, err := os.ReadFile(filepath.Join(dir+"-old", "to", "a.md")); err != nil || string(b) != "moved" {
		t.Errorf("the root the rename began in: to/a.md %q, %v; want the file moved", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "to", "a.md")); !os.IsNotExist(err) {
		t.Errorf("the replacement gained to/a.md (%v); want it untouched", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "from", "a.md")); string(b) != "replacement" {
		t.Errorf("the replacement's from/a.md is %q; want it untouched", b)
	}
}

// A conditional removal under RootPerOperation checks and removes through one root: the root
// replaced between the check and the removal, the entry checked is the one removed, and the
// replacement's entry of that name stays (the mutant: the removal through a root opened again).
func TestAConditionalRemoveKeepsItsRootWhenTheRootIsReplaced(t *testing.T) {
	dir := rootDir(t)
	f := newPerOp(t, dir)
	fi := mustWrite(t, f, "a.md", "checked")
	replaced := replaceRootAt(t, "preremove", dir, map[string]string{"a.md": "replacement"})
	if err := f.RemoveIf(bg, "a.md", fi.Version); err != nil {
		t.Fatal(err)
	}
	if !*replaced {
		return
	}
	if _, err := os.Stat(filepath.Join(dir+"-old", "a.md")); !os.IsNotExist(err) {
		t.Errorf("the entry checked is still there (%v); want it removed", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "replacement" {
		t.Errorf("the replacement's a.md is %q; want it untouched", b)
	}
}
