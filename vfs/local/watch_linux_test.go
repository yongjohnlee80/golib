//go:build linux

package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/vfs"
)

func startWatch(t *testing.T, f *FS, dir string) (<-chan vfs.Event, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(bg)
	ev, err := f.Watch(ctx, dir, vfs.Recursive())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { // the watch is fully released before the next test starts
		cancel()
		for range ev {
		}
	})
	return ev, cancel
}

// await reads until an event matching (p, op) arrives; it returns everything seen on the way.
func await(t *testing.T, events <-chan vfs.Event, p string, op vfs.Op) []vfs.Event {
	t.Helper()
	var seen []vfs.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("channel closed waiting for %v %s; saw %v", op, p, seen)
			}
			seen = append(seen, ev)
			if ev.Path == p && ev.Op == op {
				return seen
			}
		case <-deadline:
			t.Fatalf("no %v %s within 5s; saw %v", op, p, seen)
		}
	}
}

// drainClosed waits for the channel to close and returns what arrived.
func drainClosed(t *testing.T, events <-chan vfs.Event) []vfs.Event {
	t.Helper()
	var seen []vfs.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return seen
			}
			seen = append(seen, ev)
		case <-deadline:
			t.Fatalf("the event channel did not close; saw %v", seen)
		}
	}
}

// TestWatchMoveInPopulated: a populated directory moved in is watched and reported as overflowed, and
// later changes inside it arrive.
func TestWatchMoveInPopulated(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	staging := filepath.Join(dir, "staging")
	_ = os.MkdirAll(filepath.Join(staging, "deep"), 0o755)
	_ = os.WriteFile(filepath.Join(staging, "deep", "pre.md"), []byte("p"), 0o644)
	if err := os.Rename(staging, filepath.Join(dir, "w", "in")); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/in", vfs.OpOverflow)
	mustWrite(t, f, "w/in/deep/post.md", "q")
	await(t, events, "w/in/deep/post.md", vfs.OpCreate)
}

// TestWatchMoveWithin: after a directory moves inside the watch, its events carry the new path.
func TestWatchMoveWithin(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w/a/sub")
	events, _ := startWatch(t, f, "w")
	if err := f.Rename(bg, "w/a", "w/b"); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/a", vfs.OpRemove)
	await(t, events, "w/b", vfs.OpCreate)
	mustWrite(t, f, "w/b/sub/x.md", "x")
	seen := await(t, events, "w/b/sub/x.md", vfs.OpCreate)
	for _, ev := range seen {
		if strings.HasPrefix(ev.Path, "w/a/") {
			t.Fatalf("an event used the old path: %v", seen)
		}
	}
}

// TestWatchEditorSave: write-temp-then-rename arrives as OpCreate on the target; the driver's own
// temp files never appear.
func TestWatchEditorSave(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	mustWrite(t, f, "w/a.md", "1")
	events, _ := startWatch(t, f, "w")
	swp := filepath.Join(dir, "w", ".a.md.swp")
	_ = os.WriteFile(swp, []byte("2"), 0o644)
	_ = os.Rename(swp, filepath.Join(dir, "w", "a.md"))
	await(t, events, "w/a.md", vfs.OpCreate)
	mustWrite(t, f, "w/a.md", "3")
	for _, ev := range await(t, events, "w/a.md", vfs.OpCreate) {
		if strings.Contains(ev.Path, vfs.TempPrefix) {
			t.Fatalf("a driver temp file surfaced: %v", ev)
		}
	}
}

// TestWatchSymlinkSwap: a watched subdirectory replaced by a link to outside yields nothing from
// outside, and the watch refuses to be started through such a link.
func TestWatchSymlinkSwap(t *testing.T) {
	f, dir := newLocal(t)
	outside := t.TempDir()
	_ = f.MkdirAll(bg, "w/sub")
	events, _ := startWatch(t, f, "w")
	sub := filepath.Join(dir, "w", "sub")
	_ = os.Remove(sub)
	await(t, events, "w/sub", vfs.OpRemove)
	_ = os.Symlink(outside, sub)
	await(t, events, "w/sub", vfs.OpCreate)
	_ = os.WriteFile(filepath.Join(outside, "leak.md"), []byte("x"), 0o644)
	mustWrite(t, f, "w/marker.md", "m") // a later in-root event bounds the wait
	for _, ev := range await(t, events, "w/marker.md", vfs.OpCreate) {
		if strings.Contains(ev.Path, "leak") {
			t.Fatalf("an event from outside the root: %v", ev)
		}
	}
	_ = os.Symlink(outside, filepath.Join(dir, "out"))
	if _, err := f.Watch(bg, "out"); err == nil {
		t.Fatal("Watch through a link to outside succeeded")
	}
}

// watchFDs counts this process's inotify instances and pipes — the fds a watch opens.
func watchFDs(t *testing.T) int {
	t.Helper()
	e, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range e {
		l, err := os.Readlink(filepath.Join("/proc/self/fd", d.Name()))
		if err == nil && (l == "anon_inode:inotify" || strings.HasPrefix(l, "pipe:")) {
			n++
		}
	}
	return n
}

// TestWatchCancelReleases: cancel closes the channel and every fd the watch opened.
func TestWatchCancelReleases(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w/a/b/c")
	before := watchFDs(t)
	for range 5 {
		events, cancel := startWatch(t, f, "w")
		if during := watchFDs(t); during != before+3 {
			t.Fatalf("a live watch holds %d inotify/pipe fds, want 3", during-before)
		}
		mustWrite(t, f, "w/a/x.md", "x")
		await(t, events, "w/a/x.md", vfs.OpCreate)
		cancel()
		drainClosed(t, events)
	}
	if after := watchFDs(t); after != before {
		t.Fatalf("fds: %d before, %d after five watches", before, after)
	}
}

// TestWatchCloseEnds: closing the FS ends its watches.
func TestWatchCloseEnds(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	_ = f.Close()
	drainClosed(t, events)
}

// TestWatchRootGone: deleting the watched directory yields a final OpOverflow{""}, then the close.
func TestWatchRootGone(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w/sub")
	events, _ := startWatch(t, f, "w")
	_ = os.RemoveAll(filepath.Join(dir, "w"))
	seen := drainClosed(t, events)
	if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
		t.Fatalf("last event = %v, want OpOverflow{\"\"}", seen)
	}
}

// TestWatchOverflow: a consumer that stops reading makes the kernel queue overflow, and that is reported.
func TestWatchOverflow(t *testing.T) {
	b, err := os.ReadFile("/proc/sys/fs/inotify/max_queued_events")
	if err != nil {
		t.Skipf("no inotify limits: %v", err)
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if limit <= 0 || limit > 100_000 {
		t.Skipf("max_queued_events = %d: too large to flood in a test", limit)
	}
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	for i := range limit + 500 { // not reading: the reader stalls on its first send, the kernel queue fills
		_ = os.WriteFile(filepath.Join(dir, "w", fmt.Sprint("f", i)), nil, 0o644)
	}
	await(t, events, "", vfs.OpOverflow)
}

func TestPollLocal(t *testing.T) {
	f, _ := newLocal(t)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	_ = f.MkdirAll(bg, "p")
	events, err := vfs.Poll(ctx, f, "p", 20*time.Millisecond, vfs.Recursive())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, "p/a.md", "1")
	await(t, events, "p/a.md", vfs.OpCreate)
	time.Sleep(30 * time.Millisecond)
	mustWrite(t, f, "p/a.md", "22")
	await(t, events, "p/a.md", vfs.OpWrite)
}
