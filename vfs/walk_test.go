package vfs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
)

func TestWalkStopsEarlyAndRejectsFileStart(t *testing.T) {
	ctx := context.Background()
	fsys := memfs.New()
	_ = fsys.MkdirAll(ctx, "a/b")
	_, _ = fsys.WriteFile(ctx, "a/b/x.md", strings.NewReader("x"))
	n := 0
	for _, err := range vfs.Walk(ctx, fsys, ".") {
		if err != nil {
			t.Fatal(err)
		}
		n++
		break
	}
	if n != 1 {
		t.Fatalf("early stop yielded %d", n)
	}
	var got []error
	for _, err := range vfs.Walk(ctx, fsys, "a/b/x.md") {
		got = append(got, err)
	}
	if len(got) != 1 || !errors.Is(got[0], errs.ErrInvalidArgument) {
		t.Fatalf("Walk from a file = %v, want one ErrInvalidArgument", got)
	}
	got = got[:0]
	for _, err := range vfs.Walk(ctx, fsys, "missing") {
		got = append(got, err)
	}
	if len(got) != 1 || !errors.Is(got[0], vfs.ErrNotExist) {
		t.Fatalf("Walk from a missing dir = %v, want one ErrNotExist", got)
	}
}

// failingDir wraps an FS so that listing one directory fails.
type failingDir struct {
	vfs.FS
	dir string
}

func (f failingDir) ReadDir(ctx context.Context, name string) ([]vfs.FileInfo, error) {
	if name == f.dir {
		return nil, errs.Wrap(errs.ErrUnsupported, "listing %s refused", name)
	}
	return f.FS.ReadDir(ctx, name)
}

// TestWalkContinuesPastAnUnreadableDirectory: the failing subtree is reported once and hides only itself;
// a sibling after it is still walked, and breaking on the error stops the walk.
func TestWalkContinuesPastAnUnreadableDirectory(t *testing.T) {
	ctx := context.Background()
	mem := memfs.New()
	_ = mem.MkdirAll(ctx, "a/deep")
	_ = mem.MkdirAll(ctx, "b")
	_, _ = mem.WriteFile(ctx, "a/deep/x.md", strings.NewReader("x"))
	_, _ = mem.WriteFile(ctx, "b/new.md", strings.NewReader("n"))
	fsys := failingDir{FS: mem, dir: "a"}

	var paths, failed []string
	for fi, err := range vfs.Walk(ctx, fsys, ".") {
		if err != nil {
			failed = append(failed, fi.Path)
			continue
		}
		paths = append(paths, fi.Path)
	}
	if want := []string{"a", "b", "b/new.md"}; strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("walked %v, want %v (the sibling after the unreadable subtree must be walked)", paths, want)
	}
	if len(failed) != 1 || failed[0] != "a" {
		t.Fatalf("errors for %v, want exactly one for a", failed)
	}

	n := 0
	for _, err := range vfs.Walk(ctx, fsys, ".") {
		n++
		if err != nil {
			break
		}
	}
	if n != 2 { // "a", then its error
		t.Fatalf("breaking on the first error yielded %d items, want 2", n)
	}
}

// cancelOnList wraps an FS so that listing one directory cancels the walk's ctx, as a deadline or a
// shutdown would mid-walk, and fails with ctx's error.
type cancelOnList struct {
	vfs.FS
	dir    string
	cancel context.CancelFunc
	err    error // what the listing reports; nil means ctx's own error
}

func (f cancelOnList) ReadDir(ctx context.Context, name string) ([]vfs.FileInfo, error) {
	if name == f.dir {
		f.cancel()
		if f.err != nil {
			return nil, f.err
		}
		return nil, ctx.Err()
	}
	return f.FS.ReadDir(ctx, name)
}

// TestWalkStopsWhenCtxEnds: a listing that fails because ctx ended stops the whole walk — ctx's error is
// yielded once, last, and no later sibling (file or directory) is yielded.
func TestWalkStopsWhenCtxEnds(t *testing.T) {
	// the listing reports ctx's error, or an unrelated one: either way ctx ended, and ctx's error is last
	for _, listErr := range []error{nil, errs.Wrap(errs.ErrUnsupported, "unrelated")} {
		walkStopsWhenCtxEnds(t, listErr)
	}
}

func walkStopsWhenCtxEnds(t *testing.T, listErr error) {
	t.Helper()
	mem := memfs.New()
	bg := context.Background()
	_ = mem.MkdirAll(bg, "a")
	_ = mem.MkdirAll(bg, "c")
	_, _ = mem.WriteFile(bg, "b.md", strings.NewReader("b"))
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	var got []string
	var errsSeen []error
	for fi, err := range vfs.Walk(ctx, cancelOnList{FS: mem, dir: "a", cancel: cancel, err: listErr}, ".") {
		if err != nil {
			errsSeen = append(errsSeen, err)
			continue // a consumer that keeps going must still see the walk end
		}
		if len(errsSeen) > 0 {
			t.Fatalf("an entry after ctx's error: %s", fi.Path)
		}
		got = append(got, fi.Path)
	}
	if len(errsSeen) != 1 || !errors.Is(errsSeen[0], context.Canceled) {
		t.Fatalf("errors = %v, want exactly one context.Canceled", errsSeen)
	}
	if strings.Join(got, ",") != "a" {
		t.Fatalf("walked %v, want only a (b.md and c come after the cancelled listing)", got)
	}
}

// TestWalkStopsWhenTheConsumerCancels: ctx cancelled between two entries of one directory ends the walk
// before the next entry — ctx is checked per entry, not only per directory.
func TestWalkStopsWhenTheConsumerCancels(t *testing.T) {
	mem := memfs.New()
	bg := context.Background()
	for _, n := range []string{"x1.md", "x2.md", "x3.md"} {
		_, _ = mem.WriteFile(bg, n, strings.NewReader(n))
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	var got []string
	var errsSeen []error
	for fi, err := range vfs.Walk(ctx, mem, ".") {
		if err != nil {
			errsSeen = append(errsSeen, err)
			continue
		}
		got = append(got, fi.Path)
		cancel() // after the first entry
	}
	if strings.Join(got, ",") != "x1.md" || len(errsSeen) != 1 || !errors.Is(errsSeen[0], context.Canceled) {
		t.Fatalf("walked %v, errors %v; want only x1.md, then one context.Canceled", got, errsSeen)
	}
}
