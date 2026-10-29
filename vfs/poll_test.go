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

// pollClock is a Poll's clock in a test's hands: each wait Poll asks for comes to the test, which
// ends it when it chooses. Poll lists only when a wait ends, so what the test changes while Poll
// waits is what the next listing finds, and the wait asked after it tells what that listing did.
type pollClock chan pollWait

type pollWait struct {
	d    time.Duration
	fire chan time.Time
}

func (c pollClock) after(d time.Duration) <-chan time.Time {
	w := pollWait{d, make(chan time.Time, 1)}
	c <- w
	return w.fire
}

// take is the wait Poll asks for next, not yet ended.
func (c pollClock) take(t *testing.T) pollWait {
	t.Helper()
	select {
	case w := <-c:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("Poll asked for no wait")
		return pollWait{}
	}
}

// end ends w: Poll lists, then asks for its next wait.
func (w pollWait) end() { w.fire <- time.Time{} }

// TestPollBackoff: each listing that finds no change doubles the wait, up to the backoff; a listing
// that finds a create, a write or a remove, or that fails, brings it back to the interval, and the
// listing after a failure is a new baseline that finds no change. Without the option, or with one
// not past the interval, every wait is the interval.
func TestPollBackoff(t *testing.T) {
	const iv, most = 10 * time.Millisecond, 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fsys := memfs.New()
	if err := fsys.MkdirAll(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	clock := make(pollClock, 1) // buffered: a wait asked for after ctx ends does not hold Poll
	evs, err := vfs.Poll(ctx, fsys, "d", iv, vfs.PollBackoff(most), vfs.PollAfter(clock.after))
	if err != nil {
		t.Fatal(err)
	}
	// expect is the next wait, which must be want: what the listing before it did
	expect := func(step string, want time.Duration) pollWait {
		t.Helper()
		w := clock.take(t)
		if w.d != want {
			t.Fatalf("%s: the wait is %v, want %v", step, w.d, want)
		}
		return w
	}
	event := func(step string, op vfs.Op) {
		t.Helper()
		select {
		case ev := <-evs:
			if ev.Op != op {
				t.Fatalf("%s: event %v, want op %v", step, ev, op)
			}
		default: // sent before the wait after it was asked for
			t.Fatalf("%s: no event", step)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if _, err := fsys.WriteFile(ctx, name, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}

	w := expect("first", iv)
	for _, d := range []time.Duration{2 * iv, 4 * iv, 8 * iv, most, most} {
		w.end()
		w = expect("idle", d)
	}
	write("d/a", "1")
	w.end()
	w = expect("create", iv)
	event("create", vfs.OpCreate)
	w.end()
	w = expect("idle after the create", 2*iv)
	write("d/a", "22")
	w.end()
	w = expect("write", iv)
	event("write", vfs.OpWrite)
	w.end()
	w = expect("idle after the write", 2*iv)
	if err := fsys.Remove(ctx, "d/a"); err != nil {
		t.Fatal(err)
	}
	w.end()
	w = expect("remove", iv)
	event("remove", vfs.OpRemove)
	w.end()
	w = expect("idle after the remove", 2*iv)
	if err := fsys.RemoveAll(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	w.end()
	w = expect("failed listing", iv)
	event("failed listing", vfs.OpOverflow)
	if err := fsys.MkdirAll(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	w.end()
	w = expect("new baseline", 2*iv)
	w.end()
	expect("idle after the baseline", 4*iv)

	for _, tc := range []struct {
		name string
		opts []vfs.WatchOption
	}{
		{"no backoff", nil},
		{"backoff at the interval", []vfs.WatchOption{vfs.PollBackoff(iv)}},
		{"backoff below the interval", []vfs.WatchOption{vfs.PollBackoff(iv / 2)}},
	} {
		clock := make(pollClock, 1)
		ctx, cancel := context.WithCancel(ctx)
		if _, err := vfs.Poll(ctx, fsys, "d", iv, append(tc.opts, vfs.PollAfter(clock.after))...); err != nil {
			t.Fatal(err)
		}
		for i := range 4 {
			w := clock.take(t)
			if w.d != iv {
				t.Errorf("%s: wait %d is %v, want the interval %v", tc.name, i, w.d, iv)
			}
			w.end()
		}
		cancel()
	}
}
