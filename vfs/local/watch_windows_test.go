//go:build windows

package local

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/vfs"
)

// The ReadDirectoryChangesW watch, against the real kernel: the cells macOS's FSEvents watch keeps,
// and the watched directory moved, which Windows reports nothing about.

func winWatchWith(t *testing.T, f *FS, dir string, opts ...vfs.WatchOption) (<-chan vfs.Event, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(bg)
	ev, err := f.Watch(ctx, dir, opts...)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		for range ev {
		}
	})
	return ev, cancel
}

// winAwaitAny is await for any of ops.
func winAwaitAny(t *testing.T, events <-chan vfs.Event, p string, ops ...vfs.Op) []vfs.Event {
	t.Helper()
	var seen []vfs.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("channel closed waiting for %v %s; saw %v", ops, p, seen)
			}
			seen = append(seen, ev)
			if ev.Path == p && slices.Contains(ops, ev.Op) {
				return seen
			}
		case <-deadline:
			t.Fatalf("no %v %s within 5s; saw %v", ops, p, seen)
		}
	}
}

// winQuiet returns what arrives within d.
func winQuiet(events <-chan vfs.Event, d time.Duration) []vfs.Event {
	var seen []vfs.Event
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return seen
			}
			seen = append(seen, ev)
		case <-deadline:
			return seen
		}
	}
}

func TestWindowsWatchCreateWriteRemove(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	mustWrite(t, f, "w/a.md", "1")
	await(t, events, "w/a.md", vfs.OpCreate)
	mustWrite(t, f, "w/a.md", "2") // vfs's atomic replace: the name goes and comes back, a create
	winAwaitAny(t, events, "w/a.md", vfs.OpCreate, vfs.OpWrite)
	if err := f.Remove(bg, "w/a.md"); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/a.md", vfs.OpRemove)
}

// An append, no rename, is a write.
func TestWindowsWatchInPlaceWrite(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	mustWrite(t, f, "w/a.md", "1")
	events, _ := startWatch(t, f, "w")
	fh, err := os.OpenFile(filepath.Join(dir, "w", "a.md"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString("more")
	_ = fh.Close()
	await(t, events, "w/a.md", vfs.OpWrite)
}

// A temp-and-rename save reports the target and never the temp.
func TestWindowsWatchEditorSave(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	mustWrite(t, f, "w/a.md", "1")
	events, _ := startWatch(t, f, "w")
	tmp := filepath.Join(dir, "w", ".a.md.swp")
	_ = os.WriteFile(tmp, []byte("2"), 0o644)
	_ = os.Rename(tmp, filepath.Join(dir, "w", "a.md"))
	mustWrite(t, f, "w/b.md", "x") // vfs's own atomic write, through a .vfs-tmp- file
	seen := await(t, events, "w/b.md", vfs.OpCreate)
	for _, ev := range seen {
		if strings.Contains(ev.Path, vfs.TempPrefix) {
			t.Fatalf("a temp file was reported: %v (all: %v)", ev, seen)
		}
	}
	if !slices.ContainsFunc(seen, func(ev vfs.Event) bool { return ev.Path == "w/a.md" }) {
		t.Fatalf("the saved target was not reported: %v", seen)
	}
}

// A populated directory moved in reports only itself, so the watch yields OpCreate and OpOverflow
// for it, and later changes inside it arrive.
func TestWindowsWatchMoveInPopulated(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	out := t.TempDir()
	_ = os.MkdirAll(filepath.Join(out, "sub", "deep"), 0o755)
	_ = os.WriteFile(filepath.Join(out, "sub", "deep", "x.md"), nil, 0o644)
	if err := os.Rename(filepath.Join(out, "sub"), filepath.Join(dir, "w", "sub")); err != nil {
		t.Fatal(err)
	}
	seen := await(t, events, "w/sub", vfs.OpOverflow)
	if !slices.Contains(seen, vfs.Event{Path: "w/sub", Op: vfs.OpCreate}) {
		t.Fatalf("no OpCreate before the overflow: %v", seen)
	}
	mustWrite(t, f, "w/sub/deep/y.md", "y")
	await(t, events, "w/sub/deep/y.md", vfs.OpCreate)
}

// A move inside is OpRemove(old) and OpCreate(new); a move out, OpRemove.
func TestWindowsWatchMoveWithinAndOut(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w/a")
	mustWrite(t, f, "w/a/x.md", "x")
	events, _ := startWatch(t, f, "w")
	if err := f.Rename(bg, "w/a", "w/b"); err != nil {
		t.Fatal(err)
	}
	seen := await(t, events, "w/b", vfs.OpCreate)
	if all := append(seen, winQuiet(events, 300*time.Millisecond)...); !slices.Contains(all, vfs.Event{Path: "w/a", Op: vfs.OpRemove}) {
		t.Fatalf("no OpRemove for the old name: %v", all)
	}
	if err := os.Rename(filepath.Join(dir, "w", "b"), filepath.Join(t.TempDir(), "b")); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/b", vfs.OpRemove)
}

// Only dir's direct children are reported, and a directory appearing is not overflowed.
func TestWindowsWatchNotRecursive(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w/sub")
	events, _ := winWatchWith(t, f, "w")
	mustWrite(t, f, "w/sub/deep.md", "d")
	_ = f.MkdirAll(bg, "w/new")
	mustWrite(t, f, "w/top.md", "t")
	seen := await(t, events, "w/top.md", vfs.OpCreate)
	for _, ev := range seen {
		if strings.HasPrefix(ev.Path, "w/sub/") || ev.Op == vfs.OpOverflow {
			t.Fatalf("a non-recursive watch reported %v (all: %v)", ev, seen)
		}
	}
}

// A watch of a subdirectory reports root-relative, slash-separated paths, from deeper too.
func TestWindowsWatchSubdirectoryPaths(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "docs/guide/part")
	events, _ := startWatch(t, f, "docs/guide")
	mustWrite(t, f, "docs/guide/part/intro.md", "i")
	await(t, events, "docs/guide/part/intro.md", vfs.OpCreate)
}

// A skipped directory moved in populated is OpCreate alone, and nothing written inside it later is
// reported.
func TestWindowsWatchSkipMovedIn(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := winWatchWith(t, f, "w", vfs.Recursive(), vfs.SkipDirs(func(d string) bool { return filepath.Base(d) == "node_modules" }))
	out := t.TempDir()
	_ = os.MkdirAll(filepath.Join(out, "node_modules", "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(out, "node_modules", "pkg", "README.md"), nil, 0o644)
	if err := os.Rename(filepath.Join(out, "node_modules"), filepath.Join(dir, "w", "node_modules")); err != nil {
		t.Fatal(err)
	}
	seen := await(t, events, "w/node_modules", vfs.OpCreate)
	mustWrite(t, f, "w/node_modules/pkg/later.md", "l")
	mustWrite(t, f, "w/after.md", "a")
	seen = append(seen, await(t, events, "w/after.md", vfs.OpCreate)...)
	seen = append(seen, winQuiet(events, 300*time.Millisecond)...)
	for _, ev := range seen {
		if strings.HasPrefix(ev.Path, "w/node_modules/") || ev.Op == vfs.OpOverflow {
			t.Fatalf("the skipped directory leaked %v (all: %v)", ev, seen)
		}
	}
}

// Deleting the watched directory yields a final OpOverflow{""}, then the close, and leaves no
// handle open.
func TestWindowsWatchRootGone(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w/sub")
	events, _ := startWatch(t, f, "w")
	_ = os.RemoveAll(filepath.Join(dir, "w"))
	seen := drainClosed(t, events)
	if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
		t.Fatalf("last event = %v, want OpOverflow{\"\"}", seen)
	}
	if n := winWatches.Load(); n != 0 {
		t.Fatalf("%d watch handles left open after the root went", n)
	}
}

// The watched directory moved within the root, which reports nothing inside it, is noticed by the
// root check: a final OpOverflow{""}, then the close.
func TestWindowsWatchRootMoved(t *testing.T) {
	old := winRootCheck
	winRootCheck = 50 * time.Millisecond
	t.Cleanup(func() { winRootCheck = old })
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	if err := f.Rename(bg, "w", "moved"); err != nil {
		t.Fatal(err)
	}
	seen := drainClosed(t, events)
	if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
		t.Fatalf("last event = %v, want OpOverflow{\"\"}", seen)
	}
}

// The watched directory moved and then kept busy, its changes completing every read before the idle
// check could come, is noticed all the same: a final OpOverflow{""}, then the close.
func TestWindowsWatchBusyRootMoved(t *testing.T) {
	old := winRootCheck
	winRootCheck = 50 * time.Millisecond
	t.Cleanup(func() { winRootCheck = old })
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	if err := f.Rename(bg, "w", "moved"); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { // a write every 5 ms in the moved directory: no read waits out the 50 ms check
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			_ = os.WriteFile(filepath.Join(dir, "moved", "busy-"+strconv.Itoa(i%20)+".md"), []byte(strconv.Itoa(i)), 0o644)
		}
	}()
	seen := drainClosed(t, events)
	close(stop)
	<-done
	if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
		t.Fatalf("last event = %v, want OpOverflow{\"\"}", seen)
	}
}

