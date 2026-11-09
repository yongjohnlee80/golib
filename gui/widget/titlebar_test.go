package widget

import (
	"context"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/io/system"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// fakeWindow is a window's controls that records what it is asked.
type fakeWindow struct {
	maxed bool
	asked []system.Action
}

func (f *fakeWindow) Perform(a system.Action) { f.asked = append(f.asked, a) }
func (f *fakeWindow) Maximized() bool         { return f.maxed }

// titleBarApp runs bar as the root of a w x 1 TestBackend with caps, and waits for its first frame.
func titleBarApp(t *testing.T, bar *TitleBar, w int, caps tui.Capabilities) (*tui.App, *tui.TestBackend) {
	t.Helper()
	tb := tui.NewTestBackend(w, 1, tui.WithTestCapabilities(caps))
	app := tui.NewApp(bar, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("the title bar's App did not stop")
		}
	})
	for deadline := time.Now().Add(5 * time.Second); tb.Flushes() == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no frame")
		}
	}
	return app, tb
}

// onLoop runs fn on app's loop and waits for it.
func onLoop(t *testing.T, app *tui.App, fn func()) {
	t.Helper()
	done := make(chan struct{})
	app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the loop did not run the update")
	}
}

// On a terminal there is no window: the bar is a reversed row with the title and the trailing
// widgets at its right, and no controls.
func TestATitleBarOnATerminalIsARowWithNoControls(t *testing.T) {
	bar := NewTitleBar("Notes", core.NewText("synced"))
	app, tb := titleBarApp(t, bar, 30, tui.Capabilities{})
	want := " Notes" + strings.Repeat(" ", 30-len(" Notes")-len("synced")) + "synced"
	for deadline := time.Now().Add(3 * time.Second); tb.String() != want && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
	}
	if got := tb.String(); got != want {
		t.Fatalf("the bar is %q, want %q", got, want)
	}
	row := tb.Snapshot()[0]
	for x := 1; x <= len("Notes"); x++ { // the title is bold on the reversed bar; the gap is not bold
		if a := row[x].Attrs.Mask; a&tui.AttrBold == 0 || a&tui.AttrReverse == 0 {
			t.Fatalf("title cell %d attrs %v, want bold and reversed", x, a)
		}
	}
	if a := row[10].Attrs.Mask; a&tui.AttrBold != 0 || a&tui.AttrReverse == 0 {
		t.Fatalf("gap cell attrs %v, want reversed, not bold", a)
	}
	onLoop(t, app, func() {
		if bar.window != nil || len(bar.controls()) != 0 {
			t.Error("a title bar on a terminal has window controls")
		}
		if bar.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 29, Count: 2}) {
			t.Error("a terminal's title bar took a double-click")
		}
	})
}

// With a window, the controls (close, maximize, minimize from the right, three cells each) act on
// it on release; maximize restores a maximized window; a double-click on the bar maximizes.
func TestATitleBarsControlsActOnItsWindow(t *testing.T) {
	bar := NewTitleBar("Notes")
	win := &fakeWindow{}
	bar.window = win // as Init finds it on the GUI backend
	app, _ := titleBarApp(t, bar, 30, tui.Capabilities{NativeViews: true})
	click := func(x int) {
		bar.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Count: 1})
		bar.HandleEvent(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x})
	}
	onLoop(t, app, func() {
		click(28) // close: columns 27-29
		click(25) // maximize: 24-26
		win.maxed = true
		click(24) // maximize on a maximized window restores it
		click(21) // minimize: 21-23
		click(10) // the title: nothing
		bar.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 10, Count: 2})
	})
	want := []system.Action{system.ActionClose, system.ActionMaximize, system.ActionUnmaximize, system.ActionMinimize, system.ActionUnmaximize}
	if len(win.asked) != len(want) {
		t.Fatalf("the window was asked %v, want %v", win.asked, want)
	}
	for i := range want {
		if win.asked[i] != want[i] {
			t.Fatalf("the window was asked %v, want %v", win.asked, want)
		}
	}
}

