// Package vfstest is the conformance suite for [github.com/yongjohnlee80/golib/vfs.FS] drivers. A driver
// passes it by running [TestFS] from its own tests with a constructor for a fresh, empty filesystem.
//
// The core cells run for every driver. Each capability — ConditionalWriter, ExclusiveCreator,
// NoReplaceRenamer, Watcher, Copier — has its own cell group, run when the driver implements the
// interface and skipped, with the capability's name, when it does not. A capability that the platform
// refuses at runtime (errs.ErrUnsupported) skips its cell with that error rather than passing silently.
package vfstest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/vfs"
)

// TestFS runs the conformance suite. newFS must return a fresh, empty filesystem for each call.
func TestFS(t *testing.T, newFS func(t *testing.T) vfs.FS) {
	t.Helper()
	cells := []struct {
		name string
		fn   func(t *testing.T, fsys vfs.FS)
	}{
		{"WriteReadStat", testWriteReadStat},
		{"ReplaceChangesVersion", testReplaceChangesVersion},
		{"OpenOffset", testOpenOffset},
		{"InvalidNames", testInvalidNames},
		{"NotExist", testNotExist},
		{"MkdirAll", testMkdirAll},
		{"ReadDirSorted", testReadDirSorted},
		{"Remove", testRemove},
		{"RemoveAll", testRemoveAll},
		{"Rename", testRename},
		{"Walk", testWalk},
		{"WalkSkipDirs", testWalkSkipDirs},
		{"PollSkipDirs", testPollSkipDirs},
		{"Close", testClose},
		{"ConditionalWriter/WriteFileIf", as(testWriteFileIf)},
		{"ConditionalWriter/RemoveIf", as(testRemoveIf)},
		{"ConditionalWriter/Race", as(testConditionRace)},
		{"ConditionalWriter/InvalidNames", as(testConditionalInvalidNames)},
		{"ExclusiveCreator", as(testCreateExclusive)},
		{"NoReplaceRenamer", as(testRenameNoReplace)},
		{"Watcher", as(testWatch)},
		{"Watcher/SkipDirs", as(testWatchSkipDirs)},
		{"Copier", as(testCopy)},
	}
	for _, c := range cells {
		t.Run(c.name, func(t *testing.T) {
			fsys := newFS(t)
			t.Cleanup(func() { _ = fsys.Close() })
			c.fn(t, fsys)
		})
	}
}

// as adapts a capability cell: it runs fn with fsys asserted to C, or skips naming C.
func as[C any](fn func(t *testing.T, fsys vfs.FS, c C)) func(t *testing.T, fsys vfs.FS) {
	return func(t *testing.T, fsys vfs.FS) {
		c, ok := fsys.(C)
		if !ok {
			t.Skipf("driver does not implement %T", (*C)(nil))
		}
		fn(t, fsys, c)
	}
}

// skipUnsupported skips the cell when the platform refused the capability at runtime.
func skipUnsupported(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, errs.ErrUnsupported) {
		t.Skipf("capability refused by the platform: %v", err)
	}
}

var bg = context.Background()

func write(t *testing.T, fsys vfs.FS, name, content string, opts ...vfs.WriteOption) vfs.FileInfo {
	t.Helper()
	fi, err := fsys.WriteFile(bg, name, strings.NewReader(content), opts...)
	if err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
	return fi
}

