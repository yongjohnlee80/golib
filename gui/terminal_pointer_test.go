package gui_test

import (
	"io"
	"testing"

	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/gui"
	guiwidget "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// quietProc is a program that prints nothing and runs until closed.
type quietProc struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func newQuietProc() *quietProc                   { r, w := io.Pipe(); return &quietProc{r: r, w: w} }
func (p *quietProc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *quietProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *quietProc) Resize(int, int) error       { return nil }
func (p *quietProc) Close() error                { return p.w.Close() }
func (p *quietProc) Wait() (int, error)          { return 0, nil }

// shownHolder shows its child over its whole area, or not: hiding it unmounts the child, as a panel
// hidden does.
type shownHolder struct {
	child tui.Component
	ctx   *tui.Context
	on    bool
}

func (h *shownHolder) Init(ctx *tui.Context) {
	h.ctx = ctx
	if h.on {
		ctx.Mount(h.child)
	}
}
func (h *shownHolder) Layout(c tui.Constraints) tui.Size {
	sz := tui.Size{W: c.MaxW, H: c.MaxH}
	if h.on {
		h.ctx.LayoutChild(h.child, tui.Tight(sz))
		h.ctx.PlaceChild(h.child, tui.Rect{W: sz.W, H: sz.H})
	}
	return sz
}
func (h *shownHolder) Render(tui.Surface)         {}
func (h *shownHolder) HandleEvent(tui.Event) bool { return false }
func (h *shownHolder) show(on bool) {
	if on == h.on {
		return
	}
	h.on = on
	if on {
		h.ctx.Mount(h.child)
	} else {
		h.ctx.Unmount(h.child)
	}
	h.ctx.RequestLayout()
}

// terminalTabs is a TerminalTabs with the native style's terminal views and one tab running.
func terminalTabs(t *testing.T, p *pipeHost, tabs *tuiwidget.TerminalTabs) {
	t.Helper()
	views := guiwidget.NewTerminalViews(1)
	p.update(t, func() {
		tabs.SetTerminalViews(views.Make)
		id, err := tabs.Add("a", tuiwidget.WithStarter(func(pty.Cmd) (tuiwidget.TerminalProcess, error) { return newQuietProc(), nil }))
		if err == nil {
			err = tabs.Start(id)
		}
		if err != nil {
			t.Error(err)
		}
	})
}

// texted waits for a frame from i on whose areas hold an I-beam, and returns it.
func texted(t *testing.T, p *pipeHost, i int) []gui.CursorCells {
	t.Helper()
	for end := i + 200; i < end; i++ {
		if f := p.frame(t, i); from(f, pointer.CursorText) >= 0 {
			return f
		}
	}
	t.Fatal("no frame showed an I-beam")
	return nil
}

// A terminal view is an I-beam over its body, through the App and the Backend as a window runs
// them; its tab strip's row keeps the arrow. Hidden (its node unmounted), it leaves no I-beam behind.
func TestATerminalViewIsAnIBeamOverItsBodyAndNoneOnceHidden(t *testing.T) {
	tabs := tuiwidget.NewTerminalTabs()
	h := &shownHolder{child: tabs, on: true}
	p := startPipe(t, h, 40, 10)
	terminalTabs(t, p, tabs)
	f := texted(t, p, 0)
	rows := map[int]bool{}
	for _, a := range f {
		if a.Cursor != pointer.CursorText {
			continue
		}
		rows[a.Y] = true
		if a.X != 0 || a.W != 40 {
			t.Errorf("an I-beam row %+v, want the body's whole width, 0 to 40", a)
		}
	}
	if rows[0] || len(rows) != 9 {
		t.Fatalf("the I-beam over rows %v, want the body's nine (1 to 9), not the strip's row 0: %+v", rows, f)
	}
	n := p.update(t, func() { h.show(false) })
	if got := p.frame(t, n); from(got, pointer.CursorText) >= 0 {
		t.Fatalf("hidden, the view left an I-beam: %+v", got)
	}
}

// A terminal view in a Float concealed, as a window minimized is, leaves no I-beam over the page
// beneath it.
func TestAConcealedTerminalViewLeavesNoIBeam(t *testing.T) {
	tabs := tuiwidget.NewTerminalTabs()
	float := tuiwidget.NewFloat(tabs, tuiwidget.WithAnchor(tuiwidget.AtRect(tui.Rect{X: 20, Y: 0, W: 20, H: 10})))
	host := tuiwidget.NewOverlayHost(tuiwidget.NewText("page"))
	host.Attach(float)
	p := startPipe(t, host, 40, 10)
	p.update(t, func() { float.Show() })
	terminalTabs(t, p, tabs)
	if f := texted(t, p, 0); from(f, pointer.CursorText) != 20 {
		t.Fatalf("shown, the view's I-beam starts at column %d, want its Float's, 20: %+v", from(f, pointer.CursorText), f)
	}
	n := p.update(t, func() { float.SetVisible(false) })
	if got := p.frame(t, n); from(got, pointer.CursorText) >= 0 {
		t.Fatalf("concealed, the view left an I-beam over the page: %+v", got)
	}
}
