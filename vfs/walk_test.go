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