func read(t *testing.T, fsys vfs.FS, name string) string {
	t.Helper()
	rc, err := fsys.Open(bg, name, 0)
	if err != nil {
		t.Fatalf("Open(%q): %v", name, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %q: %v", name, err)
	}
	return string(b)
}

func mustMkdir(t *testing.T, fsys vfs.FS, name string) {
	t.Helper()
	if err := fsys.MkdirAll(bg, name); err != nil {
		t.Fatalf("MkdirAll(%q): %v", name, err)
	}
}

func testWriteReadStat(t *testing.T, fsys vfs.FS) {
	mustMkdir(t, fsys, "docs")
	fi := write(t, fsys, "docs/a.md", "hello")
	if fi.Path != "docs/a.md" || fi.Name != "a.md" || fi.Size != 5 || !fi.IsRegular() || fi.Version == "" {
		t.Fatalf("WriteFile FileInfo = %+v", fi)
	}
	if got := read(t, fsys, "docs/a.md"); got != "hello" {
		t.Fatalf("content = %q, want hello", got)
	}
	st, err := fsys.Stat(bg, "docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != fi.Version || st.Size != 5 {
		t.Fatalf("Stat = %+v, WriteFile returned %+v", st, fi)
	}
	again, _ := fsys.Stat(bg, "docs/a.md")
	if again.Version != st.Version {
		t.Fatalf("Version changed without a write: %s → %s", st.Version, again.Version)
	}
	root, err := fsys.Stat(bg, ".")
	if err != nil || !root.IsDir() {
		t.Fatalf("Stat(.) = %+v, %v; want a directory", root, err)
	}
}

func testReplaceChangesVersion(t *testing.T, fsys vfs.FS) {
	v1 := write(t, fsys, "a.md", "one").Version
	v2 := write(t, fsys, "a.md", "two").Version
	v3 := write(t, fsys, "a.md", "two").Version // same content, same size
	if v1 == v2 || v2 == v3 || v1 == v3 {
		t.Fatalf("versions must differ per write: %s %s %s", v1, v2, v3)
	}
	if got := read(t, fsys, "a.md"); got != "two" {
		t.Fatalf("content = %q", got)
	}
}

func testOpenOffset(t *testing.T, fsys vfs.FS) {
	write(t, fsys, "a.txt", "0123456789")
	for _, c := range []struct {
		off  int64
		want string
	}{{0, "0123456789"}, {4, "456789"}, {10, ""}, {99, ""}} {
		rc, err := fsys.Open(bg, "a.txt", c.off)
		if err != nil {
			t.Fatalf("Open(off=%d): %v", c.off, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		if string(b) != c.want {
			t.Fatalf("Open(off=%d) = %q, want %q", c.off, b, c.want)
		}
	}
}

// badNames are invalid for every verb: lexical escapes, non-canonical forms and the driver's temp prefix.
var badNames = []string{"", "/etc/passwd", "../x", "a/../../x", "a/./b", "a//b", "a/", vfs.TempPrefix + "x"}

func testInvalidNames(t *testing.T, fsys vfs.FS) {
	for _, name := range badNames {
		checks := map[string]error{}
		_, checks["Stat"] = fsys.Stat(bg, name)
		_, checks["ReadDir"] = fsys.ReadDir(bg, name)
		_, checks["Open"] = fsys.Open(bg, name, 0)
		_, checks["WriteFile"] = fsys.WriteFile(bg, name, strings.NewReader("x"))
		checks["MkdirAll"] = fsys.MkdirAll(bg, name)
		checks["Remove"] = fsys.Remove(bg, name)
		checks["RemoveAll"] = fsys.RemoveAll(bg, name)
		checks["RenameFrom"] = fsys.Rename(bg, name, "ok.md")
		checks["RenameTo"] = fsys.Rename(bg, "ok.md", name)
		for verb, err := range checks {
			var pe *fs.PathError
			if !errors.Is(err, vfs.ErrInvalidName) || !errors.As(err, &pe) {
				t.Errorf("%s(%q): err = %v, want *fs.PathError wrapping ErrInvalidName", verb, name, err)
			}
		}
	}
	// the root itself cannot be written, removed or renamed
	for verb, err := range map[string]error{
		"WriteFile": func() error { _, e := fsys.WriteFile(bg, ".", strings.NewReader("x")); return e }(),
		"Remove":    fsys.Remove(bg, "."),
		"RemoveAll": fsys.RemoveAll(bg, "."),
		"Rename":    fsys.Rename(bg, ".", "x"),
	} {
		if !errors.Is(err, vfs.ErrInvalidName) {
			t.Errorf("%s(.): err = %v, want ErrInvalidName", verb, err)
		}
	}
}

func testNotExist(t *testing.T, fsys vfs.FS) {
	for verb, err := range map[string]error{
		"Stat":      func() error { _, e := fsys.Stat(bg, "nope"); return e }(),
		"ReadDir":   func() error { _, e := fsys.ReadDir(bg, "nope"); return e }(),
		"Open":      func() error { _, e := fsys.Open(bg, "nope", 0); return e }(),
		"Remove":    fsys.Remove(bg, "nope"),
		"Rename":    fsys.Rename(bg, "nope", "x"),
		"WriteFile": func() error { _, e := fsys.WriteFile(bg, "nodir/a.md", strings.NewReader("x")); return e }(),
	} {
		var pe *fs.PathError
		if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pe) {
			t.Errorf("%s on a missing name: err = %v, want *fs.PathError wrapping fs.ErrNotExist", verb, err)
		}
	}
	if err := fsys.RemoveAll(bg, "nope"); err != nil {
		t.Errorf("RemoveAll on a missing name: %v, want nil", err)
	}
}

func testMkdirAll(t *testing.T, fsys vfs.FS) {
	mustMkdir(t, fsys, "a/b/c")
	mustMkdir(t, fsys, "a/b/c") // idempotent
	mustMkdir(t, fsys, ".")
	for _, d := range []string{"a", "a/b", "a/b/c"} {
		fi, err := fsys.Stat(bg, d)
		if err != nil || !fi.IsDir() {
			t.Fatalf("Stat(%q) = %+v, %v; want a directory", d, fi, err)
		}
	}
	write(t, fsys, "f", "x")
	if err := fsys.MkdirAll(bg, "f/sub"); err == nil {
		t.Fatalf("MkdirAll through a file succeeded")
	}
}

func testReadDirSorted(t *testing.T, fsys vfs.FS) {
	for _, n := range []string{"c.md", "a.md", "b.md"} {
		write(t, fsys, n, n)
	}
	mustMkdir(t, fsys, "dir")
	entries, err := fsys.ReadDir(bg, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Path != e.Name {
			t.Errorf("root entry Path = %q, want %q", e.Path, e.Name)
		}
	}
	if want := []string{"a.md", "b.md", "c.md", "dir"}; !slices.Equal(names, want) {
		t.Fatalf("ReadDir = %v, want %v", names, want)
	}
	if _, err := fsys.ReadDir(bg, "a.md"); err == nil {
		t.Fatalf("ReadDir on a file succeeded")
	}
}

func testRemove(t *testing.T, fsys vfs.FS) {
	mustMkdir(t, fsys, "d")
	write(t, fsys, "d/a.md", "x")
	if err := fsys.Remove(bg, "d"); err == nil {
		t.Fatalf("Remove of a non-empty directory succeeded")
	}
	if err := fsys.Remove(bg, "d/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Remove(bg, "d"); err != nil {
		t.Fatalf("Remove of an empty directory: %v", err)
	}
	if _, err := fsys.Stat(bg, "d"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("after Remove, Stat err = %v", err)
	}
}

func testRemoveAll(t *testing.T, fsys vfs.FS) {
	mustMkdir(t, fsys, "d/e")
	write(t, fsys, "d/e/a.md", "x")
	write(t, fsys, "keep.md", "k")
	if err := fsys.RemoveAll(bg, "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.Stat(bg, "d"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("after RemoveAll, Stat(d) err = %v", err)
	}
	if got := read(t, fsys, "keep.md"); got != "k" {
		t.Fatalf("RemoveAll touched a sibling: %q", got)
	}
}

func testRename(t *testing.T, fsys vfs.FS) {
	write(t, fsys, "a.md", "A")
	mustMkdir(t, fsys, "dir")
	if err := fsys.Rename(bg, "a.md", "dir/b.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.Stat(bg, "a.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("source still exists after Rename: %v", err)
	}
	if got := read(t, fsys, "dir/b.md"); got != "A" {
		t.Fatalf("renamed content = %q", got)
	}
	// replace an existing file
	write(t, fsys, "c.md", "C")
	if err := fsys.Rename(bg, "c.md", "dir/b.md"); err != nil {
		t.Fatalf("Rename over an existing file: %v", err)
	}
	if got := read(t, fsys, "dir/b.md"); got != "C" {
		t.Fatalf("after replacing rename, content = %q", got)
	}
	// a directory with contents
	mustMkdir(t, fsys, "tree/sub")
	write(t, fsys, "tree/sub/x.md", "X")
	if err := fsys.Rename(bg, "tree", "moved"); err != nil {
		t.Fatalf("Rename of a directory: %v", err)
	}
	if got := read(t, fsys, "moved/sub/x.md"); got != "X" {
		t.Fatalf("moved tree content = %q", got)
	}
}

func testWalk(t *testing.T, fsys vfs.FS) {
	mustMkdir(t, fsys, "a/b")
	write(t, fsys, "a/b/x.md", "x")
	write(t, fsys, "a/y.md", "y")
	write(t, fsys, "z.md", "z")
	var got []string
	for fi, err := range vfs.Walk(bg, fsys, ".") {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, fi.Path)
	}
	want := []string{"a", "a/b", "a/b/x.md", "a/y.md", "z.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("Walk = %v, want %v", got, want)
	}
	for fi, err := range vfs.Walk(bg, fsys, "z.md") {
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("Walk from a file yielded %+v, %v; want one ErrInvalidArgument", fi, err)
		}
	}
}

