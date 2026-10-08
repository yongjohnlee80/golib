package widget_test

import (
	"context"
	"image/color"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	gw "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

type panelHarness struct {
	t     *testing.T
	app   *tui.App
	tb    *tui.TestBackend
	panel *gw.Panel
}

func startPanel(t *testing.T, at tui.Rect, opts ...gw.PanelOption) *panelHarness {
	t.Helper()
	panel := gw.NewPanel(widget.NewText("content"), append([]gw.PanelOption{gw.WithTitle("Notes")}, opts...)...)
	float := widget.NewFloat(panel)
	bg := tui.NewStack()
	bg.AddAt(widget.NewText("background"), 0, 0)
	bg.AddAt(widget.NewText("under the panel"), 5, 4)
	host := widget.NewOverlayHost(bg)
	tb := tui.NewTestBackend(60, 20)
	app := tui.NewApp(host, tui.WithBackend(tb))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	h := &panelHarness{t: t, app: app, tb: tb, panel: panel}
	h.until("a frame", func() bool { return tb.Flushes() > 0 })
	h.onLoop(func() {
		host.Attach(float)
		panel.InFloat(float, at)
		float.Show()
	})
	h.until("the panel", func() bool { return strings.Contains(tb.String(), "Notes") })
	return h
}

func (h *panelHarness) onLoop(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatal("the loop did not run the update")
	}
}

func (h *panelHarness) until(what string, cond func() bool) {
	h.t.Helper()
	for range 300 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s; screen:\n%s", what, h.tb.String())
}

func (h *panelHarness) mouse(kind tui.MouseKind, x, y int) {
	h.t.Helper()
	if err := h.tb.Inject(tui.MouseEvent{Kind: kind, Button: tui.MouseLeft, X: x, Y: y}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *panelHarness) bounds() tui.Rect {
	var r tui.Rect
	h.onLoop(func() { r = h.panel.Bounds() })
	return r
}

// row is the screen's row y.
func (h *panelHarness) row(y int) string {
	rows := strings.Split(h.tb.String(), "\n")
	if y < len(rows) {
		return rows[y]
	}
	return ""
}

func TestPanelMovesByItsTitleBar(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 24, H: 6}, gw.Movable())
	h.mouse(tui.MousePress, 10, 2)
	h.mouse(tui.MouseMotion, 15, 3)
	h.mouse(tui.MouseMotion, 17, 4)
	h.mouse(tui.MouseRelease, 17, 4)
	h.until("the move", func() bool { return h.bounds() == tui.Rect{X: 9, Y: 4, W: 24, H: 6} })
	h.until("the title on its new row", func() bool { return strings.Contains(h.row(4), "Notes") })
}

func TestPanelResizesByItsCorner(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 24, H: 6}, gw.Resizable())
	h.mouse(tui.MousePress, 2+24-1, 2+6-1)
	h.mouse(tui.MouseMotion, 2+24+3, 2+6+2)
	h.mouse(tui.MouseRelease, 2+24+3, 2+6+2)
	h.until("the resize", func() bool { return h.bounds() == tui.Rect{X: 2, Y: 2, W: 28, H: 9} })
}

func TestPanelWithoutTheOptionsStaysPut(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 24, H: 6})
	h.mouse(tui.MousePress, 10, 2)
	h.mouse(tui.MouseMotion, 15, 5)
	h.mouse(tui.MouseRelease, 15, 5)
	time.Sleep(50 * time.Millisecond)
	if b := h.bounds(); b != (tui.Rect{X: 2, Y: 2, W: 24, H: 6}) {
		t.Fatalf("an immovable panel moved to %+v", b)
	}
	if strings.Contains(h.row(2), "[x]") {
		t.Errorf("a panel without Closable shows a close control: %q", h.row(2))
	}
}

func TestPanelControls(t *testing.T) {
	var closed atomic.Bool
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 30, H: 6},
		gw.Closable(func() { closed.Store(true) }), gw.Minimizable(), gw.Maximizable())
	title := h.row(2)
	for _, c := range []string{"[x]", "[□]", "[_]"} {
		if !strings.Contains(title, c) {
			t.Fatalf("title bar %q lacks %s", title, c)
		}
	}
	right := 2 + 30
	click := func(x int) { h.mouse(tui.MousePress, x, 2); h.mouse(tui.MouseRelease, x, 2) }

	click(right - 3*3 + 1) // [_]
	h.until("minimized", func() bool {
		var m bool
		h.onLoop(func() { m = h.panel.Minimized() })
		return m
	})
	h.until("the content gone", func() bool { return !strings.Contains(h.tb.String(), "content") })
	click(right - 3*3 + 1)
	h.until("restored", func() bool { return strings.Contains(h.tb.String(), "content") })

	click(right - 2*3 + 1) // [□]
	h.until("maximized", func() bool {
		var m bool
		h.onLoop(func() { m = h.panel.Maximized() })
		return m
	})
	// maximized, the panel fills the 60-column screen: its bar is row 0, its controls at the right
	h.until("the bar on row 0", func() bool {
		return strings.Contains(h.row(0), "Notes") && strings.HasSuffix(strings.TrimRight(h.row(0), " "), "[x]")
	})
	h.mouse(tui.MousePress, 60-2*3+1, 0)
	h.mouse(tui.MouseRelease, 60-2*3+1, 0)
	h.until("restored to its rect", func() bool {
		return strings.Contains(h.row(2), "Notes") && h.bounds() == tui.Rect{X: 2, Y: 2, W: 30, H: 6}
	})

	click(right - 3 + 1) // [x]
	h.until("close called", closed.Load)
}

