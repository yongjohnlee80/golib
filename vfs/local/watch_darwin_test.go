//go:build darwin && cgo

package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/vfs"
)

// The FSEvents watch, against the real kernel. t.TempDir lives under
// /var, a symlink to /private/var — the real-path mapping is exercised by every test here.

func watchWith(t *testing.T, f *FS, dir string, opts ...vfs.WatchOption) (<-chan vfs.Event, context.CancelFunc) {
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

// none fails if, within d, an event satisfying bad arrives; it returns what it saw.
func none(t *testing.T, events <-chan vfs.Event, d time.Duration, bad func(vfs.Event) bool) []vfs.Event {
	t.Helper()
	var seen []vfs.Event
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return seen
			}
			seen = append(seen, ev)
			if bad(ev) {
				t.Fatalf("unexpected %v; saw %v", ev, seen)
			}
		case <-deadline:
			return seen
		}
	}
}

func TestDarwinWatchCreateWriteRemove(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	mustWrite(t, f, "w/a.md", "1")
	await(t, events, "w/a.md", vfs.OpCreate)
	mustWrite(t, f, "w/a.md", "2") // an atomic replace: a create (FSEvents flags accumulate) or a write
	awaitAny(t, events, "w/a.md", vfs.OpCreate, vfs.OpWrite)
	if err := f.Remove(bg, "w/a.md"); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/a.md", vfs.OpRemove)
}