// skipNodeModules is the SkipDirs predicate the skip cells use.
func skipNodeModules(dir string) bool { return path.Base(dir) == "node_modules" }

// testWalkSkipDirs: Walk yields a skipped directory but none of its contents.
func testWalkSkipDirs(t *testing.T, fsys vfs.FS) {
	mustMkdir(t, fsys, "a/node_modules/pkg")
	write(t, fsys, "a/node_modules/pkg/README.md", "r")
	write(t, fsys, "a/x.md", "x")
	var got []string
	for fi, err := range vfs.Walk(bg, fsys, ".", vfs.WalkSkipDirs(skipNodeModules)) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, fi.Path)
	}
	if want := []string{"a", "a/node_modules", "a/x.md"}; !slices.Equal(got, want) {
		t.Fatalf("Walk with WalkSkipDirs = %v, want %v", got, want)
	}
}

// testPollSkipDirs: Poll reports nothing inside a skipped directory, but does report the directory
// itself appearing — and never an OpOverflow for it.
func testPollSkipDirs(t *testing.T, fsys vfs.FS) {
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	mustMkdir(t, fsys, "w")
	events, err := vfs.Poll(ctx, fsys, "w", 20*time.Millisecond, vfs.Recursive(), vfs.SkipDirs(skipNodeModules))
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, fsys, "w/node_modules/pkg")
	write(t, fsys, "w/node_modules/pkg/README.md", "r")
	write(t, fsys, "w/after.md", "a")
	assertSkipped(t, events, "w/node_modules", "w/after.md")
}

