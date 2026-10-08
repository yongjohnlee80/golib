package widget_test

import (
	"context"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	gw "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestNativeEditorWindowAltDragAndEscapeUseSharedBehavior(t *testing.T) {
	editor := gw.NewEditor(gw.WithPanel(gw.WithTitle("Notes"), gw.WithWindowCapabilities(widget.WithWindowMove(), widget.WithWindowResize())))
	float := widget.NewFloat(editor)
	host := widget.NewOverlayHost(widget.NewButton("base"))
	tb := tui.NewTestBackend(60, 20)
	app := tui.NewApp(host, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("GUI window test did not stop")
		}
	})
	h := &panelHarness{t: t, app: app, tb: tb, panel: editor.Panel()}
	h.until("frame", func() bool { return tb.Flushes() > 0 })
	h.onLoop(func() {
		host.Attach(float)
		editor.Panel().InFloat(float, tui.Rect{X: 2, Y: 2, W: 24, H: 7})
		float.Show()
	})
	h.until("editor window", func() bool { return h.bounds() == (tui.Rect{X: 2, Y: 2, W: 24, H: 7}) })
	if err := tb.Inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 10, Y: 5},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 15, Y: 7}); err != nil {
		t.Fatal(err)
	}
	h.until("native content drag", func() bool { return h.bounds() == (tui.Rect{X: 7, Y: 4, W: 24, H: 7}) })
	if err := tb.Inject(tui.KeyEvent{Code: tui.KeyEscape}); err != nil {
		t.Fatal(err)
	}
	h.until("cancelled native drag", func() bool { return h.bounds() == (tui.Rect{X: 2, Y: 2, W: 24, H: 7}) })
	h.onLoop(func() {
		if editor.WindowBehavior() != editor.Panel().WindowBehavior() {
			t.Error("Editor did not expose the Panel's shared core")
		}
	})
}

func TestGuiWindowButtonsPaintRealControlGeometry(t *testing.T) {
	model := widget.NewMinimizedWindows()
	body := tui.NewFlex(tui.Vertical)
	window := widget.NewWindowMod(body, widget.WithWindowMaximize(), widget.WithWindowMinimize(model), widget.WithWindowClose())
	controls := gw.NewWindowButtons(window.Core())
	body.Add(controls)
	body.AddWeighted(widget.NewEditor(), 1)
	float := widget.NewFloat(window, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 20, H: 8})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
	tb := tui.NewTestBackend(40, 15)
	app := tui.NewApp(host, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("GUI controls test did not stop")
		}
	})
	h := &panelHarness{t: t, app: app, tb: tb}
	h.until("frame", func() bool { return tb.Flushes() > 0 })
	h.onLoop(func() { host.Attach(float); float.Show() })
	h.until("control layout", func() bool { var r tui.Rect; h.onLoop(func() { r = window.Core().Bounds() }); return r.W == 20 })
	h.onLoop(func() {
		v, ok := controls.NativeView()
		if !ok {
			t.Error("no GUI controls view")
			return
		}
		canvas := gui.NewRecordingCanvas(gui.Size{W: 160, H: 16}, gui.Size{W: 8, H: 16})
		v.(gui.View).Paint(canvas)
		dots := 0
		var centres []gui.Point
		for _, child := range window.Context().Children(controls) {
			if button, ok := child.(*widget.Button); ok {
				if r, valid := window.Context().ResolveAnchor(button.Context().NodeAnchor()); valid {
					centres = append(centres, gui.Pt((float32(r.X)+float32(r.W)/2)*8, (float32(r.Y)+float32(r.H)/2)*16))
				}
			}
		}
		for _, call := range canvas.Calls {
			if call.Op == "FillEllipse" {
				if dots >= len(centres) || centres[dots] != gui.Pt(call.Rect.X+call.Rect.W/2, call.Rect.Y+call.Rect.H/2) {
					t.Error("circle centre differs from its real Button region")
				}
				dots++
				if call.Rect.X < 0 || call.Rect.X+call.Rect.W > 160 {
					t.Error("circle painted outside its control row")
				}
			}
		}
		if dots != 3 {
			t.Errorf("painted %d circles, want 3 enabled real controls", dots)
		}
	})
}

func TestPanelCollapsedMoveRestoresAtTheMovedPosition(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 30, H: 6}, gw.Movable(), gw.Minimizable())
	h.mouse(tui.MousePress, 30, 2)
	h.mouse(tui.MouseRelease, 30, 2)
	h.until("collapsed", func() bool {
		var height int
		h.onLoop(func() { height = h.panel.WindowBehavior().Bounds().H })
		return height == 1
	})
	h.mouse(tui.MousePress, 10, 2)
	h.mouse(tui.MouseMotion, 15, 3)
	h.mouse(tui.MouseRelease, 15, 3)
	h.until("moved collapsed row", func() bool {
		var bounds tui.Rect
		h.onLoop(func() { bounds = h.panel.WindowBehavior().Bounds() })
		return bounds == (tui.Rect{X: 7, Y: 3, W: 30, H: 1})
	})
	h.mouse(tui.MousePress, 35, 3)
	h.mouse(tui.MouseRelease, 35, 3)
	h.until("restored at moved position", func() bool { return h.bounds() == (tui.Rect{X: 7, Y: 3, W: 30, H: 6}) })
}

func TestPanelCapabilityOnlyDoubleClickMaximize(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 30, H: 6}, gw.WithWindowCapabilities(widget.WithWindowMaximize()))
	h.mouse(tui.MousePress, 10, 2)
	h.mouse(tui.MouseRelease, 10, 2)
	h.mouse(tui.MousePress, 10, 2)
	h.mouse(tui.MouseRelease, 10, 2)
	h.until("capability-only double click", func() bool { return h.bounds() == (tui.Rect{W: 60, H: 20}) })
}

func TestPanelConsumerResizeMinimumIsNotOverwritten(t *testing.T) {
	h := startPanel(t, tui.Rect{X: 2, Y: 2, W: 30, H: 10}, gw.Resizable(),
		gw.WithWindowCapabilities(widget.WithWindowMinSize(tui.Size{W: 25, H: 8})))
	h.onLoop(func() {
		if !h.panel.WindowBehavior().ResizeBy(-100, -100) {
			t.Error("resize refused")
		}
	})
	h.until("consumer minimum", func() bool { return h.bounds() == (tui.Rect{X: 2, Y: 2, W: 25, H: 8}) })
}
