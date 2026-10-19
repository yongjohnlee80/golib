package widget

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// pxBackend is a test backend whose cell is 8×16 logical pixels, as a window's.
type pxBackend struct{ *tui.TestBackend }

func (pxBackend) CellPixels() tui.CellPixels { return tui.CellPixels{W: 8, H: 16} }

// sizedProc is a program that prints nothing and records the sizes it is given.
type sizedProc struct {
	r    *io.PipeReader
	w    *io.PipeWriter
	mu   sync.Mutex
	size [2]int
}

func newSizedProc() *sizedProc                   { r, w := io.Pipe(); return &sizedProc{r: r, w: w} }
func (p *sizedProc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *sizedProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *sizedProc) Resize(rows, cols int) error {
	p.mu.Lock()
	p.size = [2]int{rows, cols}
	p.mu.Unlock()
	return nil
}
func (p *sizedProc) Close() error       { return p.w.Close() }
func (p *sizedProc) Wait() (int, error) { return 0, nil }
func (p *sizedProc) last() [2]int       { p.mu.Lock(); defer p.mu.Unlock(); return p.size }

// holder shows its child over its whole area, or not: hiding it unmounts the child, showing it
// again mounts it anew, as a panel hidden and shown does.
type holder struct {
	child tui.Component
	ctx   *tui.Context
	shown bool
}

func (h *holder) Init(ctx *tui.Context) {
	h.ctx = ctx
	if h.shown {
		ctx.Mount(h.child)
	}
}
func (h *holder) Layout(c tui.Constraints) tui.Size {
	sz := tui.Size{W: c.MaxW, H: c.MaxH}
	if h.shown {
		h.ctx.LayoutChild(h.child, tui.Tight(sz))
		h.ctx.PlaceChild(h.child, tui.Rect{W: sz.W, H: sz.H})
	}
	return sz
}
func (h *holder) Render(tui.Surface)         {}
func (h *holder) HandleEvent(tui.Event) bool { return false }
func (h *holder) show(on bool) {
	if on == h.shown {
		return
	}
	h.shown = on
	if on {
		h.ctx.Mount(h.child)
	} else {
		h.ctx.Unmount(h.child)
	}
	h.ctx.RequestLayout()
}

func runOn(t *testing.T, app *tui.App, fn func()) {
	t.Helper()
	done := make(chan struct{})
	app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not run the update")
	}
}

// TerminalViews through the real wiring: SetTerminalViews(views.Make) makes each tab's view with
// its terminal; the first layout, before any Paint, sizes each program in the view's cells from the
// window's (8×16 at half scale: 4×8); the terminal reads its view back as a gui.View, which paints
// the frame; a measured cell (on its first Paint) lays the terminal out again in it; hidden and shown
// again, the view works on the new mount; a closed tab's view is retired and paints nothing.
func TestTerminalViewsSizeAndPaintTheirTerminals(t *testing.T) {
	tabs := tuiwidget.NewTerminalTabs()
	h := &holder{child: tabs, shown: true}
	tb := tui.NewTestBackend(40, 10)
	app := tui.NewApp(h, tui.WithBackend(pxBackend{tb}), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	waitFor(t, "a frame", func() bool { return tb.Flushes() > 0 })

	views := NewTerminalViews(0.5)
	procs := []*sizedProc{newSizedProc(), newSizedProc()}
	var ids []tuiwidget.TerminalTabID
	runOn(t, app, func() {
		tabs.SetTerminalViews(views.Make)
		for i, name := range []string{"a", "b"} {
			p := procs[i]
			id, err := tabs.Add(name, tuiwidget.WithStarter(func(pty.Cmd) (tuiwidget.TerminalProcess, error) { return p, nil }))
			if err != nil {
				t.Fatal(err)
			}
			if err := tabs.Start(id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
	})
	// the strip takes a row: the body is 40×9 cells of 8×16, in cells of 4×8 that is 80×18
	waitFor(t, "the active program in the view's cells", func() bool { return procs[0].last() == [2]int{18, 80} })
	var active *terminalView
	runOn(t, app, func() {
		if len(views.views) != 2 || views.views[0].owner == views.views[1].owner {
			t.Fatalf("%d views, owners distinct %v", len(views.views), len(views.views) == 2 && views.views[0].owner != views.views[1].owner)
		}
		for _, v := range views.views {
			if back, ok := v.owner.TerminalView().(gui.CaretView); !ok || back != gui.CaretView(v) {
				t.Errorf("a terminal reads back %T, not its own view", v.owner.TerminalView())
			}
		}
		active = views.views[0]
	})

	paint := func() []gui.PaintCall {
		var calls []gui.PaintCall
		runOn(t, app, func() {
			c := gui.NewRecordingCanvas(gui.Size{W: 320, H: 144}, gui.Size{W: 8, H: 16})
			c.TextPx = 14 // a 7 px terminal: its headless cell is 4×9
			active.Paint(c)
			calls = c.Calls
		})
		return calls
	}
	paint()
	waitFor(t, "laid out again in the measured cell", func() bool { return procs[0].last() == [2]int{16, 80} })
	waitFor(t, "the frame at the new size", func() bool {
		calls := paint()
		return len(calls) == 1 && calls[0].Op == "PaintTerminalCells" && calls[0].Cells == (gui.CellRect{W: 80, H: 16})
	})

	runOn(t, app, func() { h.show(false) })
	runOn(t, app, func() { h.show(true) })
	runOn(t, app, func() { active.owner.Context().RequestLayout() })
	waitFor(t, "the size kept on the new mount", func() bool { return procs[0].last() == [2]int{16, 80} })
	if calls := paint(); len(calls) != 1 {
		t.Fatalf("after a remount the view painted %d calls", len(calls))
	}

	runOn(t, app, func() { _, _ = tabs.Remove(ids[0]) })
	waitFor(t, "the closed tab's view retired", func() bool {
		var retired bool
		runOn(t, app, func() { retired = active.retired })
		return retired
	})
	if calls := paint(); len(calls) != 0 {
		t.Fatalf("a retired view painted %d calls", len(calls))
	}
}
