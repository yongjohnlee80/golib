//go:build linux || darwin

package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/vfstest"
)

var bg = context.Background()

func newLocal(t *testing.T) (*FS, string) {
	t.Helper()
	dir := t.TempDir()
	f, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, dir
}

func TestConformance(t *testing.T) {
	vfstest.TestFS(t, func(t *testing.T) vfs.FS {
		f, err := New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return f
	})
}

func mustWrite(t *testing.T, f *FS, name, content string) vfs.FileInfo {
	t.Helper()
	fi, err := f.WriteFile(bg, name, strings.NewReader(content))
	if err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
	return fi
}

func readAll(t *testing.T, f *FS, name string) string {
	t.Helper()
	rc, err := f.Open(bg, name, 0)
	if err != nil {
		t.Fatalf("Open(%q): %v", name, err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b)
}

// temps lists the driver's temp files anywhere under dir, on disk.
func temps(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && vfs.IsTemp(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestAtomicReplaceReaders: readers racing a stream of replaces see one whole version, never a mix.
func TestAtomicReplaceReaders(t *testing.T) {
	f, _ := newLocal(t)
	a := strings.Repeat("A", 1<<20)
	b := strings.Repeat("B", 1<<20)
	mustWrite(t, f, "x", a)
	var stop atomic.Bool
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for !stop.Load() {
				rc, err := f.Open(bg, "x", 0)
				if err != nil {
					t.Errorf("Open: %v", err)
					return
				}
				got, _ := io.ReadAll(rc)
				rc.Close()
				if s := string(got); s != a && s != b {
					t.Errorf("a reader saw a mix: %d bytes, first %q last %q", len(s), s[:1], s[len(s)-1:])
					return
				}
			}
		})
	}
	for i := range 50 {
		mustWrite(t, f, "x", []string{a, b}[i%2])
	}
	stop.Store(true)
	wg.Wait()
}

// TestCreateExclusiveReaders: readers racing a CreateExclusive see not-exist or the whole file.
func TestCreateExclusiveReaders(t *testing.T) {
	f, _ := newLocal(t)
	body := strings.Repeat("C", 4<<20)
	for i := range 10 {
		name := "c" + string(rune('0'+i))
		var stop atomic.Bool
		var wg sync.WaitGroup
		wg.Go(func() {
			for !stop.Load() {
				rc, err := f.Open(bg, name, 0)
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					t.Errorf("Open: %v", err)
					return
				}
				got, _ := io.ReadAll(rc)
				rc.Close()
				if len(got) != len(body) {
					t.Errorf("a reader saw a partial create: %d of %d bytes", len(got), len(body))
					return
				}
			}
		})
		if _, err := f.CreateExclusive(bg, name, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
		stop.Store(true)
		wg.Wait()
	}
}

type failingReader struct{ n int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, errors.New("boom")
	}
	k := min(len(p), r.n)
	r.n -= k
	return k, nil
}