func TestPanelNativeBar(t *testing.T) {
	p := gw.NewPanel(widget.NewText("x"), gw.WithTitle("Notes"), gw.Closable(func() {}), gw.Minimizable(), gw.Maximizable())
	v, ok := p.NativeView()
	if !ok {
		t.Fatal("no native view")
	}
	rc := gui.NewRecordingCanvas(gui.Size{W: 240, H: 80}, gui.Size{W: 8, H: 16})
	v.(gui.View).Paint(rc)
	dots, texts := 0, 0
	for _, c := range rc.Calls {
		switch c.Op {
		case "FillEllipse":
			dots++
		case "DrawText":
			texts++
		}
	}
	if dots != 3 || texts != 1 {
		t.Errorf("native bar drew %d controls and %d texts; want 3 and the title", dots, texts)
	}
	if p.NativeScope() != tui.ScopeChrome {
		t.Errorf("scope %v, want ScopeChrome: the children's cells cover the bar", p.NativeScope())
	}
}

func TestPanelMinimizedCollapsesItsFloat(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 30, H: 6}, gw.Minimizable())
	h.until("the content", func() bool { return strings.Contains(h.tb.String(), "content") })
	if strings.Contains(h.row(4), "under the panel") {
		t.Fatalf("the open panel does not cover the background: %q", h.row(4))
	}
	h.mouse(tui.MousePress, 2+30-3+1, 2)
	h.mouse(tui.MouseRelease, 2+30-3+1, 2)
	h.until("the title row alone", func() bool { return h.bounds() == tui.Rect{X: 2, Y: 2, W: 30, H: 1} })
	// what was under the panel's content shows again
	h.until("the background under it", func() bool { return strings.Contains(h.row(4), "under the panel") })
	if !strings.Contains(h.row(2), "Notes") {
		t.Errorf("the title bar went with the content: %q", h.row(2))
	}
}

func TestPanelBoundsWhileMaximized(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 30, H: 6}, gw.Maximizable())
	h.mouse(tui.MousePress, 2+30-3+1, 2)
	h.mouse(tui.MouseRelease, 2+30-3+1, 2)
	h.until("the whole screen", func() bool { return h.bounds() == tui.Rect{W: 60, H: 20} })
}

// The bar is a surface of the page around the panel, not the cells' reversed row: on a dark page
// with a reversed bar style (white cells), the bar stays dark and the title is a muted light.
func TestPanelBarIsAPageSurface(t *testing.T) {
	p := gw.NewPanel(widget.NewText("x"), gw.WithTitle("Notes"))
	v, _ := p.NativeView()
	white := color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}
	page := color.NRGBA{R: 0x1e, G: 0x1e, B: 0x24, A: 0xff}
	rc := gui.NewRecordingCanvas(gui.Size{W: 240, H: 80}, gui.Size{W: 8, H: 16})
	rc.Around = page
	rc.Colors = func(int, int) (fg, bg color.NRGBA) { return page, white } // reversed
	v.(gui.View).Paint(rc)
	lum := func(c color.NRGBA) int { return int(c.R) + int(c.G) + int(c.B) }
	for _, c := range rc.Calls {
		if c.Op == "FillRect" && c.Rect.Y == 0 && c.Rect.W == 240 {
			if lum(c.Brush.Color)-lum(page) > 60 {
				t.Errorf("bar fill %v is a band off the page %v", c.Brush.Color, page)
			}
		}
		if c.Op == "DrawText" {
			if col := c.Brush.Color; lum(col) <= lum(page) || col == white {
				t.Errorf("title %v: want a muted light on the dark page, not the page or full white", col)
			}
			if c.Text.Height > 16*0.92*1.4 {
				t.Errorf("title %vpx tall: not at the window's text size", c.Text.Height)
			}
		}
	}
	text, fill, pg := p.BarColors()
	if text != white || pg != page || fill == white {
		t.Errorf("BarColors %v %v %v: want the text white, the page %v, a fill off the page", text, fill, pg, page)
	}
}
