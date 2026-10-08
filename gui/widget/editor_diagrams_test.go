package widget

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// fakeDiagrams answers Pending until finish marks a source done, and keeps every request's
// context and ready.
type fakeDiagrams struct {
	mu      sync.Mutex
	calls   int
	ctxs    []context.Context
	readies []func()
	done    map[string]bool
}

func (f *fakeDiagrams) Diagram(ctx context.Context, req DiagramRequest, ready func()) DiagramAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.ctxs = append(f.ctxs, ctx)
	f.readies = append(f.readies, ready)
	if f.done[req.Src] {
		return DiagramAnswer{State: Ready, Pic: gui.ViewFunc(func(gui.Canvas) {}), Size: gui.Size{W: 50, H: 30}}
	}
	return DiagramAnswer{State: Pending}
}

func (f *fakeDiagrams) state() (calls int, ctx context.Context, ready func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.ctxs[len(f.ctxs)-1], f.readies[len(f.readies)-1]
}

// finish marks src done and calls the last request's ready from another goroutine.
func (f *fakeDiagrams) finish(src string) {
	f.mu.Lock()
	f.done[src] = true
	r := f.readies[len(f.readies)-1]
	f.mu.Unlock()
	go r()
}

const diagramDoc = "top\n```mermaid\nA-->B\n```\nend"

func startDiagrams(t *testing.T) (*edHarness, *fakeDiagrams) {
	t.Helper()
	f := &fakeDiagrams{done: map[string]bool{}}
	h := startEditor(t, 50, 14, WithRenderer(NewMarkdownRenderer(WithDiagrams(f))), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(diagramDoc)))
	h.paint()
	return h, f
}

func (h *edHarness) picture(line int) gui.View {
	var v gui.View
	h.onLoop(func() { v = h.e.layout.lay(h.e.layout.blockOf(line), nil).bl.Picture })
	return v
}

func TestDiagramPendingThenReady(t *testing.T) {
	h, f := startDiagrams(t)
	if calls, _, _ := f.state(); calls != 1 {
		t.Fatalf("asked %d times, want once", calls)
	}
	if h.picture(1) != nil {
		t.Fatal("a pending diagram drew a picture")
	}
	f.finish("A-->B")
	h.until("the ready picture", func() bool { h.paint(); return h.picture(1) != nil })
	if calls, _, _ := f.state(); calls != 2 {
		t.Errorf("asked %d times, want twice: once, then again after ready", calls)
	}
}

func TestDiagramStaleReadyAfterAnEditIsDropped(t *testing.T) {
	h, f := startDiagrams(t)
	_, ctx0, ready0 := f.state()
	// edit the diagram's source: line 2, insert C, back to Normal, and leave the block
	h.keys(key('j'), key('j'), key('i'), key('C'), code(tui.KeyEscape), key('g'), key('g'))
	h.paint()
	var src string
	h.onLoop(func() { src = h.e.core.LineAt(2) })
	if src != "CA-->B" {
		t.Fatalf("the edit did not happen: line 2 is %q", src)
	}
	if ctx0.Err() == nil {
		t.Fatal("the edited block's old request was not cancelled")
	}
	calls, _, _ := f.state()
	if calls != 2 {
		t.Fatalf("asked %d times, want a new request for the edited source", calls)
	}
	ready0() // the withdrawn request's ready, late
	time.Sleep(50 * time.Millisecond)
	h.paint()
	if c, _, _ := f.state(); c != calls {
		t.Errorf("a stale ready asked again: %d calls, want %d", c, calls)
	}
	if h.picture(1) != nil {
		t.Error("a stale ready drew a picture")
	}
}

func TestDiagramUnmountCancels(t *testing.T) {
	h, f := startDiagrams(t)
	_, ctx0, ready0 := f.state()
	h.onLoop(func() { h.sh.ctx.Unmount(h.e) })
	if ctx0.Err() == nil {
		t.Fatal("unmounting the editor did not cancel its pending request")
	}
	ready0() // after unmount: nothing to deliver to, nothing panics
	time.Sleep(20 * time.Millisecond)
}
