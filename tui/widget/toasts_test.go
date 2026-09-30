package widget_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// toastClock is a clock a test moves.
type toastClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *toastClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *toastClock) add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// toastScreen is a page of dots over a status row, with the toasts attached over it.
func toastScreen(t *testing.T, opts ...widget.ToastsOption) (*harness, *widget.Toasts, *toastClock) {
	t.Helper()
	clock := &toastClock{now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	toasts := widget.NewToasts(append([]widget.ToastsOption{widget.WithToastClock(clock.Now), widget.WithToastMargin(1),
		widget.WithToastWidth(24)}, opts...)...)
	page := widget.NewText(strings.Repeat(strings.Repeat(".", 40)+"\n", 11)+"STATUS", widget.WithWrapMode(widget.Wrap))
	host := widget.NewOverlayHost(page)
	host.Attach(toasts.Float())
	h := startApp(t, host, 40, 12)
	return h, toasts, clock
}

// tick is the clock's second passing, as the stack's timer delivers it.
func tick(h *harness, toasts *widget.Toasts) {
	h.onLoop(func() { toasts.HandleEvent(tui.TickEvent{}) })
	h.settle()
}

// TestAToastShowsAboveTheStatusLine: a toast is a box at the bottom right, its age in its border,
// the margin row under it left as it was; finished, it lingers, then goes.
func TestAToastShowsAboveTheStatusLine(t *testing.T) {
	h, toasts, clock := toastScreen(t)
	h.onLoop(func() { toasts.Post(widget.Toast{Text: "saved a.md"}) })
	h.waitFor("the toast", func() bool { return strings.Contains(h.row(9), "saved a.md") })
	if top := h.row(8); !strings.HasSuffix(top, "╮") || !strings.Contains(top, " now ") {
		t.Fatalf("the top border %q, want its age, now, at the right", top)
	}
	if bottom := h.row(10); !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("the bottom border %q, want it on the row above the margin", bottom)
	}
	if status := h.row(11); !strings.HasPrefix(status, "STATUS") {
		t.Fatalf("the margin row %q, want the status line under it untouched", status)
	}
	if left := h.row(9); !strings.HasPrefix(left, "......") {
		t.Fatalf("the row %q, want the page left of the toast", left)
	}
	clock.add(2 * time.Second)
	tick(h, toasts)
	if !strings.Contains(h.row(9), "saved a.md") {
		t.Fatal("the toast went before its linger")
	}
	clock.add(time.Second)
	tick(h, toasts)
	h.waitFor("the toast gone", func() bool { return !strings.Contains(h.row(9), "saved") })
	if s, w := toastsLen(h, toasts); s != 0 || w != 0 {
		t.Fatalf("after the linger: %d showing, %d waiting", s, w)
	}
}

func toastsLen(h *harness, toasts *widget.Toasts) (showing, waiting int) {
	h.onLoop(func() { showing, waiting = toasts.Len() })
	return
}

// TestToastsStackQueueAndSayHowLongAgo: three show, the newest nearest the corner; a fourth
// waits, and when a place opens it shows its age from when it was posted.
func TestToastsStackQueueAndSayHowLongAgo(t *testing.T) {
	h, toasts, clock := toastScreen(t)
	h.onLoop(func() {
		for _, s := range []string{"one", "two", "three", "four"} {
			toasts.Post(widget.Toast{Text: s})
		}
	})
	h.waitFor("three stacked", func() bool {
		return strings.Contains(h.row(3), "one") && strings.Contains(h.row(6), "two") && strings.Contains(h.row(9), "three")
	})
	if s, w := toastsLen(h, toasts); s != 3 || w != 1 {
		t.Fatalf("%d showing, %d waiting; want 3 and 1", s, w)
	}
	if strings.Contains(h.screen(), "four") {
		t.Fatal("the fourth shows beyond the maximum")
	}
	clock.add(20 * time.Second) // the three have lingered; the fourth waited all along
	tick(h, toasts)
	h.waitFor("the fourth, late", func() bool { return strings.Contains(h.row(9), "four") })
	if top := h.row(8); !strings.Contains(top, " 20s ago ") {
		t.Fatalf("the late toast's border %q, want its age since it was posted", top)
	}
	for _, s := range []string{"one", "two", "three"} {
		if strings.Contains(h.screen(), s) {
			t.Fatalf("%q still shows after its linger", s)
		}
	}
}

