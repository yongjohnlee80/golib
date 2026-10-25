package tui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// A frame laid out for a size the grid no longer has is dropped, as a terminal clips a frame drawn
// for its old size: InjectResize replaced the grid while the frame was on its way. A frame told the
// grid's size, or told nothing, is applied, and still panics on an update outside the grid. The
// TestBackend is told sizes beside Flush (FrameSizer), never instead of it (no SizedFlusher).
func TestAFrameForAReplacedGridIsDropped(t *testing.T) {
	tb := NewTestBackend(100, 30)
	tb.InjectResize(55, 30)
	x := Cell{Content: "x", Width: 1}
	tb.FrameSize(Size{W: 100, H: 30})
	if err := tb.Flush([]CellUpdate{{X: 94, Y: 0, Cell: x}, {X: 3, Y: 0, Cell: x}}); err != nil {
		t.Fatalf("a frame for the old 100x30 grid: %v; want it dropped", err)
	}
	if strings.Contains(tb.String(), "x") {
		t.Fatalf("a frame for the old grid was applied:\n%s", tb.String())
	}
	tb.FrameSize(Size{W: 55, H: 30})
	if err := tb.Flush([]CellUpdate{{X: 4, Y: 0, Cell: x}}); err != nil {
		t.Fatal(err)
	}
	if err := tb.Flush([]CellUpdate{{X: 6, Y: 0, Cell: x}}); err != nil { // told nothing: applied
		t.Fatal(err)
	}
	if row := strings.Split(tb.String(), "\n")[0]; !strings.HasPrefix(row, "    x x") {
		t.Fatalf("the frames for the grid's 55x30: first row %q; want x at columns 4 and 6", row)
	}
	tb.FrameSize(Size{W: 55, H: 30})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("an update outside the grid, in a frame for the grid's size, did not panic")
			}
		}()
		_ = tb.Flush([]CellUpdate{{X: 94, Y: 0, Cell: x}})
	}()
	if _, ok := Backend(tb).(SizedFlusher); ok {
		t.Error("TestBackend is a SizedFlusher: a wrapper's own Flush would be passed over")
	}
}

// toldFlush is a TestBackend wrapped as a test does it, its Flush overridden to see each frame,
// logging what the App tells it.
type toldFlush struct {
	*TestBackend
	mu  sync.Mutex
	log []string
}

func (b *toldFlush) FrameSize(s Size) {
	b.mu.Lock()
	b.log = append(b.log, fmt.Sprintf("size %dx%d", s.W, s.H))
	b.mu.Unlock()
	b.TestBackend.FrameSize(s)
}

func (b *toldFlush) Flush(diff []CellUpdate) error {
	b.mu.Lock()
	b.log = append(b.log, "flush")
	b.mu.Unlock()
	return b.TestBackend.Flush(diff)
}

// A wrapper that embeds a TestBackend and overrides Flush still sees every frame through its own
// Flush, each told its size just before it, and the frames reach the grid.
func TestAWrappedTestBackendsFlushSeesEveryFrameToldItsSize(t *testing.T) {
	tb := &toldFlush{TestBackend: NewTestBackend(20, 5)}
	h := runApp(t, NewApp(&probe{name: "root", fill: "A"}, WithBackend(tb), WithMinFrameInterval(0)), tb.TestBackend)
	waitFor(t, "a frame", func() bool { return tb.Flushes() > 0 })
	h.sync()
	tb.mu.Lock()
	log := append([]string(nil), tb.log...)
	tb.mu.Unlock()
	if len(log) < 2 || len(log)%2 != 0 {
		t.Fatalf("the wrapper was told %v; want each frame's size, then its Flush", log)
	}
	for i := 0; i < len(log); i += 2 {
		if log[i] != "size 20x5" || log[i+1] != "flush" {
			t.Fatalf("the wrapper was told %v; want each frame's size, then its Flush", log)
		}
	}
	if !strings.Contains(tb.String(), "AAAA") {
		t.Fatalf("the frames did not reach the grid:\n%s", tb.String())
	}
}

// bothSizes is a backend with both size capabilities, logging which the App uses.
type bothSizes struct {
	*TestBackend
	mu  sync.Mutex
	log []string
}

func (b *bothSizes) FlushSized(s Size, diff []CellUpdate) error {
	b.mu.Lock()
	b.log = append(b.log, fmt.Sprintf("flushSized %dx%d", s.W, s.H))
	b.mu.Unlock()
	return b.TestBackend.Flush(diff)
}

func (b *bothSizes) FrameSize(s Size) {
	b.mu.Lock()
	b.log = append(b.log, "frameSize")
	b.mu.Unlock()
}

func (b *bothSizes) Flush(diff []CellUpdate) error {
	b.mu.Lock()
	b.log = append(b.log, "flush")
	b.mu.Unlock()
	return b.TestBackend.Flush(diff)
}

// SizedFlusher takes precedence: a backend with both size capabilities gets FlushSized alone, never
// FrameSize then Flush.
func TestASizedFlusherThatIsAlsoAFrameSizerGetsFlushSizedAlone(t *testing.T) {
	tb := &bothSizes{TestBackend: NewTestBackend(20, 5)}
	h := runApp(t, NewApp(&probe{name: "root", fill: "A"}, WithBackend(tb), WithMinFrameInterval(0)), tb.TestBackend)
	waitFor(t, "a frame", func() bool { return tb.Flushes() > 0 })
	h.sync()
	tb.mu.Lock()
	log := append([]string(nil), tb.log...)
	tb.mu.Unlock()
	if len(log) == 0 {
		t.Fatal("the backend was told nothing")
	}
	for _, l := range log {
		if l != "flushSized 20x5" {
			t.Fatalf("the backend was told %v; want FlushSized alone, every frame", log)
		}
	}
}