// testWatchSkipDirs: a Watcher with SkipDirs reports a skipped directory appearing as OpCreate and
// nothing else for it — no OpOverflow, nothing inside it — while the rest of the tree is reported.
func testWatchSkipDirs(t *testing.T, fsys vfs.FS, w vfs.Watcher) {
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	mustMkdir(t, fsys, "w")
	events, err := w.Watch(ctx, "w", vfs.Recursive(), vfs.SkipDirs(skipNodeModules))
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, fsys, "w/node_modules")
	write(t, fsys, "w/node_modules/inside.md", "i")
	mustMkdir(t, fsys, "w/node_modules/pkg")
	write(t, fsys, "w/node_modules/pkg/README.md", "r")
	write(t, fsys, "w/after.md", "a")
	assertSkipped(t, events, "w/node_modules", "w/after.md")
}

// assertSkipped reads events until one for sentinel arrives, then fails if any of them was inside
// skipped, an OpOverflow for it (or for everything), or if skipped's own OpCreate never came.
func assertSkipped(t *testing.T, events <-chan vfs.Event, skipped, sentinel string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var seen []vfs.Event
	created := false
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("event channel closed; saw %v", seen)
			}
			seen = append(seen, ev)
			switch {
			case strings.HasPrefix(ev.Path, skipped+"/"):
				t.Fatalf("an event inside the skipped %s: %v (all: %v)", skipped, ev, seen)
			case ev.Op == vfs.OpOverflow && (ev.Path == skipped || ev.Path == ""):
				t.Fatalf("an OpOverflow for the skipped %s: %v (all: %v)", skipped, ev, seen)
			case ev.Path == skipped && ev.Op == vfs.OpCreate:
				created = true
			}
			if ev.Path == sentinel {
				// drain briefly: a late event inside the skipped dir must not follow either
				settle := time.After(300 * time.Millisecond)
				for {
					select {
					case ev, ok := <-events:
						if !ok {
							goto done
						}
						seen = append(seen, ev)
						if strings.HasPrefix(ev.Path, skipped+"/") || (ev.Op == vfs.OpOverflow && (ev.Path == skipped || ev.Path == "")) {
							t.Fatalf("a late event for the skipped %s: %v (all: %v)", skipped, ev, seen)
						}
						if ev.Path == skipped && ev.Op == vfs.OpCreate {
							created = true
						}
					case <-settle:
						goto done
					}
				}
			done:
				if !created {
					t.Fatalf("no OpCreate for the skipped directory %s itself; saw %v", skipped, seen)
				}
				return
			}
		case <-deadline:
			t.Fatalf("no event for %s within 5s; saw %v", sentinel, seen)
		}
	}
}

