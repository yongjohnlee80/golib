package tui

import (
	"sync"
	"testing"
)

// sizedFrame is one FlushSized call: the size it was handed, and its diff, copied.
type sizedFrame struct {
	size Size
	diff []CellUpdate
}

// sizedBackend is a TestBackend that is a SizedFlusher: it records every FlushSized, and counts
// Flush, which the App must never call on it.
type sizedBackend struct {
	*TestBackend

	mu      sync.Mutex
	frames  []sizedFrame
	flushes int
}

func (b *sizedBackend) FlushSized(size Size, diff []CellUpdate) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames = append(b.frames, sizedFrame{size: size, diff: append([]CellUpdate(nil), diff...)})
	return nil
}

func (b *sizedBackend) Flush(diff []CellUpdate) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushes++
	return nil
}

func (b *sizedBackend) recorded() ([]sizedFrame, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]sizedFrame(nil), b.frames...), b.flushes
}

func startSized(t *testing.T, root Component, w, h int, opts ...AppOption) (*harness, *sizedBackend) {
	t.Helper()
	sb := &sizedBackend{TestBackend: NewTestBackend(w, h)}
	all := append([]AppOption{WithBackend(sb), WithMinFrameInterval(0)}, opts...)
	return runApp(t, NewApp(root, all...), sb.TestBackend), sb
}

// covered is how many cells a diff writes: a wide head covers its continuation too.
func covered(diff []CellUpdate) int {
	n := 0
	for _, u := range diff {
		n += max(int(u.Cell.Width), 1)
	}
	return n
}

// checkSized fails on any frame whose diff writes outside the size it came with, and on any frame
// at a new size (the first, or after a change) that does not cover every cell of it.
func checkSized(t *testing.T, frames []sizedFrame) {
	t.Helper()
	var last Size
	for i, f := range frames {
		for _, u := range f.diff {
			if u.X < 0 || u.Y < 0 || u.X+max(int(u.Cell.Width), 1) > f.size.W || u.Y >= f.size.H {
				t.Fatalf("frame %d at %v writes (%d, %d), outside it", i, f.size, u.X, u.Y)
			}
		}
		if f.size != last {
			if got, want := covered(f.diff), f.size.W*f.size.H; got != want {
				t.Fatalf("frame %d is the first at %v, and covers %d of its %d cells", i, f.size, got, want)
			}
			last = f.size
		}
	}
}

// The App hands a SizedFlusher its buffer's size with every diff, never calls its Flush, and the
// first diff at each new size, wide cells included, is the whole screen.
func TestSizedFlusherGetsTheBufferSizeWithEveryDiff(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 80, H: 24}, fill: "表"}
	h, sb := startSized(t, root, 21, 5)
	waitFor(t, "the first frame", func() bool { f, _ := sb.recorded(); return len(f) > 0 })
	h.tb.InjectResize(8, 2)
	waitFor(t, "a frame at 8×2", func() bool {
		f, _ := sb.recorded()
		return len(f) > 0 && f[len(f)-1].size == Size{W: 8, H: 2}
	})
	h.tb.InjectResize(30, 6)
	waitFor(t, "a frame at 30×6", func() bool {
		f, _ := sb.recorded()
		return len(f) > 0 && f[len(f)-1].size == Size{W: 30, H: 6}
	})
	frames, flushes := sb.recorded()
	if frames[0].size != (Size{W: 21, H: 5}) {
		t.Fatalf("the first frame came with %v, not the starting 21×5", frames[0].size)
	}
	checkSized(t, frames)
	if flushes != 0 {
		t.Fatalf("Flush was called %d times on a SizedFlusher", flushes)
	}
}

// The too-small screen is flushed the same way: its size comes with its diff.
func TestSizedFlusherGetsTheTooSmallScreen(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 40, H: 10}, fill: "x"}
	h, sb := startSized(t, root, 50, 12, WithMinimumSize(Size{W: 40, H: 10}))
	waitFor(t, "the first frame", func() bool { f, _ := sb.recorded(); return len(f) > 0 })
	h.tb.InjectResize(30, 8)
	waitFor(t, "the too-small screen at 30×8", func() bool {
		f, _ := sb.recorded()
		return len(f) > 0 && f[len(f)-1].size == Size{W: 30, H: 8}
	})
	h.onLoop(func() {
		if !h.app.tooSmall() {
			t.Error("30×8 is not the too-small screen under a 40×10 minimum")
		}
	})
	frames, flushes := sb.recorded()
	checkSized(t, frames)
	if flushes != 0 {
		t.Fatalf("Flush was called %d times on a SizedFlusher", flushes)
	}
}