// awaitAny is await for any of ops.
func awaitAny(t *testing.T, events <-chan vfs.Event, p string, ops ...vfs.Op) []vfs.Event {
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

// TestDarwinWatchInPlaceWrite: an append — no rename, as `echo >>` does — is reported.
func TestDarwinWatchInPlaceWrite(t *testing.T) {
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
	awaitAny(t, events, "w/a.md", vfs.OpCreate, vfs.OpWrite)
}

// TestDarwinWatchEditorSave: a temp-and-rename save reports the target and never the temp.
func TestDarwinWatchEditorSave(t *testing.T) {
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

// TestDarwinWatchMoveInPopulated: a populated directory moved in reports only itself, so the watch
// yields OpCreate and OpOverflow for it — and later changes inside it arrive.
func TestDarwinWatchMoveInPopulated(t *testing.T) {
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

// TestDarwinWatchMoveWithinAndOut: a move inside is OpRemove(old) + OpCreate(new); a move out, OpRemove.
func TestDarwinWatchMoveWithinAndOut(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w/a")
	mustWrite(t, f, "w/a/x.md", "x")
	events, _ := startWatch(t, f, "w")
	if err := f.Rename(bg, "w/a", "w/b"); err != nil {
		t.Fatal(err)
	}
	seen := await(t, events, "w/b", vfs.OpCreate)
	more := none(t, events, 300*time.Millisecond, func(vfs.Event) bool { return false })
	if all := append(seen, more...); !slices.Contains(all, vfs.Event{Path: "w/a", Op: vfs.OpRemove}) {
		t.Fatalf("no OpRemove for the old name: %v", all)
	}
	if err := os.Rename(filepath.Join(dir, "w", "b"), filepath.Join(t.TempDir(), "b")); err != nil {
		t.Fatal(err)
	}
	await(t, events, "w/b", vfs.OpRemove)
}

// TestDarwinWatchNotRecursive: only dir's direct children are reported, and a directory appearing is
// not overflowed.
func TestDarwinWatchNotRecursive(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w/sub")
	events, _ := watchWith(t, f, "w")
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

// TestDarwinWatchSubdirectoryPaths: a watch of a subdirectory reports root-relative paths.
func TestDarwinWatchSubdirectoryPaths(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "docs/guide")
	events, _ := startWatch(t, f, "docs/guide")
	mustWrite(t, f, "docs/guide/intro.md", "i")
	await(t, events, "docs/guide/intro.md", vfs.OpCreate)
}

// TestDarwinWatchSkipMovedIn: a skipped directory moved in populated is OpCreate alone, and
// nothing written inside it later is reported.
func TestDarwinWatchSkipMovedIn(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := watchWith(t, f, "w", vfs.Recursive(), vfs.SkipDirs(func(d string) bool { return filepath.Base(d) == "node_modules" }))
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
	seen = append(seen, none(t, events, 300*time.Millisecond, func(vfs.Event) bool { return false })...)
	for _, ev := range seen {
		if strings.HasPrefix(ev.Path, "w/node_modules/") || ev.Op == vfs.OpOverflow {
			t.Fatalf("the skipped directory leaked %v (all: %v)", ev, seen)
		}
	}
}

// TestDarwinWatchRootGone: deleting the watched directory yields a final OpOverflow{""}, then the close.
func TestDarwinWatchRootGone(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w/sub")
	events, _ := startWatch(t, f, "w")
	_ = os.RemoveAll(filepath.Join(dir, "w"))
	seen := drainClosed(t, events)
	if len(seen) == 0 || seen[len(seen)-1] != (vfs.Event{Path: "", Op: vfs.OpOverflow}) {
		t.Fatalf("last event = %v, want OpOverflow{\"\"}", seen)
	}
	if n := fseHandles.Load(); n != 0 {
		t.Fatalf("%d watches left in the handle table after the root went", n)
	}
}

// TestDarwinWatchCancelAndCloseRelease: a cancelled watch and a closed FS each close the channel and
// leave nothing in the handle table.
func TestDarwinWatchCancelAndCloseRelease(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	for range 5 {
		events, cancel := startWatch(t, f, "w")
		mustWrite(t, f, "w/x.md", "x")
		awaitAny(t, events, "w/x.md", vfs.OpCreate, vfs.OpWrite)
		cancel()
		drainClosed(t, events)
	}
	events, _ := startWatch(t, f, "w")
	_ = f.Close()
	drainClosed(t, events)
	if n := fseHandles.Load(); n != 0 {
		t.Fatalf("%d watches left in the handle table", n)
	}
}

// TestDarwinWatchSetupFailureUnwinds: a stream that cannot be created or started fails Watch and
// leaves nothing behind.
func TestDarwinWatchSetupFailureUnwinds(t *testing.T) {
	f, _ := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	for _, step := range []string{"create", "start"} {
		t.Run(step, func(t *testing.T) {
			hook := func(op string) error {
				if op == step {
					return errors.New("injected")
				}
				return nil
			}
			fseFault.Store(&hook)
			defer fseFault.Store(nil)
			before := fseHandles.Load()
			if _, err := f.Watch(bg, "w", vfs.Recursive()); err == nil || !strings.Contains(err.Error(), "FSEventStream") {
				t.Fatalf("Watch with a failing %s: err = %v", step, err)
			}
			if n := fseHandles.Load(); n != before {
				t.Fatalf("a failed %s left %d watches in the handle table", step, n-before)
			}
		})
	}
}

// TestDarwinWatchStalledConsumer: a consumer that falls behind the queue bound gets one OpOverflow{""}
// and then live events again.
func TestDarwinWatchStalledConsumer(t *testing.T) {
	fseQueueBound.Store(20)
	defer fseQueueBound.Store(fseQueueBoundDefault)
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "w")
	events, _ := startWatch(t, f, "w")
	for i := range 200 { // nothing reads: the run goroutine blocks on its first send, the queue fills
		_ = os.WriteFile(filepath.Join(dir, "w", fmt.Sprint("f", i, ".md")), nil, 0o644)
	}
	time.Sleep(300 * time.Millisecond) // FSEvents delivers the burst while nothing reads
	seen := await(t, events, "", vfs.OpOverflow)
	overflows := 0
	for _, ev := range seen {
		if ev.Op == vfs.OpOverflow {
			overflows++
		}
	}
	if overflows != 1 {
		t.Fatalf("%d overflows for one stall, want 1: %v", overflows, seen)
	}
	mustWrite(t, f, "w/live.md", "l")
	await(t, events, "w/live.md", vfs.OpCreate)
}

// BenchmarkDarwinWatchLatency records how long a write takes to arrive (recorded, not gated). Run with -bench, and read the p50/p99 it reports.
func BenchmarkDarwinWatchLatency(b *testing.B) {
	f, err := New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	_ = f.MkdirAll(bg, "w")
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	events, err := f.Watch(ctx, "w", vfs.Recursive())
	if err != nil {
		b.Fatal(err)
	}
	var lat []time.Duration
	for i := range b.N {
		p := fmt.Sprint("w/f", i, ".md")
		start := time.Now()
		if _, err := f.WriteFile(bg, p, strings.NewReader("x")); err != nil {
			b.Fatal(err)
		}
		for ev := range events {
			if ev.Path == p {
				lat = append(lat, time.Since(start))
				break
			}
		}
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	if len(lat) > 0 {
		b.ReportMetric(float64(lat[len(lat)/2].Microseconds())/1000, "p50-ms")
		b.ReportMetric(float64(lat[len(lat)*99/100].Microseconds())/1000, "p99-ms")
	}
}
