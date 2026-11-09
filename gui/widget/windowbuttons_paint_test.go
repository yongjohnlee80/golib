package widget_test

import (
	"context"
	"image/color"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	gw "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// Each control's circle has its operation's colour (WithWindowButtonColors), the labels set
// through WithWindowButtonWidgets reach the real Buttons, a disabled control's circle is faint, an
// armed one is ringed, and a row wholly covered by another panel paints nothing.
func TestGuiWindowButtonsColourEachControlByItsOperation(t *testing.T) {
	closeC := color.NRGBA{R: 0x10, A: 0xff}
	maxC := color.NRGBA{G: 0x20, A: 0xff}
	minC := color.NRGBA{B: 0x30, A: 0xff}
	model := core.NewMinimizedWindows()
	body := tui.NewFlex(tui.Vertical)
	window := core.NewWindowMod(body, core.WithWindowMaximize(), core.WithWindowMinimize(model), core.WithWindowClose())
	controls := gw.NewWindowButtons(window.Core(), nil,
		gw.WithWindowButtonColors(closeC, maxC, minC),
		gw.WithWindowButtonWidgets(core.WithWindowButtonLabels("C", "M", "N")))
	body.Add(controls)
	body.AddWeighted(editor.NewEditor(), 1)
	float := core.NewFloat(window, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 20, H: 8})))
	host := core.NewOverlayHost(core.NewButton("base"))
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

	want := map[string]color.NRGBA{"C": closeC, "M": maxC, "N": minC}
	h.onLoop(func() {
		if controls.HandleEvent(tui.KeyEvent{}) {
			t.Error("WindowButtons consumed an event: its Buttons take input")
		}
		v, ok := controls.NativeView()
		if !ok {
			t.Error("no GUI controls view")
			return
		}
		buttons := map[gui.Point]*core.Button{}
		for _, child := range window.Context().Children(controls) {
			if b, ok := child.(*core.Button); ok {
				if r, valid := window.Context().ResolveAnchor(b.Context().NodeAnchor()); valid {
					buttons[gui.Pt((float32(r.X)+float32(r.W)/2)*8, (float32(r.Y)+float32(r.H)/2)*16)] = b
				}
			}
		}
		if len(buttons) != 3 {
			t.Errorf("%d placed controls, want 3", len(buttons))
			return
		}
		paint := func() []gui.PaintCall {
			c := gui.NewRecordingCanvas(gui.Size{W: 160, H: 16}, gui.Size{W: 8, H: 16})
			v.(gui.View).Paint(c)
			return c.Calls
		}
		// check paints the row and, for each circle, finds its Button by centre and wants its
		// colour at alpha a, and a ring after it exactly when ringed names it.
		check := func(faint, ringed string) {
			t.Helper()
			calls := paint()
			seen := map[string]bool{}
			for i, call := range calls {
				if call.Op != "FillEllipse" {
					continue
				}
				b := buttons[gui.Pt(call.Rect.X+call.Rect.W/2, call.Rect.Y+call.Rect.H/2)]
				if b == nil {
					t.Errorf("a circle at %v over no control", call.Rect)
					continue
				}
				label := b.Label()
				seen[label] = true
				col := want[label]
				if label == faint {
					col.A = 90
				}
				if call.Brush.Color != col {
					t.Errorf("control %q painted %v, want %v", label, call.Brush.Color, col)
				}
				rung := i+1 < len(calls) && calls[i+1].Op == "StrokeRRect"
				if rung != (label == ringed) {
					t.Errorf("control %q ringed %v, want %v", label, rung, label == ringed)
				}
			}
			if len(seen) != 3 {
				t.Errorf("circles for %v, want C, M and N", seen)
			}
		}
		check("", "")

		var maxB, closeB *core.Button
		for _, b := range buttons {
			switch b.Label() {
			case "M":
				maxB = b
			case "C":
				closeB = b
			}
		}
		if maxB == nil || closeB == nil {
			t.Error("the labels set through WithWindowButtonWidgets did not reach the Buttons")
			return
		}
		maxB.SetEnabled(false)
		closeB.SetArmed(true)
		check("M", "C")
		maxB.SetEnabled(true)
		closeB.SetArmed(false)

		covered := gui.NewRecordingCanvas(gui.Size{W: 160, H: 16}, gui.Size{W: 8, H: 16})
		covered.Covers = func(int, int) bool { return true }
		v.(gui.View).Paint(covered)
		if len(covered.Calls) != 0 {
			t.Errorf("a wholly covered row painted %+v; want nothing (the host paints the covering cells)", covered.Calls)
		}
	})
}
