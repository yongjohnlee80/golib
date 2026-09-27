package vfs_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
)

// TestPollConverges: Poll compares eventual state, so the cell holds each scripted state for longer than
// the interval and asserts the events that describe it — not an exact event sequence.
func TestPollConverges(t *testing.T) {
	pollConverges(t, memfs.New())
}

func pollConverges(t *testing.T, fsys vfs.FS) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := fsys.MkdirAll(ctx, "p/sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.WriteFile(ctx, "p/keep.md", strings.NewReader("k")); err != nil {
		t.Fatal(err)
	}
	events, err := vfs.Poll(ctx, fsys, "p", 20*time.Millisecond, vfs.Recursive())
	if err != nil {
		t.Fatal(err)
	}
	expect := func(p string, op vfs.Op) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case ev := <-events:
				if ev.Path == p && ev.Op == op {
					return
				}
			case <-deadline:
				t.Fatalf("Poll: no %v for %s", op, p)
			}
		}
	}
	if _, err := fsys.WriteFile(ctx, "p/sub/a.md", strings.NewReader("1")); err != nil {
		t.Fatal(err)
	}
	expect("p/sub/a.md", vfs.OpCreate)
	if _, err := fsys.WriteFile(ctx, "p/sub/a.md", strings.NewReader("22")); err != nil {
		t.Fatal(err)
	}
	expect("p/sub/a.md", vfs.OpWrite)
	if err := fsys.Remove(ctx, "p/sub/a.md"); err != nil {
		t.Fatal(err)
	}
	expect("p/sub/a.md", vfs.OpRemove)
	cancel()
	for range events {
	}
}
