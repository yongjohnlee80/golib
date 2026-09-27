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