// testWatch: a Watcher reports create, write and remove under a recursive watch.
func testWatch(t *testing.T, fsys vfs.FS, w vfs.Watcher) {
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	mustMkdir(t, fsys, "w")
	events, err := w.Watch(ctx, "w", vfs.Recursive())
	if err != nil {
		t.Fatal(err)
	}
	write(t, fsys, "w/a.md", "1")
	waitFor(t, events, "w/a.md", vfs.OpCreate, vfs.OpWrite)
	write(t, fsys, "w/a.md", "2")
	waitFor(t, events, "w/a.md", vfs.OpCreate, vfs.OpWrite)
	mustMkdir(t, fsys, "w/sub")
	waitFor(t, events, "w/sub", vfs.OpCreate, vfs.OpOverflow)
	write(t, fsys, "w/sub/b.md", "b")
	waitFor(t, events, "w/sub/b.md", vfs.OpCreate, vfs.OpWrite)
	if err := fsys.Remove(bg, "w/a.md"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, events, "w/a.md", vfs.OpRemove)
	cancel()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
		case <-deadline:
			t.Fatalf("the event channel did not close after ctx was cancelled")
		}
	}
}

// waitFor reads events until one for p with one of ops arrives. An OpOverflow covering p also counts.
func waitFor(t *testing.T, events <-chan vfs.Event, p string, ops ...vfs.Op) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var seen bytes.Buffer
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("event channel closed while waiting for %s %v", p, ops)
			}
			fmt.Fprintf(&seen, " %s:%s", ev.Op, ev.Path)
			if ev.Path == p && slices.Contains(ops, ev.Op) {
				return
			}
			if ev.Op == vfs.OpOverflow && (ev.Path == "" || ev.Path == p || strings.HasPrefix(p, ev.Path+"/")) {
				return
			}
		case <-deadline:
			t.Fatalf("no %v event for %s within 5s; saw:%s", ops, p, seen.String())
		}
	}
}

func testClose(t *testing.T, fsys vfs.FS) {
	write(t, fsys, "a.md", "x")
	if err := fsys.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.Stat(bg, "a.md"); !errors.Is(err, errs.ErrClosed) {
		t.Fatalf("Stat after Close: err = %v, want errs.ErrClosed", err)
	}
	if _, err := fsys.WriteFile(bg, "b.md", strings.NewReader("x")); !errors.Is(err, errs.ErrClosed) {
		t.Fatalf("WriteFile after Close: err = %v, want errs.ErrClosed", err)
	}
}

func testWriteFileIf(t *testing.T, fsys vfs.FS, c vfs.ConditionalWriter) {
	v1 := write(t, fsys, "a.md", "one").Version
	fi, err := c.WriteFileIf(bg, "a.md", strings.NewReader("two"), v1)
	if err != nil {
		t.Fatalf("WriteFileIf with the current version: %v", err)
	}
	v2 := fi.Version
	if v2 == v1 {
		t.Fatalf("WriteFileIf did not change the version")
	}
	_, err = c.WriteFileIf(bg, "a.md", strings.NewReader("stale"), v1)
	var ce *vfs.ConflictError
	if !errors.As(err, &ce) || !errors.Is(err, vfs.ErrConflict) || !errors.Is(err, errs.ErrPrecondition) {
		t.Fatalf("stale WriteFileIf: err = %v, want *ConflictError wrapping ErrConflict", err)
	}
	if ce.Current.Version != v2 || ce.Want != v1 {
		t.Fatalf("ConflictError = %+v, want Current.Version %s, Want %s", ce, v2, v1)
	}
	if got := read(t, fsys, "a.md"); got != "two" {
		t.Fatalf("a failed condition changed the file: %q", got)
	}
	_, err = c.WriteFileIf(bg, "missing.md", strings.NewReader("x"), v1)
	if !errors.As(err, &ce) || ce.Current.Path != "" {
		t.Fatalf("WriteFileIf on a missing file: err = %v, want *ConflictError with no Current", err)
	}
	if _, serr := fsys.Stat(bg, "missing.md"); !errors.Is(serr, fs.ErrNotExist) {
		t.Fatalf("a failed WriteFileIf created the file: %v", serr)
	}
	// the version a Stat or ReadDir reports is the one the condition compares
	st, _ := fsys.Stat(bg, "a.md")
	if _, err := c.WriteFileIf(bg, "a.md", strings.NewReader("three"), st.Version); err != nil {
		t.Fatalf("WriteFileIf with Stat's version: %v", err)
	}
	entries, _ := fsys.ReadDir(bg, ".")
	if _, err := c.WriteFileIf(bg, "a.md", strings.NewReader("four"), entries[0].Version); err != nil {
		t.Fatalf("WriteFileIf with ReadDir's version: %v", err)
	}
}

