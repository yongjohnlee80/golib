//go:build linux || (darwin && cgo)

package local

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/vfs"
)

// The watch tests' helpers, shared by the inotify (Linux) and FSEvents (macOS) suites.

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
