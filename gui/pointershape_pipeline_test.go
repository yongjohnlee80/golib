package gui_test

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/gui"
	guiwidget "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// pipeHost runs a real App on a TestBackend whose native views, pointer shapes, cell pixels and
// flushes go to a real gui Backend, headless: the App collects its regions and flushes as it does in a window,
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
func (p *pipeHost) CellPixels() tui.CellPixels                             { return p.h.B.CellPixels() }

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
	return startPipeOn(t, root, w, h, tui.Capabilities{NativeViews: true})
}

// startPipeOn is startPipe on a TestBackend reporting caps.
func startPipeOn(t *testing.T, root tui.Component, w, h int, caps tui.Capabilities) *pipeHost {
	t.Helper()
	p := &pipeHost{TestBackend: tui.NewTestBackend(w, h, tui.WithTestCapabilities(caps)),
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

// A scrolled page's hand is where that frame drew its link: the row, the column and the width,
// through the App and the Backend as a window runs them, the page scrolled by the wheel past
// several lines so the link shows below the top. The oracle is the same view painted again
// on a recording canvas at the same scroll and size, after the frame, read only for where the
// link's text (drawn in its own colour) lands.
func TestAScrolledHTMLPagesHandIsWhereItsFrameDrewTheLink(t *testing.T) {
	var page strings.Builder
	for i := range 4 {
		fmt.Fprintf(&page, "<p>line %d</p>", i)
	}
	page.WriteString(`<p>see <a href="b.md" style="color:#ff0000">the link</a> here</p>`)
	for i := range 30 {
		fmt.Fprintf(&page, "<p>after %d</p>", i)
	}
	v := tuiwidget.NewHTMLView()
	guiwidget.BindHTML(v)
	v.SetHTML([]byte(page.String()))
	const cols, rows = 40, 12
	p := startPipe(t, v, cols, rows)
	p.frame(t, 0)
	for range 2 {
		if err := p.Inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 5, Y: 5}); err != nil {
			t.Fatal(err)
		}
	}
	// settled: the scroll and the frames flushed are the same across two turns of the loop, so the
	// last frame is the scroll's own and nothing is waiting to paint
	type turn struct {
		scroll float32
		frames int
	}
	look := func() turn {
		got := make(chan turn, 1)
		p.app.Update(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			got <- turn{v.ScrollY(), len(p.frames)}
		})
		select {
		case tn := <-got:
			return tn
		case <-time.After(5 * time.Second):
			t.Fatal("the loop did not run the update")
			return turn{}
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for last := look(); ; {
		time.Sleep(20 * time.Millisecond)
		now := look()
		if now == last && now.scroll >= 32 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the wheel's scroll never settled past two lines: %+v", now)
		}
		last = now
	}

	drawn := make(chan gui.Rect, 1)
	var scroll float32
	var f []gui.CursorCells
	p.app.Update(func() {
		scroll = v.ScrollY()
		p.mu.Lock()
		f = p.frames[len(p.frames)-1]
		p.mu.Unlock()
		rc := gui.NewRecordingCanvas(gui.Size{W: cols * 8, H: rows * 16}, gui.Size{W: 8, H: 16})
		rc.TextPx = 14 // the frame canvas's: ppem over scale
		v.BoundLayout().(gui.View).Paint(rc)
		r := gui.Rect{W: -1}
		for _, c := range rc.Calls {
			if c.Op == "DrawText" && c.Brush.Color == (color.NRGBA{R: 0xff, A: 0xff}) {
				r = c.Rect
			}
		}
		drawn <- r
	})
	var at gui.Rect
	select {
	case at = <-drawn:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not paint the oracle")
	}
	if at.W < 0 || at.Y < 0 || at.Y+at.H > rows*16 {
		t.Fatalf("scrolled %v: the oracle drew the link at %+v, not on the view: the cell observes nothing", scroll, at)
	}
	// the cells any part of the drawn link falls in: each row it touches, its columns
	y0, y1 := int(math.Floor(float64(at.Y/16))), int(math.Ceil(float64((at.Y+at.H)/16)))
	x0, x1 := int(math.Floor(float64(at.X/8))), int(math.Ceil(float64((at.X+at.W)/8)))
	var hands []gui.CursorCells
	for _, a := range f {
		if a.Cursor == pointer.CursorPointer {
			hands = append(hands, a)
		}
	}
	ok := len(hands) == y1-y0
	for i, h := range hands {
		ok = ok && h.Y == y0+i && h.H == 1 && h.X == x0 && h.W == x1-x0
	}
	if !ok {
		t.Fatalf("scrolled %v: the hands %+v; want rows %d to %d, columns %d to %d, where the frame drew the link (%+v)",
			scroll, hands, y0, y1, x0, x1, at)
	}
}
