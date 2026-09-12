package widget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// Tests for the BufferView concurrent io.Writer handle (bufWriter).

type writerHarness struct {
	t      *testing.T
	app    *tui.App
	tb     *tui.TestBackend
	cancel context.CancelFunc
	resc   chan error
}

func (h *writerHarness) onLoop(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("loop did not run Update within 3s")
	}
}

func (h *writerHarness) waitFor(desc string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", desc)
}

func (h *writerHarness) sync() {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("loop did not respond to Update within 3s")
	}
}

func (h *writerHarness) settle() {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.sync()
		h.sync()
		f := h.tb.Flushes()
		time.Sleep(3 * time.Millisecond)
		h.sync()
		if h.tb.Flushes() == f {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("frame activity did not settle within 3s")
		}
	}
}

func (h *writerHarness) grid() string {
	return h.tb.String()
}

type writerShell struct {
	child tui.Component
	ctx   *tui.Context
}

func (s *writerShell) Init(ctx *tui.Context) {
	s.ctx = ctx
	ctx.Mount(s.child)
}

func (s *writerShell) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	h := c.MaxH
	if s.child != nil {
		sz := s.ctx.LayoutChild(s.child, tui.Tight(tui.Size{W: w, H: h}))
		s.ctx.PlaceChild(s.child, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

func (s *writerShell) Render(tui.Surface) {}

func (s *writerShell) HandleEvent(tui.Event) bool { return false }

func (s *writerShell) unmountChild() {
	if s.child != nil {
		s.ctx.Unmount(s.child)
		s.child = nil
	}
}

func startWriterApp(t *testing.T, child tui.Component, w, h int) (*writerHarness, *writerShell) {
	t.Helper()
	sh := &writerShell{child: child}
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(sh, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	h2 := &writerHarness{t: t, app: app, tb: tb, cancel: cancel, resc: make(chan error, 1)}
	go func() { h2.resc <- app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-h2.resc
	})
	return h2, sh
}

func mountedWriterView(t *testing.T, w, h int, opts ...BufferViewOption) (*writerHarness, *BufferView, *writerShell) {
	t.Helper()
	v := NewBufferView(opts...)
	hh, sh := startWriterApp(t, v, w, h)
	return hh, v, sh
}

// TestBufferViewNotAWriter: *BufferView itself must NOT satisfy io.Writer
// — the handle is the only concurrent surface.
func TestBufferViewNotAWriter(t *testing.T) {
	var v any = NewBufferView()
	if _, ok := v.(io.Writer); ok {
		t.Fatalf("*BufferView satisfies io.Writer — the widget value must stay loop-owned")
	}
	if _, ok := v.(interface{ Write([]byte) (int, error) }); ok {
		t.Fatalf("*BufferView has a Write method")
	}
}

// TestBufferViewConcurrentWriters asserts 8 goroutines through ONE
// handle under -race produce ordered, uncorrupted lines.
func TestBufferViewConcurrentWriters(t *testing.T) {
	const goroutines, lines = 8, 50
	h, v, _ := mountedWriterView(t, 20, goroutines*lines+5, WithMaxLines(goroutines*lines+10))
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < lines; i++ {
				if _, err := fmt.Fprintf(w, "g%d-%04d\n", g, i); err != nil {
					t.Errorf("writer %d: %v", g, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	h.waitFor("all lines ingested", func() bool {
		var n int
		h.onLoop(func() { n = v.LineCount() })
		return n >= goroutines*lines
	})
	h.settle()

	next := make([]int, goroutines)
	seen := 0
	for _, row := range strings.Split(h.grid(), "\n") {
		row = strings.TrimRight(row, " ")
		if row == "" {
			continue
		}
		var g, i int
		if n, err := fmt.Sscanf(row, "g%d-%04d", &g, &i); n != 2 || err != nil || g < 0 || g >= goroutines {
			t.Fatalf("corrupted line %q", row)
		}
		if i != next[g] {
			t.Fatalf("goroutine %d out of order: line %d after %d", g, i, next[g])
		}
		next[g]++
		seen++
	}
	if seen != goroutines*lines {
		t.Fatalf("saw %d/%d lines", seen, goroutines*lines)
	}
}

// TestBufferViewWriterClosed asserts writes after unmount return ErrClosed.
func TestBufferViewWriterClosed(t *testing.T) {
	v := NewBufferView()
	h, sh := startWriterApp(t, v, 20, 5)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })
	if _, err := w.Write([]byte("before\n")); err != nil {
		t.Fatalf("write before unmount: %v", err)
	}
	h.waitFor("write landed", func() bool { return strings.Contains(h.grid(), "before") })

	h.onLoop(sh.unmountChild)
	if _, err := w.Write([]byte("after\n")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after unmount = %v, want ErrClosed", err)
	}
}

// TestBufferViewWriterBudgetDefault pins the pending-byte bound consumers
// actually get.
func TestBufferViewWriterBudgetDefault(t *testing.T) {
	const want = 256 << 10
	if got := writerBudget; got != want {
		t.Errorf("writer budget constant = %d, want %d (the documented bound)", got, want)
	}
	v := NewBufferView()
	if got := v.wr.budget; got != want {
		t.Errorf("fresh view's writer budget = %d, want %d", got, want)
	}
}

// TestBufferViewBoundedPending asserts a stalled loop blocks writers
// (bounded pending bytes) rather than buffering unboundedly.
func TestBufferViewBoundedPending(t *testing.T) {
	h, v, _ := mountedWriterView(t, 20, 5)

	const budget = writerChunk
	v.wr.mu.Lock()
	v.wr.budget = budget
	v.wr.mu.Unlock()

	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	// Stall the loop.
	release := make(chan struct{})
	h.app.Update(func() { <-release })

	// Push past the budget: the writer must block.
	const total = 3 * writerChunk
	done := make(chan struct{})
	var werr error
	go func() {
		_, werr = w.Write([]byte(strings.Repeat("x", total)))
		close(done)
	}()
	select {
	case <-done:
		t.Fatalf("%d KiB write completed against a stalled loop with a %d KiB budget (err=%v) — pending bytes are unbounded",
			total>>10, budget>>10, werr)
	case <-time.After(100 * time.Millisecond):
		// blocked, as required
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("writer did not resume after the loop drained")
	}
	if werr != nil {
		t.Fatalf("write failed after the loop drained: %v", werr)
	}
}