// Natively, the bar paints three controls in their colours and marks the window's drag area over
// its empty part: from its left edge to the trailing widget, not over the widget or the controls.
func TestATitleBarPaintsItsControlsAndItsDragArea(t *testing.T) {
	bar := NewTitleBar("Notes", core.NewText("synced"))
	bar.window = &fakeWindow{} // as Init finds it on the GUI backend
	app, _ := titleBarApp(t, bar, 30, tui.Capabilities{NativeViews: true})
	onLoop(t, app, func() {
		v, ok := bar.NativeView()
		if !ok {
			t.Error("no native view")
			return
		}
		c := gui.NewRecordingCanvas(gui.Size{W: 240, H: 16}, gui.Size{W: 8, H: 16})
		own := color.NRGBA{R: 0xf4, G: 0xec, B: 0xd8, A: 0xff}
		c.Colors = func(int, int) (color.NRGBA, color.NRGBA) { return own, own }
		v.(gui.View).Paint(c)
		var dots []color.NRGBA
		var move []gui.Rect
		for _, call := range c.Calls {
			switch call.Op {
			case "FillEllipse":
				dots = append(dots, call.Brush.Color)
			case "WindowMoveArea":
				move = append(move, call.Rect)
			}
		}
		if len(dots) != 3 || dots[0] != closeRed || dots[1] != maximizeGreen || dots[2] != minimizeAmber {
			t.Errorf("controls painted %v; want close, maximize and minimize from the right", dots)
		}
		// 30 columns: controls at 21-29, "synced" at 15-20, so the drag area is columns 0-13
		if len(move) != 1 || move[0] != (gui.Rect{W: 14 * 8, H: 16}) {
			t.Errorf("drag areas %v; want one over columns 0-13", move)
		}
	})
}

// SetTitle repaints the title; Title reads it.
func TestATitleBarsTitleChanges(t *testing.T) {
	bar := NewTitleBar("Notes")
	app, tb := titleBarApp(t, bar, 20, tui.Capabilities{})
	onLoop(t, app, func() { bar.SetTitle("Journal") })
	for deadline := time.Now().Add(3 * time.Second); !strings.Contains(tb.String(), "Journal") && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
	}
	if !strings.Contains(tb.String(), "Journal") || bar.Title() != "Journal" {
		t.Fatalf("after SetTitle the bar is %q (Title %q)", tb.String(), bar.Title())
	}
}

// A control acts only when its press and its release both land on it: a press on maximize released
// over close does neither, a release over close that no press armed does nothing, and a press on
// close dragged off the bar is cancelled.
func TestATitleBarsControlActsOnlyOnItsOwnPressAndRelease(t *testing.T) {
	bar := NewTitleBar("Notes")
	win := &fakeWindow{}
	bar.window = win
	app, _ := titleBarApp(t, bar, 30, tui.Capabilities{NativeViews: true})
	ev := func(kind tui.MouseKind, x, y int) {
		bar.HandleEvent(tui.MouseEvent{Kind: kind, Button: tui.MouseLeft, X: x, Y: y, Count: 1})
	}
	onLoop(t, app, func() {
		ev(tui.MousePress, 25, 0)   // maximize
		ev(tui.MouseRelease, 28, 0) // released over close
		ev(tui.MouseRelease, 28, 0) // a release no press armed
		ev(tui.MousePress, 28, 0)   // close
		ev(tui.MouseRelease, 28, 1) // released off the bar
		ev(tui.MouseRelease, 28, 0) // and no press armed this one
	})
	if len(win.asked) != 0 {
		t.Fatalf("the window was asked %v; want nothing", win.asked)
	}
	onLoop(t, app, func() { ev(tui.MousePress, 28, 0); ev(tui.MouseRelease, 28, 0) })
	if len(win.asked) != 1 || win.asked[0] != system.ActionClose {
		t.Fatalf("a whole click on close asked %v; want ActionClose", win.asked)
	}
}