func TestFailedWriteKeepsOldAndNoTemp(t *testing.T) {
	f, dir := newLocal(t)
	mustWrite(t, f, "a.md", "old")
	if _, err := f.WriteFile(bg, "a.md", &failingReader{n: 100_000}); err == nil {
		t.Fatal("a write whose reader failed succeeded")
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := f.WriteFile(ctx, "a.md", strings.NewReader("new")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: err = %v", err)
	}
	if got := readAll(t, f, "a.md"); got != "old" {
		t.Fatalf("content = %q, want old", got)
	}
	if tt := temps(t, dir); len(tt) != 0 {
		t.Fatalf("temp files left behind: %v", tt)
	}
}

// TestCommitPoint: a failure before the rename leaves the old state; one after it is a *CommitError
// with the new content in place.
func TestCommitPoint(t *testing.T) {
	f, dir := newLocal(t)
	mustWrite(t, f, "a.md", "old")
	boom := errors.New("injected")
	t.Cleanup(func() { testHook = nil })

	testHook = func(p string) error {
		if p == "precommit" {
			return boom
		}
		return nil
	}
	_, err := f.WriteFile(bg, "a.md", strings.NewReader("new"))
	if !errors.Is(err, boom) || errors.Is(err, vfs.ErrCommitted) {
		t.Fatalf("precommit failure: err = %v, want the injected error without ErrCommitted", err)
	}
	if got := readAll(t, f, "a.md"); got != "old" {
		t.Fatalf("after a precommit failure content = %q", got)
	}
	if tt := temps(t, dir); len(tt) != 0 {
		t.Fatalf("temp files left behind: %v", tt)
	}

	testHook = func(p string) error {
		if p == "postcommit" {
			return boom
		}
		return nil
	}
	for name, run := range map[string]func() error{
		"WriteFile":       func() error { _, e := f.WriteFile(bg, "a.md", strings.NewReader("new")); return e },
		"CreateExclusive": func() error { _, e := f.CreateExclusive(bg, "b.md", strings.NewReader("new")); return e },
		"Rename":          func() error { return f.Rename(bg, "b.md", "c.md") },
	} {
		err := run()
		var ce *vfs.CommitError
		if !errors.As(err, &ce) || !errors.Is(err, vfs.ErrCommitted) || !errors.Is(err, boom) {
			t.Fatalf("%s postcommit failure: err = %v, want *CommitError wrapping ErrCommitted and the cause", name, err)
		}
	}
	testHook = nil
	if readAll(t, f, "a.md") != "new" || readAll(t, f, "c.md") != "new" {
		t.Fatalf("committed changes are not in place")
	}
}

// TestSymlinkEscape asserts the EFFECT: nothing outside the root is read, written or removed through
// links planted inside it, whatever error identity os.Root reports.
func TestSymlinkEscape(t *testing.T) {
	f, dir := newLocal(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1_000_000_000, 0)
	_ = os.Chtimes(secret, old, old)
	_ = os.Symlink(outside, filepath.Join(dir, "out"))
	_ = os.Symlink(secret, filepath.Join(dir, "outf"))
	mustWrite(t, f, "a.md", "a")

	if _, err := f.Open(bg, "outf", 0); err == nil {
		t.Error("Open through a link to outside succeeded")
	}
	if _, err := f.ReadDir(bg, "out"); err == nil {
		t.Error("ReadDir through a link to outside succeeded")
	}
	if _, err := f.Open(bg, "out/secret", 0); err == nil {
		t.Error("Open beneath a link to outside succeeded")
	}
	_, _ = f.WriteFile(bg, "out/secret", strings.NewReader("pwned"))
	_, _ = f.WriteFile(bg, "out/new", strings.NewReader("pwned"))
	_, _ = f.WriteFile(bg, "outf", strings.NewReader("pwned"))
	_, _ = f.CreateExclusive(bg, "out/new2", strings.NewReader("pwned"))
	_ = f.MkdirAll(bg, "out/newdir")
	_ = f.Remove(bg, "out/secret")
	_ = f.RemoveAll(bg, "out/secret")
	_ = f.Rename(bg, "a.md", "out/a.md")
	_ = f.Rename(bg, "out/secret", "stolen")

	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 || entries[0].Name() != "secret" {
		t.Fatalf("outside dir changed: %v", entries)
	}
	b, _ := os.ReadFile(secret)
	st, _ := os.Stat(secret)
	if string(b) != "secret" || !st.ModTime().Equal(old) {
		t.Fatalf("outside file changed: %q mtime %v", b, st.ModTime())
	}
	if _, err := os.Lstat(filepath.Join(dir, "stolen")); err == nil {
		t.Fatal("a file was moved in from outside")
	}
}

// TestSymlinkPolicy: entries are reported themselves, never followed (ADR §4.1).
func TestSymlinkPolicy(t *testing.T) {
	f, dir := newLocal(t)
	mustWrite(t, f, "f.md", "target")
	if err := f.MkdirAll(bg, "d"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, "d/x.md", "x")
	_ = os.Symlink("f.md", filepath.Join(dir, "lf"))
	_ = os.Symlink("d", filepath.Join(dir, "ld"))
	_ = os.Symlink(t.TempDir(), filepath.Join(dir, "lout"))

	for _, name := range []string{"lf", "ld", "lout"} {
		st, err := f.Stat(bg, name)
		if err != nil || st.Mode&fs.ModeSymlink == 0 || st.IsDir() || st.IsRegular() || st.Size != 0 {
			t.Fatalf("Stat(%s) = %+v, %v; want a symlink entry", name, st, err)
		}
	}
	entries, _ := f.ReadDir(bg, ".")
	byName := map[string]vfs.FileInfo{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	st, _ := f.Stat(bg, "lf")
	if byName["lf"].Version != st.Version || byName["lf"].Mode&fs.ModeSymlink == 0 {
		t.Fatalf("ReadDir and Stat disagree on lf: %+v vs %+v", byName["lf"], st)
	}

	var walked []string
	for fi, err := range vfs.Walk(bg, f, ".") {
		if err != nil {
			t.Fatal(err)
		}
		walked = append(walked, fi.Path)
	}
	for _, p := range walked {
		if strings.HasPrefix(p, "ld/") || strings.HasPrefix(p, "lout/") {
			t.Fatalf("Walk entered a symlink: %v", walked)
		}
	}
	for _, err := range vfs.Walk(bg, f, "ld") {
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("Walk starting at a directory link: err = %v, want ErrInvalidArgument", err)
		}
	}

	// reading through an in-root link works
	if got := readAll(t, f, "lf"); got != "target" {
		t.Fatalf("Open through lf = %q", got)
	}
	if list, err := f.ReadDir(bg, "ld"); err != nil || len(list) != 1 {
		t.Fatalf("ReadDir through ld = %v, %v", list, err)
	}
	// writes onto a link are refused; the target is unchanged
	if _, err := f.WriteFile(bg, "lf", strings.NewReader("over")); err == nil {
		t.Fatal("WriteFile onto a symlink succeeded")
	}
	if _, err := f.WriteFileIf(bg, "lf", strings.NewReader("over"), st.Version); err == nil {
		t.Fatal("WriteFileIf onto a symlink succeeded")
	}
	if got := readAll(t, f, "f.md"); got != "target" {
		t.Fatalf("the link target changed: %q", got)
	}
	// RemoveIf with the link's own version removes the link, not the target
	if err := f.RemoveIf(bg, "lf", st.Version); err != nil {
		t.Fatalf("RemoveIf(lf, its version): %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "lf")); !os.IsNotExist(err) {
		t.Fatalf("lf still exists: %v", err)
	}
	if got := readAll(t, f, "f.md"); got != "target" {
		t.Fatalf("RemoveIf of the link touched the target: %q", got)
	}
}

// TestVersion: changes on every content change — including a same-size in-place rewrite with the mtime
// set back — and not on a read.
func TestVersion(t *testing.T) {
	f, dir := newLocal(t)
	v1 := mustWrite(t, f, "a.md", "aaaa").Version
	_ = readAll(t, f, "a.md")
	if st, _ := f.Stat(bg, "a.md"); st.Version != v1 {
		t.Fatalf("a read changed the version: %s → %s", v1, st.Version)
	}
	p := filepath.Join(dir, "a.md")
	st0, _ := os.Stat(p)
	time.Sleep(50 * time.Millisecond) // past the filesystem's coarse timestamp tick
	fh, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteAt([]byte("bbbb"), 0)
	fh.Close()
	_ = os.Chtimes(p, st0.ModTime(), st0.ModTime())
	st1, _ := os.Stat(p)
	if st1.Size() != st0.Size() || !st1.ModTime().Equal(st0.ModTime()) {
		t.Fatalf("setup: size/mtime differ (%d %v vs %d %v)", st1.Size(), st1.ModTime(), st0.Size(), st0.ModTime())
	}
	st, _ := f.Stat(bg, "a.md")
	if st.Version == v1 {
		t.Fatalf("same-size same-mtime in-place rewrite kept version %s (ctime must move it)", v1)
	}
	if v2 := mustWrite(t, f, "a.md", "bbbb").Version; v2 == st.Version {
		t.Fatalf("an atomic replace kept the version")
	}
}

func TestPerm(t *testing.T) {
	f, dir := newLocal(t)
	if _, err := f.WriteFile(bg, "p.sh", strings.NewReader("x"), vfs.WithPerm(0o750)); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, "p.sh"))
	if st.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %v, want 0750", st.Mode().Perm())
	}
	mustWrite(t, f, "p.sh", "y") // an existing file keeps its mode
	st, _ = os.Stat(filepath.Join(dir, "p.sh"))
	if st.Mode().Perm() != 0o750 {
		t.Fatalf("after replace mode = %v, want 0750", st.Mode().Perm())
	}
}

