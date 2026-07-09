package widget_test

// BufferView + Writer handle contract (ADR-0007 §2.4 rev 1, §5.7).

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestBufferViewNotAWriter: *BufferView itself must NOT satisfy io.Writer
// (rev 1, Lector Q2) — the handle is the only concurrent surface. (A
// negative interface-satisfaction assertion cannot be a compile error, so
// the type assertion runs here; if someone adds Write methods to the
// widget, this fails.)
func TestBufferViewNotAWriter(t *testing.T) {
	var v any = widget.NewBufferView()
	if _, ok := v.(io.Writer); ok {
		t.Fatalf("*BufferView satisfies io.Writer — the widget value must stay loop-owned (ADR-0007 rev 1)")
	}
	if _, ok := v.(interface{ Write([]byte) (int, error) }); ok {
		t.Fatalf("*BufferView has a Write method")
	}
}

func mountedView(t *testing.T, w, h int, opts ...widget.BufferViewOption) (*harness, *widget.BufferView, *shell) {
	t.Helper()
	v := widget.NewBufferView(opts...)
	sh := newShell(v)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, v, sh
}

// write pushes s through the handle from the test goroutine and fails on
// error.
func write(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("Writer.Write: %v", err)
	}
}

// TestBufferViewANSIStyledCells asserts §5.7: git-style SGR content through
// Writer() produces styled cells matching the escapes.
func TestBufferViewANSIStyledCells(t *testing.T) {
	h, v, _ := mountedView(t, 30, 5)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	write(t, w, "\x1b[31mred\x1b[0m plain \x1b[1;38;5;42mbold256\x1b[0m\n")
	h.waitFor("styled line", func() bool { return strings.Contains(h.grid(), "bold256") })

	// "red" in ANSI red (index 1).
	a := cellAttrs(h, 0, 0)
	if a.FG.Kind != tui.CellColorANSI || a.FG.Index != 1 {
		t.Fatalf("cell 'r' FG = %+v, want ANSI 1", a.FG)
	}
	// " plain " back at the terminal default.
	a = cellAttrs(h, 4, 0)
	if a.FG.Kind != tui.CellColorDefault || a.Mask != 0 {
		t.Fatalf("plain cell FG = %+v mask=%v, want default", a.FG, a.Mask)
	}
	// "bold256": bold + ANSI-256 index 42.
	a = cellAttrs(h, 10, 0)
	if a.FG.Kind != tui.CellColorANSI256 || a.FG.Index != 42 || a.Mask&tui.AttrBold == 0 {
		t.Fatalf("bold256 cell = %+v, want bold ANSI256(42)", a)
	}
}

// TestBufferViewPassthroughOff asserts §5.7: WithANSIPassthrough(false)
// strips escapes.
func TestBufferViewPassthroughOff(t *testing.T) {
	h, v, _ := mountedView(t, 30, 5, widget.WithANSIPassthrough(false))
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })
	write(t, w, "\x1b[31mred\x1b[0m plain\x1b]0;title\x07!\n")
	h.waitFor("stripped line", func() bool { return strings.Contains(h.grid(), "red plain!") })
	if a := cellAttrs(h, 0, 0); a.FG.Kind != tui.CellColorDefault {
		t.Fatalf("passthrough-off styled a cell: %+v", a.FG)
	}
}

// TestBufferViewFollowTail asserts §5.7: follow keeps the tail pinned;
// scroll-up disengages (event); End re-engages (event).
func TestBufferViewFollowTail(t *testing.T) {
	h, v, sh := mountedView(t, 20, 3)
	follows := record[widget.FollowTailChangedEvent](h)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	for i := 1; i <= 6; i++ {
		write(t, w, fmt.Sprintf("line-%d\n", i))
	}
	h.waitFor("tail pinned", func() bool { return strings.Contains(h.grid(), "line-6") })
	h.wantNotContains("line-1")

	h.inject(key(tui.KeyUp))
	h.barrier(sh)
	if ev, ok := follows.last(); !ok || ev.Following {
		t.Fatalf("scroll-up did not disengage follow: %+v", ev)
	}
	// New writes no longer move the view.
	before := h.grid()
	write(t, w, "line-7\n")
	h.settle()
	if h.grid() != before {
		t.Fatalf("view moved while not following")
	}

	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	if ev, ok := follows.last(); !ok || !ev.Following {
		t.Fatalf("End did not re-engage follow: %+v", ev)
	}
	h.wantContains("line-7")
}

// TestBufferViewRing asserts §5.7: the MaxLines ring drops the oldest
// lines.
func TestBufferViewRing(t *testing.T) {
	h, v, _ := mountedView(t, 20, 10, widget.WithMaxLines(3))
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })
	write(t, w, "a\nb\nc\nd\ne\n")
	h.waitFor("ring settled", func() bool { return strings.Contains(h.grid(), "e") })
	var n int
	h.onLoop(func() { n = v.LineCount() })
	if n != 3 {
		t.Fatalf("LineCount = %d, want MaxLines 3", n)
	}
	h.wantNotContains("a")
	h.wantNotContains("b")
	h.wantContains("d")
}

// TestBufferViewConcurrentWriters asserts §5.7: 8 goroutines through ONE
// handle under -race produce ordered, uncorrupted lines.
func TestBufferViewConcurrentWriters(t *testing.T) {
	const goroutines, lines = 8, 50
	// A grid tall enough to show every line at once: ordering is then
	// asserted over one snapshot, no paging.
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

	// Every line intact (uncorrupted) and each goroutine's sequence in
	// write order.
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

// TestBufferViewWriterClosed asserts §5.7: writes after unmount return
// ErrClosed.
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

// TestBufferViewBoundedPending asserts §5.7: a stalled loop blocks writers
// (bounded pending bytes) rather than buffering unboundedly.
func TestBufferViewBoundedPending(t *testing.T) {
	h, v, _ := mountedView(t, 20, 5)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	// Stall the loop.
	release := make(chan struct{})
	h.app.Update(func() { <-release })

	// Push well past the 256 KiB budget: the writer must block.
	done := make(chan struct{})
	go func() {
		big := strings.Repeat("x", 64<<10)
		for i := 0; i < 8; i++ { // 512 KiB total
			if _, err := w.Write([]byte(big)); err != nil {
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatalf("512 KiB write completed against a stalled loop — pending bytes are unbounded")
	case <-time.After(100 * time.Millisecond):
		// blocked, as required
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("writer did not resume after the loop drained")
	}
}

// TestBufferViewPartialLineAndCR: a partial trailing line renders and is
// extended by the next write; a bare \r overwrites the current line.
func TestBufferViewPartialLineAndCR(t *testing.T) {
	h, v, _ := mountedView(t, 20, 4)
	var w io.Writer
	h.onLoop(func() { w = v.Writer() })

	write(t, w, "prog: 10%")
	h.waitFor("partial line", func() bool { return strings.Contains(h.grid(), "prog: 10%") })
	write(t, w, "\rprog: 90%")
	h.waitFor("overwritten", func() bool { return strings.Contains(h.grid(), "prog: 90%") })
	h.wantNotContains("10%")
	write(t, w, " done\n")
	h.waitFor("extended", func() bool { return strings.Contains(h.grid(), "prog: 90% done") })
}
