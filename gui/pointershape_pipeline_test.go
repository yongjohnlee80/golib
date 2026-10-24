package gui_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/gui"
	guiwidget "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// pipeHost runs a real App on a TestBackend whose native views, pointer shapes and flushes go to
// a real gui Backend, headless: the App collects its regions and flushes as it does in a window,
// and the Backend paints the natives and then asks their regions, as it does in a window. Each
// flushed frame's cursor areas are kept, in order.
type pipeHost struct {
	*tui.TestBackend
	h      *gui.HeadlessFrames
	app    *tui.App
	mu     sync.Mutex
	frames [][]gui.CursorCells
}

func (p *pipeHost) NativeFor(c tui.Component) (any, tui.NativeScope, bool) { return p.h.B.NativeFor(c) }
func (p *pipeHost) PlaceNatives(ps []tui.NativePlacement)                  { p.h.B.PlaceNatives(ps) }
func (p *pipeHost) PlacePointerShapes(ps []tui.PointerShapePlacement)      { p.h.B.PlacePointerShapes(ps) }

func (p *pipeHost) Flush(diff []tui.CellUpdate) error {
	if err := p.TestBackend.Flush(diff); err != nil {
		return err
	}
	c := p.h.Flush(diff)
	p.mu.Lock()
	p.frames = append(p.frames, c)
	p.mu.Unlock()
	return nil
}

func startPipe(t *testing.T, root tui.Component, w, h int) *pipeHost {
	t.Helper()
	p := &pipeHost{TestBackend: tui.NewTestBackend(w, h, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true})),
		h: gui.NewHeadlessFrames(w, h)}
	p.app = tui.NewApp(root, tui.WithBackend(p), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = p.app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return p
}

// frame is the i'th flushed frame's cursor areas (0: the first), once it has flushed.
func (p *pipeHost) frame(t *testing.T, i int) []gui.CursorCells {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		if len(p.frames) > i {
			f := p.frames[i]
			p.mu.Unlock()
			return f
		}
		p.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("frame %d never flushed", i)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// update runs fn on the App's loop and reports how many frames had flushed before it ran: the
// next frame is the first fn's change shows in.
func (p *pipeHost) update(t *testing.T, fn func()) int {
	t.Helper()
	n := make(chan int, 1)
	p.app.Update(func() {
		p.mu.Lock()
		n <- len(p.frames)
		p.mu.Unlock()
		fn()
	})
	select {
	case i := <-n:
		return i
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not run the update")
		return 0
	}
}

// from is the leftmost column a frame's areas of cursor c start at, on any row; -1: none.
func from(f []gui.CursorCells, c pointer.Cursor) int {
	x := -1
	for _, a := range f {
		if a.Cursor == c && (x < 0 || a.X < x) {
			x = a.X
		}
	}
	return x
}

// The native editor body's I-beam is the frame's own, through the App and the Backend as a window
// runs them, with no paint made by hand: on the very first frame, and on the frame the line
// numbers turn on, past the gutter that frame painted, never the gutter before.
func TestTheNativeEditorsIBeamIsEachFramesOwnThroughThePipeline(t *testing.T) {
	e := guiwidget.NewEditor(guiwidget.WithCore(tuiwidget.CoreInitialText("one\ntwo")))
	p := startPipe(t, e, 60, 8)
	plain := from(p.frame(t, 0), pointer.CursorText)
	if plain < 0 {
		t.Fatalf("the first frame: %+v, want the native body's I-beam", p.frame(t, 0))
	}
	n := p.update(t, func() { e.SetLineNumbers(true) })
	f := p.frame(t, n)
	if got := from(f, pointer.CursorText); got <= plain {
		t.Fatalf("the frame the numbers turn on: %+v, want the I-beam past the new gutter, beyond column %d", f, plain)
	}
}

// A bound HTML page's links are a hand on the first frame that draws them, through the App and the
// Backend as a window runs them, with no paint made by hand.
func TestABoundHTMLPagesHandIsOnItsFirstFrameThroughThePipeline(t *testing.T) {
	v := tuiwidget.NewHTMLView()
	guiwidget.BindHTML(v)
	v.SetHTML([]byte(`<p>see <a href="b.md">the link</a> here</p>`))
	p := startPipe(t, v, 40, 4)
	if f := p.frame(t, 0); from(f, pointer.CursorPointer) <= 0 {
		t.Fatalf("the first frame: %+v, want a hand over the link, past \"see \"", f)
	}
}