// TestAnOngoingToastStaysUntilDone: an ongoing toast stays however long it runs, is replaced in
// place by a post with its ID, and lingers once finished.
func TestAnOngoingToastStaysUntilDone(t *testing.T) {
	h, toasts, clock := toastScreen(t)
	h.onLoop(func() { toasts.Post(widget.Toast{ID: "embed", Text: "embedding 1/10", Ongoing: true}) })
	h.waitFor("the progress", func() bool { return strings.Contains(h.row(9), "embedding 1/10") })
	clock.add(90 * time.Second)
	tick(h, toasts)
	if !strings.Contains(h.row(9), "embedding 1/10") || !strings.Contains(h.row(8), " 1m ago ") {
		t.Fatalf("after 90s the ongoing toast is %q under %q", h.row(9), h.row(8))
	}
	h.onLoop(func() { toasts.Post(widget.Toast{ID: "embed", Text: "embedding 5/10", Ongoing: true}) })
	h.waitFor("updated in place", func() bool { return strings.Contains(h.row(9), "embedding 5/10") })
	if s, _ := toastsLen(h, toasts); s != 1 {
		t.Fatalf("%d toasts showing after the update, want the one", s)
	}
	h.onLoop(func() { toasts.Done("embed") })
	clock.add(2 * time.Second)
	tick(h, toasts)
	if !strings.Contains(h.row(9), "embedding 5/10") {
		t.Fatal("the finished toast went before its linger")
	}
	clock.add(time.Second)
	tick(h, toasts)
	h.waitFor("gone", func() bool { return !strings.Contains(h.row(9), "embedding") })

	// finished by a post without Ongoing
	h.onLoop(func() { toasts.Post(widget.Toast{ID: "embed", Text: "embedding 1/2", Ongoing: true}) })
	h.waitFor("again", func() bool { return strings.Contains(h.row(9), "embedding 1/2") })
	h.onLoop(func() { toasts.Post(widget.Toast{ID: "embed", Text: "embedded 2"}) })
	clock.add(3 * time.Second)
	tick(h, toasts)
	h.waitFor("finished by the post", func() bool { return !strings.Contains(h.screen(), "embedd") })
}

// TestToastsInAnotherCornerLongerAndWrapped: at the top left the newest is at the top, under the
// margin; a long message wraps to the toast's width; the linger is what was set.
func TestToastsInAnotherCornerLongerAndWrapped(t *testing.T) {
	h, toasts, clock := toastScreen(t, widget.WithToastCorner(widget.TopLeft), widget.WithToastLinger(10*time.Second))
	h.onLoop(func() {
		toasts.Post(widget.Toast{Text: "old"})
		toasts.Post(widget.Toast{Text: "a message long enough to wrap onto a second line"})
	})
	h.waitFor("at the top left", func() bool { return strings.HasPrefix(h.row(1), "╭") })
	if h.row(0) != strings.Repeat(".", 40) {
		t.Fatalf("the margin row %q, want the page", h.row(0))
	}
	if !strings.Contains(h.row(2), "a message long") || !strings.Contains(h.row(3), "enough to wrap") {
		t.Fatalf("the newest, wrapped, first: %q / %q", h.row(2), h.row(3))
	}
	clock.add(9 * time.Second)
	tick(h, toasts)
	if !strings.Contains(h.screen(), "old") {
		t.Fatal("gone before a 10s linger")
	}
	clock.add(time.Second)
	tick(h, toasts)
	h.waitFor("gone at 10s", func() bool { return !strings.Contains(h.screen(), "old") })
	h.onLoop(func() {
		toasts.SetCorner(widget.BottomRight)
		toasts.SetLinger(0) // at least a second
		toasts.Post(widget.Toast{Text: "moved"})
	})
	h.waitFor("moved to the bottom right", func() bool { return strings.Contains(h.row(9), "moved") })
	clock.add(time.Second)
	tick(h, toasts)
	h.waitFor("a second's linger", func() bool { return !strings.Contains(h.screen(), "moved") })
}

// TestAToastsAge: now, seconds, minutes, hours.
func TestAToastsAge(t *testing.T) {
	h, toasts, clock := toastScreen(t)
	h.onLoop(func() { toasts.Post(widget.Toast{ID: "x", Text: "x", Ongoing: true}) })
	for _, c := range []struct {
		after time.Duration
		says  string
	}{{4 * time.Second, " now "}, {time.Second, " 5s ago "}, {2 * time.Hour, " 2h ago "}} {
		clock.add(c.after)
		tick(h, toasts)
		h.waitFor(c.says, func() bool { return strings.Contains(h.row(8), c.says) })
	}
}

// screen is every row, a line each.
func (h *harness) screen() string {
	var rows []string
	for y := range 12 {
		rows = append(rows, h.row(y))
	}
	return strings.Join(rows, "\n")
}
