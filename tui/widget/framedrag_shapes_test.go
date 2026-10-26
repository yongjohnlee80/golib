package widget_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// framedrag_shapes_test.go: a window's frame shows the resize arrows its plain drags resize from
// (WindowCore.FrameShapes), asked after its content paints the frame (tui.SubtreePointerShaper).

// frameWindow is a movable, resizable window of a bordered Box saying text, floating at r.
func frameWindow(text string, r tui.Rect, opts ...widget.WindowModOption) (*widget.WindowMod, *widget.Float) {
	w := widget.NewWindowMod(widget.NewBox(widget.NewText(text), widget.WithBorder(style.BorderNormal)),
		append([]widget.WindowModOption{widget.WithWindowMove(), widget.WithWindowResize()}, opts...)...)
	return w, widget.NewFloat(w, widget.WithAnchor(widget.AtRect(r)))
}

// showShaped runs host on a 40 by 14 shape host reporting caps, shows floats, and waits for text.
func showShaped(t *testing.T, host *widget.OverlayHost, caps tui.Capabilities, text string, floats ...*widget.Float) *shapeHost {
	t.Helper()
	for _, f := range floats {
		host.Attach(f)
	}
	sh := runShapedOn(t, host, 40, 14, caps)
	done := make(chan struct{})
	sh.app.Update(func() {
		for _, f := range floats {
			f.Show()
		}
		close(done)
	})
	<-done
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(sh.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("%q never drawn:\n%s", text, sh.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	return sh
}

// win's cells at (5, 2), 12 by 6: its left and right columns, its bottom row and its corners.
var (
	winAt      = tui.Rect{X: 5, Y: 2, W: 12, H: 6}
	frameCells = map[[2]int]tui.PointerShape{
		{5, 4}: tui.PointerResizeEW, {16, 4}: tui.PointerResizeEW, // the sides
		{9, 7}: tui.PointerResizeNS,                                   // the bottom
		{5, 7}: tui.PointerResizeNESW, {16, 7}: tui.PointerResizeNWSE, // the bottom corners
		{9, 2}: tui.PointerDefault, {5, 2}: tui.PointerDefault, // the title row keeps the arrow
		{9, 4}: tui.PointerDefault, // the content
	}
)

// On gui's backend a window's sides, bottom and bottom corners show the resize arrows, over the
// frame its content's Box paints; its title row and its content keep the arrow.
func TestAWindowsFrameShowsItsResizeArrows(t *testing.T) {
	_, f := frameWindow("win", winAt)
	sh := showShaped(t, widget.NewOverlayHost(widget.NewText("base")), tui.Capabilities{FrameDrag: true}, "win", f)
	expect(t, sh, frameCells)
}

// On a terminal the frame shows no arrows: it takes no plain drag.
func TestATerminalsWindowFrameShowsNoArrows(t *testing.T) {
	_, f := frameWindow("win", winAt)
	sh := showShaped(t, widget.NewOverlayHost(widget.NewText("base")), tui.Capabilities{}, "win", f)
	none := map[[2]int]tui.PointerShape{}
	for c := range frameCells {
		none[c] = tui.PointerDefault
	}
	expect(t, sh, none)
}

// A window painted above covers the frame's arrows where it lies over them, and only there.
func TestAWindowAboveCoversTheFramesArrows(t *testing.T) {
	_, under := frameWindow("win", winAt)
	_, over := frameWindow("top", tui.Rect{X: 3, Y: 3, W: 5, H: 3}) // columns 3-7, rows 3-5: over the left side
	sh := showShaped(t, widget.NewOverlayHost(widget.NewText("base")), tui.Capabilities{FrameDrag: true}, "top", under, over)
	expect(t, sh, map[[2]int]tui.PointerShape{
		{5, 4}:  tui.PointerDefault,  // the lower window's left side, under the content of the one above
		{5, 6}:  tui.PointerResizeEW, // the same side, below the one above
		{16, 4}: tui.PointerResizeEW, // its right side, uncovered
		{3, 4}:  tui.PointerResizeEW, // the sides of the window above
		{7, 4}:  tui.PointerResizeEW,
	})
}

// A maximized window shows no arrows: it takes no drag.
func TestAMaximizedWindowsFrameShowsNoArrows(t *testing.T) {
	w, f := frameWindow("win", winAt, widget.WithWindowMaximize())
	sh := showShaped(t, widget.NewOverlayHost(widget.NewText("base")), tui.Capabilities{FrameDrag: true}, "win", f)
	expect(t, sh, frameCells)
	done := make(chan bool)
	sh.app.Update(func() { done <- w.ToggleMaximize() })
	if !<-done {
		t.Fatal("the window did not maximize")
	}
	expect(t, sh, map[[2]int]tui.PointerShape{
		{0, 5}: tui.PointerDefault, {39, 5}: tui.PointerDefault, {20, 13}: tui.PointerDefault,
		{5, 4}: tui.PointerDefault, {16, 4}: tui.PointerDefault, {9, 7}: tui.PointerDefault,
	})
}