// Under a burst of resizes, shrinking and growing, each diff still indexes the size it came with:
// a frame drawn before the App took a resize comes at the size it was drawn at.
func TestSizedFlusherDiffsMatchTheirSizeUnderResizeBursts(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 80, H: 24}, fill: "表"}
	h, sb := startSized(t, root, 80, 24)
	waitFor(t, "the first frame", func() bool { f, _ := sb.recorded(); return len(f) > 0 })
	sizes := []Size{{20, 1}, {80, 24}, {1, 1}, {33, 7}, {0, 0}, {79, 3}, {20, 1}}
	for i := 0; i < 20; i++ {
		s := sizes[i%len(sizes)]
		h.tb.InjectResize(s.W, s.H)
	}
	h.tb.InjectResize(41, 9)
	waitFor(t, "the final 41×9 frame", func() bool {
		f, _ := sb.recorded()
		return len(f) > 0 && f[len(f)-1].size == Size{W: 41, H: 9}
	})
	frames, _ := sb.recorded()
	checkSized(t, frames)
}

// A backend without the capability keeps Flush: the same diffs as before.
func TestBackendWithoutSizedFlusherKeepsFlush(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}, fill: "y"}
	h := startApp(t, root, 4, 2)
	waitFor(t, "a Flush", func() bool { return h.tb.Flushes() > 0 })
	if !screenHas(h.tb, "yyyy") {
		t.Fatalf("Flush did not draw the frame:\n%s", h.tb.String())
	}
	if _, ok := Backend(h.tb).(SizedFlusher); ok {
		t.Fatal("TestBackend became a SizedFlusher: every other backend must keep Flush")
	}
}

// flushCounter is a TestBackend whose Flush records each diff it is handed before the
// TestBackend applies it: a backend without SizedFlusher.
type flushCounter struct {
	*TestBackend

	mu    sync.Mutex
	diffs [][]CellUpdate
}

func (b *flushCounter) Flush(diff []CellUpdate) error {
	b.mu.Lock()
	b.diffs = append(b.diffs, append([]CellUpdate(nil), diff...))
	b.mu.Unlock()
	return b.TestBackend.Flush(diff)
}

// Without the capability, every frame the App emits, the too-small screen's included, is exactly
// one Flush of that frame's diff.
func TestBackendWithoutSizedFlusherGetsOneFlushPerFrame(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 40, H: 10}, fill: "z"}
	fc := &flushCounter{TestBackend: NewTestBackend(50, 12)}
	h := runApp(t, NewApp(root, WithBackend(fc), WithMinFrameInterval(0), WithMinimumSize(Size{W: 40, H: 10})), fc.TestBackend)
	waitFor(t, "the first frame", func() bool { return fc.Flushes() > 0 })
	h.tb.InjectResize(30, 8) // the too-small screen
	waitFor(t, "the notice", func() bool { return screenHas(h.tb, "Screen too small") })
	h.tb.InjectResize(45, 11)
	waitFor(t, "the application again", func() bool { return screenHas(h.tb, "zzzz") })
	var frames uint64
	h.onLoop(func() { frames = uint64(h.app.frames) })
	fc.mu.Lock()
	n := len(fc.diffs)
	last := fc.diffs[n-1]
	fc.mu.Unlock()
	if uint64(n) != frames || uint64(fc.Flushes()) != frames {
		t.Fatalf("%d frames emitted, %d Flush calls recorded, %d applied: want one each", frames, n, fc.Flushes())
	}
	if got, want := covered(last), 45*11; got != want {
		t.Fatalf("the frame after the resize covers %d of %d cells", got, want)
	}
}

// A resize there and back inside one loop turn changes no geometry: the buffer is kept, and the next
// frame's diff is against it (here nothing changed, so it is empty), not a whole-screen redraw.
// A real change of size, by contrast, is a whole frame (TestSizedFlusherGetsTheBufferSizeWithEveryDiff).
func TestSizedFlusherSameSizeRoundTripKeepsTheBuffer(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 20, H: 5}, fill: "q"}
	h, sb := startSized(t, root, 20, 5)
	waitFor(t, "the first frame", func() bool { f, _ := sb.recorded(); return len(f) > 0 })
	before, _ := sb.recorded()
	h.onLoop(func() {
		h.app.dispatch(ResizeEvent{W: 10, H: 3})
		h.app.dispatch(ResizeEvent{W: 20, H: 5}) // back, before any frame
	})
	waitFor(t, "a frame after the round trip", func() bool { f, _ := sb.recorded(); return len(f) > len(before) })
	frames, _ := sb.recorded()
	for _, f := range frames[len(before):] {
		if f.size != (Size{W: 20, H: 5}) {
			t.Fatalf("a frame at %v after a round trip that never left 20×5", f.size)
		}
		if covered(f.diff) != 0 {
			t.Fatalf("the round trip redrew %d cells: the buffer was reset", covered(f.diff))
		}
	}
}
