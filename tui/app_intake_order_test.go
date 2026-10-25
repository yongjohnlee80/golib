package tui

import (
	"sync"
	"testing"
	"time"
)

// app_intake_order_test.go: the intake's resize slot keeps its place in the input (app_intake.go,
// invariant 2). Each cell holds the loop inside an Update while it injects, so everything waits in
// the intake together, and the order the intake hands it on is the order it chose, not a race.

// stallLoop holds the loop inside an Update until the returned release is closed.
func stallLoop(t *testing.T, h *harness) (release chan struct{}) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	h.app.Update(func() { close(entered); <-release })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the loop never entered the stalling Update")
	}
	return release
}

// absorbed waits for the intake to have taken everything the backend holds, so the order it
// delivers in is settled before the loop resumes.
func absorbed(t *testing.T, h *harness) {
	t.Helper()
	waitFor(t, "the intake absorbed the script", func() bool { return len(h.tb.events) == 0 })
}

// seen is an input event as the application got it: what it was, and how wide the App was then.
type seen struct {
	what string
	w    int
}

// widthProbe is a root that records each key and pointer event it gets with the App's width at
// that moment (app is set before any input is injected).
func widthProbe() (*probe, func() []seen, **App) {
	var mu sync.Mutex
	var got []seen
	app := new(*App)
	root := &probe{name: "root", fill: "A"}
	root.onEvent = func(_ *probe, ev Event) bool {
		var what string
		switch e := ev.(type) {
		case KeyEvent:
			what = e.Text
		case MouseEvent:
			if e.Kind != MouseMotion {
				return false
			}
			what = "motion" + string(rune('0'+e.X))
		default:
			return false
		}
		mu.Lock()
		got = append(got, seen{what, (*app).size.W})
		mu.Unlock()
		return true
	}
	return root, func() []seen {
		mu.Lock()
		defer mu.Unlock()
		return append([]seen(nil), got...)
	}, app
}

func startWidthProbe(t *testing.T, w, h int, opts ...AppOption) (*harness, func() []seen) {
	t.Helper()
	root, got, app := widthProbe()
	hn := startApp(t, root, w, h, opts...)
	*app = hn.app
	return hn, got
}

// waitSeen waits for n events and returns them.
func waitSeen(t *testing.T, got func() []seen, n int) []seen {
	t.Helper()
	waitFor(t, "the events delivered", func() bool { return len(got()) >= n })
	return got()
}

func sameSeen(a, b []seen) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A key typed while the too-small screen shows, with an enlarging resize right behind it, is the
// too-small screen's: the resize is delivered after it, so the application never gets it. The next
// key, after the enlargement, is the application's.
func TestAKeyTypedUnderTheTooSmallScreenStaysThere(t *testing.T) {
	t.Parallel()
	h, got := startWidthProbe(t, 30, 8, WithMinimumSize(Size{W: 40, H: 10}))
	waitFor(t, "the too-small screen", func() bool { return screenHas(h.tb, "30 × 8 — needs 40 × 10") })
	release := stallLoop(t, h)
	h.inject(KeyEvent{Code: 'x', Text: "x"})
	h.tb.InjectResize(50, 12)
	absorbed(t, h)
	close(release)
	waitFor(t, "the application again", func() bool { return !screenHas(h.tb, "Screen too small") && screenHas(h.tb, "A") })
	h.inject(KeyEvent{Code: 'y', Text: "y"})
	if all := waitSeen(t, got, 1); !sameSeen(all, []seen{{"y", 50}}) {
		t.Fatalf("the application got %+v, want only y, at 50: x was typed under the too-small screen", all)
	}
}

// A key, a resize, a key: the first key is delivered at the old size, the resize, then the second
// key at the new one.
func TestAResizeIsDeliveredInItsPlace(t *testing.T) {
	t.Parallel()
	h, got := startWidthProbe(t, 20, 5)
	release := stallLoop(t, h)
	h.inject(KeyEvent{Code: 'a', Text: "a"})
	h.tb.InjectResize(30, 5)
	h.inject(KeyEvent{Code: 'b', Text: "b"})
	absorbed(t, h)
	close(release)
	if all := waitSeen(t, got, 2); !sameSeen(all, []seen{{"a", 20}, {"b", 30}}) {
		t.Fatalf("got %+v, want a at 20 then b at 30", all)
	}
}

// A storm stays latest-wins in its place: a resize, a key, a second resize, a key. The first
// resize is superseded and never applied; the key between is delivered at the old size and the
// last key at the final one, and one resize is delivered.
func TestAResizeStormStaysLatestWinsInItsPlace(t *testing.T) {
	t.Parallel()
	h, got := startWidthProbe(t, 20, 5)
	resizes := 0
	h.onLoop(func() { SubscribeScoped(h.app.root.(*probe).ctx, func(ResizeEvent) { resizes++ }) })
	release := stallLoop(t, h)
	h.tb.InjectResize(25, 5)
	h.inject(KeyEvent{Code: 'a', Text: "a"})
	h.tb.InjectResize(30, 5)
	h.inject(KeyEvent{Code: 'b', Text: "b"})
	absorbed(t, h)
	close(release)
	if all := waitSeen(t, got, 2); !sameSeen(all, []seen{{"a", 20}, {"b", 30}}) {
		t.Fatalf("got %+v, want a at 20 (the first resize superseded) then b at 30", all)
	}
	h.sync()
	h.sync()
	var n int
	h.onLoop(func() { n = resizes })
	if n != 1 {
		t.Fatalf("%d resizes delivered, want one, the latest", n)
	}
}

// The overflow drops the oldest event; one dropped from ahead of the resize moves the resize one
// place forward, and it still comes after what was queued before it.
func TestAnOverflowDropAheadOfTheResizeKeepsItsPlace(t *testing.T) {
	t.Parallel()
	h, got := startWidthProbe(t, 20, 5, WithInputQueueSize(2))
	release := stallLoop(t, h)
	h.inject(KeyEvent{Code: 'a', Text: "a"}, KeyEvent{Code: 'b', Text: "b"})
	h.tb.InjectResize(30, 5)
	h.inject(KeyEvent{Code: 'c', Text: "c"}) // the queue is full: a, the oldest, is dropped
	absorbed(t, h)
	close(release)
	if all := waitSeen(t, got, 2); !sameSeen(all, []seen{{"b", 20}, {"c", 30}}) {
		t.Fatalf("got %+v, want b at 20 then c at 30 (a dropped)", all)
	}
}

// Motions coalesce only on the same side of a resize: one aimed at the old size is never replaced
// by one aimed at the new.
func TestMotionNeverCoalescesAcrossAResize(t *testing.T) {
	t.Parallel()
	h, got := startWidthProbe(t, 20, 5)
	release := stallLoop(t, h)
	h.inject(MouseEvent{Kind: MouseMotion, X: 1, Y: 0})
	h.tb.InjectResize(30, 5)
	h.inject(MouseEvent{Kind: MouseMotion, X: 2, Y: 0})
	absorbed(t, h)
	close(release)
	if all := waitSeen(t, got, 2); !sameSeen(all, []seen{{"motion1", 20}, {"motion2", 30}}) {
		t.Fatalf("got %+v, want motion1 at 20 then motion2 at 30", all)
	}
}
