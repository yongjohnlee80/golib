//go:build windows

package local

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yongjohnlee80/golib/vfs"
)

// junction makes link a junction to target, as mklink /J does.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
}

// A write's directory, pinned by its handle, keeps the write in place: the directory renamed and a
// junction to a folder outside the root swapped in on its path between the temp file and the
// commit, the commit still lands in the directory pinned (now under its new name), never outside.
func TestAWriteLandsInItsPinnedDirectoryWhenAJunctionIsSwappedIn(t *testing.T) {
	bothRoots(t, writeLandsInItsPinnedDirectory)
}

func writeLandsInItsPinnedDirectory(t *testing.T, f *FS, dir string) {
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testHook = nil })
	landed := filepath.Join(dir, "moved", "a.md")
	testHook = func(p string) error {
		if p != "precommit" {
			return nil
		}
		testHook = nil
		// A handle open in the directory may keep Windows from moving it at all; then nothing can
		// be swapped in, which confines the write as well, and it lands where it began.
		if err := os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "moved")); err != nil {
			t.Logf("the pinned directory can't be moved while the write runs (%v): nothing to swap", err)
			landed = filepath.Join(dir, "d", "a.md")
			return nil
		}
		junction(t, filepath.Join(dir, "d"), outside)
		return nil
	}
	// The commit lands in the pinned directory. After it, looking the path up again meets the
	// junction, which the root refuses to follow out: the write reports itself committed, which is
	// the truth, rather than reading anything outside.
	if _, err := f.WriteFile(bg, "d/a.md", strings.NewReader("pinned")); err != nil {
		var ce *vfs.CommitError
		if !errors.As(err, &ce) {
			t.Fatalf("WriteFile: %v; want success, or a CommitError once the path leads outside", err)
		}
	}
	if b, err := os.ReadFile(landed); err != nil || string(b) != "pinned" {
		t.Errorf("%s holds %q, %v; want the write in its pinned directory", landed, b, err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("the write left the root: the folder outside holds %v", entries)
	}
}

// A rename into a junction that leads outside the root is refused, and nothing leaves the root.
func TestARenameThroughAJunctionOutsideTheRootIsRefused(t *testing.T) {
	bothRoots(t, renameThroughAJunctionIsRefused)
}

func renameThroughAJunctionIsRefused(t *testing.T, f *FS, dir string) {
	outside := t.TempDir()
	mustWrite(t, f, "a.md", "stays")
	junction(t, filepath.Join(dir, "j"), outside)
	for _, noReplace := range []bool{false, true} {
		var err error
		if noReplace {
			err = f.RenameNoReplace(bg, "a.md", "j/a.md")
		} else {
			err = f.Rename(bg, "a.md", "j/a.md")
		}
		if err == nil {
			t.Errorf("rename (no-replace %v) into a junction outside the root succeeded", noReplace)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("a rename left the root: the folder outside holds %v", entries)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "a.md")); err != nil || string(b) != "stays" {
		t.Errorf("a.md is %q, %v after the refused renames", b, err)
	}
}

// A no-replace rename moves a file or a directory whole, and refuses an existing name with a
// conflict, leaving both untouched.
func TestANoReplaceRenameMovesWholeAndRefusesAnExistingName(t *testing.T) {
	f, dir := newLocal(t)
	mustWrite(t, f, "a.md", "aaa")
	mustWrite(t, f, "b.md", "bbb")
	err := f.RenameNoReplace(bg, "a.md", "b.md")
	var conflict *vfs.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("a no-replace rename onto b.md gave %v, want a conflict", err)
	}
	for name, want := range map[string]string{"a.md": "aaa", "b.md": "bbb"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s is %q, %v after the refused rename; want %q", name, b, err, want)
		}
	}
	if err := f.RenameNoReplace(bg, "a.md", "c.md"); err != nil {
		t.Fatalf("a no-replace rename to a fresh name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); !os.IsNotExist(err) {
		t.Errorf("a.md still there after it moved: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "c.md")); string(b) != "aaa" {
		t.Errorf("c.md is %q, want aaa", b)
	}
	// directories: moved whole, and refused onto an existing one
	for _, d := range []string{"d1", "d2"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, f, "d1/x.md", "x")
	if err := f.RenameNoReplace(bg, "d1", "d2"); !errors.As(err, &conflict) {
		t.Errorf("a no-replace rename of d1 onto d2 gave %v, want a conflict", err)
	}
	if err := f.RenameNoReplace(bg, "d1", "d3"); err != nil {
		t.Fatalf("a no-replace rename of a directory: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "d3", "x.md")); string(b) != "x" {
		t.Errorf("d3/x.md is %q, want the moved directory's file", b)
	}
}

// bothRoots runs cell on an FS holding its root from New, then on one opening it per operation
// (RootPerOperation): the jail confines either the same.
func bothRoots(t *testing.T, cell func(t *testing.T, f *FS, dir string)) {
	t.Run("held root", func(t *testing.T) {
		f, dir := newLocal(t)
		cell(t, f, dir)
	})
	t.Run("root per operation", func(t *testing.T) {
		dir := t.TempDir()
		cell(t, newPerOp(t, dir), dir)
	})
}

// holdAtCommit opens name in dir as a reader does (os.Open, no delete-sharing) at the write's
// commit point, and answers the open file; the cell closes it.
func holdAtCommit(t *testing.T, dir, name string) <-chan *os.File {
	t.Helper()
	held := make(chan *os.File, 1)
	t.Cleanup(func() { testHook = nil })
	testHook = func(p string) error {
		if p != "precommit" {
			return nil
		}
		testHook = nil
		fh, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("holding %s: %v", name, err)
			return nil
		}
		held <- fh
		return nil
	}
	return held
}

// A write over a file a reader holds open (os.Open, which doesn't share delete) waits the hold
// out: the reader lets go within replaceBusyWait, and the write lands.
func TestAWriteWaitsOutAReaderHoldingItsFile(t *testing.T) {
	bothRoots(t, func(t *testing.T, f *FS, dir string) {
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		held := holdAtCommit(t, dir, "a.md")
		go func() {
			fh := <-held
			time.Sleep(200 * time.Millisecond) // a reader's moment, well inside replaceBusyWait
			fh.Close()
		}()
		if _, err := f.WriteFile(bg, "a.md", strings.NewReader("new")); err != nil {
			t.Fatalf("WriteFile over a file held for 200 ms: %v; want it written once let go", err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "new" {
			t.Fatalf("a.md holds %q, want the write's", b)
		}
	})
}

// A write over a file held past replaceBusyWait fails, with the hold's own refusal whole (a
// sharing violation), and leaves the file as it was.
func TestAWriteOverAFileHeldPastTheWaitSaysItIsInUse(t *testing.T) {
	bothRoots(t, func(t *testing.T, f *FS, dir string) {
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		held := holdAtCommit(t, dir, "a.md")
		start := time.Now()
		_, err := f.WriteFile(bg, "a.md", strings.NewReader("new"))
		took := time.Since(start)
		select {
		case fh := <-held:
			fh.Close()
		default:
			t.Fatal("the file was never held")
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Fatalf("WriteFile over a held file: %v; want the sharing violation", err)
		}
		if took < replaceBusyWait {
			t.Errorf("the write gave up after %s, before replaceBusyWait (%s)", took, replaceBusyWait)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "old" {
			t.Fatalf("a.md holds %q, want it as it was", b)
		}
	})
}