func testRemoveIf(t *testing.T, fsys vfs.FS, c vfs.ConditionalWriter) {
	v1 := write(t, fsys, "a.md", "one").Version
	v2 := write(t, fsys, "a.md", "two").Version
	if err := c.RemoveIf(bg, "a.md", v1); !errors.Is(err, vfs.ErrConflict) {
		t.Fatalf("RemoveIf with a stale version: err = %v, want ErrConflict", err)
	}
	if got := read(t, fsys, "a.md"); got != "two" {
		t.Fatalf("a failed RemoveIf changed the file: %q", got)
	}
	if err := c.RemoveIf(bg, "a.md", v2); err != nil {
		t.Fatalf("RemoveIf with the current version: %v", err)
	}
	if err := c.RemoveIf(bg, "a.md", v2); !errors.Is(err, vfs.ErrConflict) {
		t.Fatalf("RemoveIf on a removed file: err = %v, want ErrConflict", err)
	}
}

// testConditionRace: many writers racing WriteFileIf(v) through one instance — exactly one wins.
func testConditionRace(t *testing.T, fsys vfs.FS, c vfs.ConditionalWriter) {
	v := write(t, fsys, "a.md", "base").Version
	const n = 16
	var wins, conflicts int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, err := c.WriteFileIf(bg, "a.md", strings.NewReader(fmt.Sprint("w", i)), v)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, vfs.ErrConflict):
				conflicts++
			default:
				t.Errorf("racing writer: %v", err)
			}
		})
	}
	wg.Wait()
	if wins != 1 || conflicts != n-1 {
		t.Fatalf("WriteFileIf race: %d wins, %d conflicts; want exactly 1 win", wins, conflicts)
	}
}

func testConditionalInvalidNames(t *testing.T, fsys vfs.FS, c vfs.ConditionalWriter) {
	for _, name := range badNames {
		_, werr := c.WriteFileIf(bg, name, strings.NewReader("x"), "v")
		rerr := c.RemoveIf(bg, name, "v")
		for verb, err := range map[string]error{"WriteFileIf": werr, "RemoveIf": rerr} {
			if !errors.Is(err, vfs.ErrInvalidName) {
				t.Errorf("%s(%q): err = %v, want ErrInvalidName", verb, name, err)
			}
		}
	}
}

