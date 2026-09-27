//go:build linux

package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

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

// injectFault makes the watch step op fail with err for rel, until the test ends.
func injectFault(t *testing.T, op, rel string, err error) {
	t.Helper()
	h := func(o, r string) error {
		if o == op && r == rel {
			return err
		}
		return nil
	}
	watchFault.Store(&h)
	t.Cleanup(func() { watchFault.Store(nil) })
}

// TestWatchSetupFailsLoudly: a subtree that cannot be watched at setup fails Watch — no silent gap —
// and releases everything the half-built watch opened.
func TestWatchSetupFailsLoudly(t *testing.T) {
	for _, c := range []struct {
		op, rel string
		err     error
	}{
		{"readdir", "w/a", unix.EACCES},
		{"addwatch", "w/a/b", unix.EMFILE},
		{"addwatch", "w/a", unix.ENOSPC},
	} {
		t.Run(c.op+" "+c.rel, func(t *testing.T) {
			f, _ := newLocal(t)
			_ = f.MkdirAll(bg, "w/a/b")
			before := watchFDs(t)
			injectFault(t, c.op, c.rel, c.err)
			ev, err := f.Watch(bg, "w", vfs.Recursive())
			if !errors.Is(err, c.err) {
				t.Fatalf("Watch = %v, %v; want an error wrapping %v", ev, err, c.err)
			}
			if after := watchFDs(t); after != before {
				t.Fatalf("a failed Watch leaked fds: %d before, %d after", before, after)
			}
		})
	}
}

// TestWatchSetupPermissionDenied: the same through a real unreadable directory.
func TestWatchSetupPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w/locked/in")
	locked := filepath.Join(dir, "w", "locked")
	_ = os.Chmod(locked, 0)
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := f.Watch(bg, "w", vfs.Recursive()); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Watch over an unreadable subtree: err = %v, want permission denied", err)
	}
}

// TestWatchAdoptFailureEndsWatch: a directory that appears later and cannot be watched ends the watch
// with OpOverflow{""} instead of staying silently unwatched.
func TestWatchAdoptFailureEndsWatch(t *testing.T) {
	for _, c := range []struct {
		name, op string
		appear   func(t *testing.T, f *FS, dir string)
	}{
		{"created", "addwatch", func(t *testing.T, f *FS, dir string) { _ = f.MkdirAll(bg, "w/new") }},
		{"moved in", "readdir", func(t *testing.T, f *FS, dir string) {
			_ = os.MkdirAll(filepath.Join(dir, "staging", "deep"), 0o755)
			_ = os.Rename(filepath.Join(dir, "staging"), filepath.Join(dir, "w", "new"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, dir := newLocal(t)
			_ = f.MkdirAll(bg, "w")
			injectFault(t, c.op, "w/new", unix.EMFILE)
			events, _ := startWatch(t, f, "w")
			c.appear(t, f, dir)
			seen := drainClosed(t, events)
			if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
				t.Fatalf("events = %v, want a final OpOverflow{\"\"} and the close", seen)
			}
		})
	}
}

// TestWatchMovedBeforeItsWatch: a directory that vanishes from under its first watch attempt — moved
// within the tree before the watch landed — is adopted at its new name.
func TestWatchMovedBeforeItsWatch(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	injectFault(t, "addwatch", "w/tmp", unix.ENOENT) // as if it moved away first
	events, _ := startWatch(t, f, "w")
	// Nothing is read until both are done: the reader stalls on its first send, so the move's
	// IN_MOVED_FROM and IN_MOVED_TO reach it in one read and pair — the case under test. (Split across
	// reads they would not pair, and the unpaired path adopts anyway.)
	_ = f.MkdirAll(bg, "w/tmp")
	if err := os.Rename(filepath.Join(dir, "w", "tmp"), filepath.Join(dir, "w", "final")); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/final", vfs.OpOverflow)
	mustWrite(t, f, "w/final/x.md", "x")
	await(t, events, "w/final/x.md", vfs.OpCreate)
}