func TestRenameIntoItself(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "d/e")
	for _, run := range []func() error{
		func() error { return f.Rename(bg, "d", "d/e/d") },
		func() error { return f.RenameNoReplace(bg, "d", "d/e/d") },
	} {
		if err := run(); !errors.Is(err, errs.ErrInvalidArgument) || errors.Is(err, errs.ErrUnsupported) {
			t.Fatalf("move into itself: err = %v, want ErrInvalidArgument (not ErrUnsupported)", err)
		}
	}
}

func TestReadDirHidesTemps(t *testing.T) {
	f, dir := newLocal(t)
	mustWrite(t, f, "a.md", "a")
	_ = os.WriteFile(filepath.Join(dir, vfs.TempPrefix+"a.md-deadbeef"), []byte("x"), 0o644)
	entries, _ := f.ReadDir(bg, ".")
	if len(entries) != 1 || entries[0].Name != "a.md" {
		t.Fatalf("ReadDir = %+v, want only a.md", entries)
	}
	var buf bytes.Buffer
	for fi := range vfs.Walk(bg, f, ".") {
		buf.WriteString(fi.Path + " ")
	}
	if strings.Contains(buf.String(), vfs.TempPrefix) {
		t.Fatalf("Walk yielded a temp file: %s", buf.String())
	}
}
