package widget_test

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// Tests for the BufferView concurrent io.Writer handle (bufWriter).

// TestBufferViewNotAWriter: *BufferView itself must NOT satisfy io.Writer
// — the handle is the only concurrent surface.
func TestBufferViewNotAWriter(t *testing.T) {
	var v any = widget.NewBufferView()
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
	h, v, _ := mountedView(t, 20, goroutines*lines+5, widget.WithMaxLines(goroutines*lines+10))
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
	v := widget.NewBufferView()
	sh := newShell(v)
	h := startApp(t, sh, 20, 5)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })
	write(t, w, "before\n")
	h.waitFor("write landed", func() bool { return strings.Contains(h.grid(), "before") })

	h.onLoop(sh.unmountChild)
	if _, err := w.Write([]byte("after\n")); !errors.Is(err, widget.ErrClosed) {
		t.Fatalf("write after unmount = %v, want ErrClosed", err)
	}
}

// TestBufferViewWriterBudgetDefault pins the pending-byte bound consumers
// actually get.
func TestBufferViewWriterBudgetDefault(t *testing.T) {
	const want = 256 << 10
	if got := widget.WriterBudgetDefaultForTest; got != want {
		t.Errorf("writer budget constant = %d, want %d (the documented bound)", got, want)
	}
	if got := widget.WriterBudgetOfForTest(widget.NewBufferView()); got != want {
		t.Errorf("fresh view's writer budget = %d, want %d", got, want)
	}
}

// TestBufferViewBoundedPending asserts a stalled loop blocks writers
// (bounded pending bytes) rather than buffering unboundedly.
func TestBufferViewBoundedPending(t *testing.T) {
	h, v, _ := mountedView(t, 20, 5)

	const budget = widget.WriterChunkForTest
	widget.SetWriterBudgetForTest(v, budget)

	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	// Stall the loop.
	release := make(chan struct{})
	h.app.Update(func() { <-release })

	// Push past the budget: the writer must block.
	const total = 3 * widget.WriterChunkForTest
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

	h.sync()
}