// A cancelled watch and a closed FS each close the channel and leave no handle open.
func TestWindowsWatchCancelAndCloseRelease(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	for range 5 {
		events, cancel := startWatch(t, f, "w")
		mustWrite(t, f, "w/x.md", "x")
		winAwaitAny(t, events, "w/x.md", vfs.OpCreate, vfs.OpWrite)
		cancel()
		drainClosed(t, events)
	}
	events, _ := startWatch(t, f, "w")
	_ = f.Close()
	drainClosed(t, events)
	if n := winWatches.Load(); n != 0 {
		t.Fatalf("%d watch handles left open", n)
	}
}

// A consumer that stops reading while the tree changes past the kernel's buffer is told to re-read
// everything, OpOverflow{""}, once it reads again.
func TestWindowsWatchStalledConsumer(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	mustWrite(t, f, "w/first.md", "1")
	time.Sleep(200 * time.Millisecond) // the watch is now blocked sending first.md's event
	for i := range 3000 {              // ~3000 records of 40-60 bytes each: past the 64 KiB buffer
		_ = os.WriteFile(filepath.Join(dir, "w", "file-with-a-long-name-"+strings.Repeat("x", 8)+"-"+itoa(i)+".md"), nil, 0o644)
	}
	await(t, events, "", vfs.OpOverflow)
}

func itoa(i int) string { return strconv.Itoa(i) }

// Watch of a file, or of a name not there, fails at once.
func TestWindowsWatchSetupErrors(t *testing.T) {
	f, _ := newLocal(t)
	mustWrite(t, f, "a.md", "a")
	if _, err := f.Watch(bg, "a.md"); err == nil {
		t.Fatal("Watch of a file succeeded")
	}
	if _, err := f.Watch(bg, "missing"); err == nil {
		t.Fatal("Watch of a missing directory succeeded")
	}
	if n := winWatches.Load(); n != 0 {
		t.Fatalf("%d watch handles left open after failed setups", n)
	}
}