func testCreateExclusive(t *testing.T, fsys vfs.FS, c vfs.ExclusiveCreator) {
	fi, err := c.CreateExclusive(bg, "a.md", strings.NewReader("first"))
	skipUnsupported(t, err)
	if err != nil {
		t.Fatalf("CreateExclusive on a free name: %v", err)
	}
	if st, _ := fsys.Stat(bg, "a.md"); st.Version != fi.Version {
		t.Fatalf("CreateExclusive returned version %s, Stat has %s", fi.Version, st.Version)
	}
	_, err = c.CreateExclusive(bg, "a.md", strings.NewReader("second"))
	var ce *vfs.ConflictError
	if !errors.As(err, &ce) || ce.Current.Path != "a.md" || ce.Current.Version != fi.Version {
		t.Fatalf("CreateExclusive on an existing file: err = %v, want *ConflictError with Current", err)
	}
	if got := read(t, fsys, "a.md"); got != "first" {
		t.Fatalf("CreateExclusive overwrote the file: %q", got)
	}
	mustMkdir(t, fsys, "d")
	if _, err := c.CreateExclusive(bg, "d", strings.NewReader("x")); !errors.Is(err, vfs.ErrConflict) {
		t.Fatalf("CreateExclusive onto a directory: err = %v, want ErrConflict", err)
	}
	if _, err := c.CreateExclusive(bg, "nodir/a.md", strings.NewReader("x")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("CreateExclusive under a missing directory: err = %v, want fs.ErrNotExist", err)
	}
	for _, name := range badNames {
		if _, err := c.CreateExclusive(bg, name, strings.NewReader("x")); !errors.Is(err, vfs.ErrInvalidName) {
			t.Errorf("CreateExclusive(%q): err = %v, want ErrInvalidName", name, err)
		}
	}
}

func testRenameNoReplace(t *testing.T, fsys vfs.FS, c vfs.NoReplaceRenamer) {
	write(t, fsys, "a.md", "A")
	write(t, fsys, "b.md", "B")
	err := c.RenameNoReplace(bg, "a.md", "b.md")
	skipUnsupported(t, err)
	var ce *vfs.ConflictError
	if !errors.As(err, &ce) || ce.Current.Path != "b.md" {
		t.Fatalf("RenameNoReplace onto an existing name: err = %v, want *ConflictError with Current", err)
	}
	if read(t, fsys, "b.md") != "B" || read(t, fsys, "a.md") != "A" {
		t.Fatalf("a refused no-replace rename changed something")
	}
	if err := c.RenameNoReplace(bg, "a.md", "c.md"); err != nil {
		t.Fatalf("RenameNoReplace onto a free name: %v", err)
	}
	if _, err := fsys.Stat(bg, "a.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("source still exists after RenameNoReplace: %v", err)
	}
	if got := read(t, fsys, "c.md"); got != "A" {
		t.Fatalf("renamed content = %q", got)
	}
	// directories too, both refused and moved
	mustMkdir(t, fsys, "d1/sub")
	write(t, fsys, "d1/sub/x.md", "X")
	mustMkdir(t, fsys, "d2")
	if err := c.RenameNoReplace(bg, "d1", "d2"); !errors.Is(err, vfs.ErrConflict) {
		t.Fatalf("directory RenameNoReplace onto an existing dir: err = %v, want ErrConflict", err)
	}
	if err := c.RenameNoReplace(bg, "d1", "d3"); err != nil {
		t.Fatalf("directory RenameNoReplace onto a free name: %v", err)
	}
	if got := read(t, fsys, "d3/sub/x.md"); got != "X" {
		t.Fatalf("moved tree content = %q", got)
	}
	for _, name := range badNames {
		if err := c.RenameNoReplace(bg, "c.md", name); !errors.Is(err, vfs.ErrInvalidName) {
			t.Errorf("RenameNoReplace(c.md, %q): err = %v, want ErrInvalidName", name, err)
		}
		if err := c.RenameNoReplace(bg, name, "free.md"); !errors.Is(err, vfs.ErrInvalidName) {
			t.Errorf("RenameNoReplace(%q, free.md): err = %v, want ErrInvalidName", name, err)
		}
	}
}

func testCopy(t *testing.T, fsys vfs.FS, c vfs.Copier) {
	write(t, fsys, "a.md", "A")
	fi, err := c.Copy(bg, "a.md", "b.md")
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if fi.Path != "b.md" || fi.Size != 1 {
		t.Fatalf("Copy FileInfo = %+v", fi)
	}
	if read(t, fsys, "a.md") != "A" || read(t, fsys, "b.md") != "A" {
		t.Fatalf("Copy content mismatch")
	}
	write(t, fsys, "c.md", "C")
	if _, err := c.Copy(bg, "c.md", "b.md"); err != nil {
		t.Fatalf("Copy over an existing file: %v", err)
	}
	if got := read(t, fsys, "b.md"); got != "C" {
		t.Fatalf("after replacing Copy, content = %q", got)
	}
	if _, err := c.Copy(bg, "nope.md", "x.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Copy from a missing name: err = %v, want fs.ErrNotExist", err)
	}
}
