package widget_test

import (
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// cellsOf is row y's cells from x, n of them.
func cellsOf(h *harness, y, x, n int) string {
	r := []rune(h.row(y))
	if x+n > len(r) {
		return ""
	}
	return string(r[x : x+n])
}

// A Box's window controls are drawn as listed, in the order given, at the title row's right end
// with a border cell before the corner: none unlisted, the title truncated before them, and none on
// a box too narrow for them. TitleWindowControls reports them as drawn, for a painter.
func TestABoxDrawsOnlyTheWindowControlsItLists(t *testing.T) {
	box := core.NewBox(core.NewText("body"), core.WithTitle("abcdefghijklmnopqrstuvwxyz"), core.WithBorder(style.BorderNormal))
	h := startApp(t, box, 24, 4)
	defer h.stop()

	all := []core.WindowOperation{core.WindowMinimize, core.WindowMaximize, core.WindowClose}
	h.onLoop(func() { box.SetWindowControls(all) })
	h.waitFor("the three controls", func() bool { return cellsOf(h, 0, 13, 9) == " _  +  x " })
	if got := []rune(h.row(0)); strings.ContainsAny(string(got[12:13]), "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("the title runs into the border cell before the controls: %q", h.row(0))
	}
	var spans []core.TabControl
	var row int
	h.onLoop(func() { spans, row = box.TitleWindowControls(), box.TitleRow() })
	want := []core.TabControl{{X: 13, W: 3, Op: core.WindowMinimize}, {X: 16, W: 3, Op: core.WindowMaximize}, {X: 19, W: 3, Op: core.WindowClose}}
	if !slices.Equal(spans, want) || row != 0 {
		t.Errorf("TitleWindowControls %+v on row %d; want %+v on row 0", spans, row, want)
	}

	// x stays at 19 as it was with all three, so the wait is for _ and + to be gone: the new frame
	h.onLoop(func() { box.SetWindowControls([]core.WindowOperation{core.WindowClose}) })
	h.waitFor("x alone", func() bool { return !strings.ContainsAny(h.row(0), "_+") })
	if got := cellsOf(h, 0, 19, 3); got != " x " {
		t.Errorf("close alone drawn as %q at 19, want \" x \": %q", got, h.row(0))
	}

	h.onLoop(func() { box.SetWindowControls(nil) })
	h.waitFor("none", func() bool { return !strings.Contains(h.row(0), " x ") })
	h.onLoop(func() { spans = box.TitleWindowControls() })
	if len(spans) != 0 {
		t.Errorf("a box without controls reports %+v", spans)
	}
}

// A box too narrow for its controls (inner width under their cells + 4) hides them and reports none.
func TestANarrowBoxHidesItsWindowControls(t *testing.T) {
	box := core.NewBox(core.NewText("b"), core.WithTitle("t"), core.WithBorder(style.BorderNormal))
	box.SetWindowControls([]core.WindowOperation{core.WindowMinimize, core.WindowMaximize, core.WindowClose})
	h := startApp(t, box, 13, 3) // inner 11, under 9 + 4
	defer h.stop()
	h.settle()
	if r := h.row(0); strings.ContainsAny(r, "_+x") {
		t.Errorf("a box too narrow for its controls drew some: %q", r)
	}
	var spans []core.TabControl
	h.onLoop(func() { spans = box.TitleWindowControls() })
	if len(spans) != 0 {
		t.Errorf("hidden controls reported: %+v", spans)
	}
}

// controlWindow is a movable, resizable window of a titled Box with all three controls, floating at
// r on a backend that takes plain frame drags, as a window's: what a press on each control asked is
// recorded.
func controlWindow(t *testing.T, r tui.Rect) (*harness, *core.WindowMod, *[]core.WindowOperation) {
	t.Helper()
	h, w, asked, _ := controlWindowOf(t, r, core.NewText("body"))
	return h, w, asked
}

// controlWindowOf is controlWindow with content of the caller's and more window options, returning
// its overlay host too.
func controlWindowOf(t *testing.T, r tui.Rect, content tui.Component, opts ...core.WindowModOption) (*harness, *core.WindowMod, *[]core.WindowOperation, *core.OverlayHost) {
	t.Helper()
	box := core.NewBox(content, core.WithTitle("win"), core.WithBorder(style.BorderNormal))
	box.SetWindowControls([]core.WindowOperation{core.WindowMinimize, core.WindowMaximize, core.WindowClose})
	var asked []core.WindowOperation
	box.SetOnWindowControl(func(op core.WindowOperation) { asked = append(asked, op) })
	w := core.NewWindowMod(box, append([]core.WindowModOption{core.WithWindowMove(), core.WithWindowResize()}, opts...)...)
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(r)))
	host := core.NewOverlayHost(core.NewText("base"))
	host.Attach(f)
	tb := tui.NewTestBackend(50, 20, tui.WithTestCapabilities(tui.Capabilities{FrameDrag: true}))
	h := startAppOn(t, host, tb)
	h.onLoop(func() { f.Show() })
	h.waitFor("the window", func() bool { return strings.Contains(h.row(r.Y), " x ") })
	return h, w, &asked, host
}

// askedOn is what the controls asked so far, read on the loop.
func askedOn(h *harness, asked *[]core.WindowOperation) []core.WindowOperation {
	var got []core.WindowOperation
	h.onLoop(func() { got = slices.Clone(*asked) })
	return got
}

// leftPress is a left press at (x, y); its release is select_test.go's release.
func leftPress(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y}
}

// The release after a press a control claimed goes where any release goes, under the pointer, and
// nothing acts on it: the claim takes no capture, the window began no drag to end, and a Button
// under the pointer, whose press it never saw, does not activate. Released on the control itself,
// it asks nothing more either: a control acts on its press.
func TestTheReleaseAfterAClaimedPressActsOnNothing(t *testing.T) {
	at := tui.Rect{X: 5, Y: 2, W: 24, H: 6}
	var fired atomic.Int64
	button := core.NewButton("OK", core.WithOnActivate(func() { fired.Add(1) }))
	h, w, asked, _ := controlWindowOf(t, at, button)
	defer h.stop()
	bx, by := at.X+2, at.Y+1 // the Button, on the box's first content row
	h.waitFor("the Button", func() bool { return strings.Contains(h.row(by), "OK") })

	// the Button is where the cell thinks: a press and release on it activate it
	h.inject(leftPress(bx, by), release(bx, by))
	h.waitFor("the Button activated", func() bool { return fired.Load() == 1 })

	h.inject(leftPress(at.X+20, at.Y), release(bx, by)) // pressed on x, released over the Button
	h.settle()
	h.inject(leftPress(at.X+20, at.Y), release(at.X+20, at.Y)) // pressed and released on x
	h.settle()
	if got := askedOn(h, asked); !slices.Equal(got, []core.WindowOperation{core.WindowClose, core.WindowClose}) {
		t.Errorf("the controls asked %v; want one close per press", got)
	}
	if n := fired.Load(); n != 1 {
		t.Errorf("the Button activated %d times; want only its own press's once: a release after a claimed press activated it", n)
	}
	var capture bool
	h.onLoop(func() { capture = w.Context().HasPointerCapture() })
	if capture {
		t.Error("a capture is held after the claimed presses and their releases")
	}
	if got := windowBounds(h, w); got != at {
		t.Errorf("the window moved to %+v", got)
	}
}

// Two presses on a control in a double press are two requests, as a tab strip's controls make, and
// never the title's own double press: a window that maximizes on its title's double press does not
// on a control's. The title's text, double pressed, maximizes it, so that path is live here.
func TestADoublePressOnAControlIsTwoRequestsAndNeverTheTitlesMaximize(t *testing.T) {
	at := tui.Rect{X: 5, Y: 2, W: 24, H: 6}
	h, w, asked, _ := controlWindowOf(t, at, core.NewText("body"), core.WithWindowMaximize())
	defer h.stop()
	maximized := func() bool {
		var m bool
		h.onLoop(func() { m = w.Core().State().Maximized })
		return m
	}
	plus := at.X + 17
	h.inject(leftPress(plus, at.Y), release(plus, at.Y), leftPress(plus, at.Y), release(plus, at.Y))
	h.settle()
	if got := askedOn(h, asked); !slices.Equal(got, []core.WindowOperation{core.WindowMaximize, core.WindowMaximize}) {
		t.Errorf("a double press on + asked %v; want two maximize requests", got)
	}
	if maximized() {
		t.Fatal("a double press on + maximized the window: the title's double press reached a control")
	}
	title := at.X + 3
	h.inject(leftPress(title, at.Y), release(title, at.Y), leftPress(title, at.Y), release(title, at.Y))
	h.waitFor("the title's double press maximizes", maximized)
}

// A Box behind a dialog is as inert as its window's content: while the dialog traps focus, a press
// on its control is dropped before any resolver is asked, so nothing is requested. Once the dialog
// is gone, the same press asks again.
func TestABoxControlBehindADialogDoesNothing(t *testing.T) {
	at := tui.Rect{X: 5, Y: 2, W: 24, H: 6}
	h, _, asked, host := controlWindowOf(t, at, core.NewText("body"))
	defer h.stop()
	m := core.NewModal(core.NewText("ask"))
	var err error
	h.onLoop(func() { err = m.Open(host) })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	h.waitFor("the dialog", func() bool { return strings.Contains(h.screen(), "ask") })
	x := at.X + 20
	h.inject(leftPress(x, at.Y), release(x, at.Y))
	h.settle()
	if got := askedOn(h, asked); len(got) != 0 {
		t.Errorf("a control behind an open dialog asked %v; want nothing", got)
	}
	h.onLoop(func() { m.Dismiss(core.DismissProgrammatic) })
	h.settle()
	h.inject(leftPress(x, at.Y), release(x, at.Y))
	h.waitFor("the control asks once the dialog is gone", func() bool { return slices.Equal(askedOn(h, asked), []core.WindowOperation{core.WindowClose}) })
}

// A press on a Box's window control is the control's, whichever way the window would take it: its
// plain drag on the frame (its unclaimed press, asked last) and its modified drag (its resolver,
// asked before the press is delivered) both leave it, so the control's function runs once and no
// drag begins. The same presses on the title's text start the drags, so both paths are live here.
func TestAPressOnABoxWindowControlNeverStartsTheWindowsDrag(t *testing.T) {
	at := tui.Rect{X: 5, Y: 2, W: 24, H: 6}
	// the controls' columns in the window: 13, 16, 19, so at 18, 21 and 24 on screen
	for _, tc := range []struct {
		name string
		mods tui.Mods
		x    int
		want core.WindowOperation
	}{
		{"a plain press on x (the frame's unclaimed press)", 0, at.X + 20, core.WindowClose},
		{"an Alt press on _ (the window's resolver)", tui.ModAlt, at.X + 14, core.WindowMinimize},
		{"a plain press on +", 0, at.X + 17, core.WindowMaximize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, w, asked := controlWindow(t, at)
			defer h.stop()
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tc.mods, X: tc.x, Y: at.Y})
			h.settle()
			var capture bool
			var got []core.WindowOperation
			h.onLoop(func() { capture, got = w.Context().HasPointerCapture(), slices.Clone(*asked) })
			if capture {
				t.Error("the press on a control began the window's drag")
			}
			if !slices.Equal(got, []core.WindowOperation{tc.want}) {
				t.Errorf("the control asked %v; want [%v]", got, tc.want)
			}
			h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: tc.x + 4, Y: at.Y + 3},
				tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: tc.x + 4, Y: at.Y + 3})
			h.settle()
			if got := windowBounds(h, w); got != at {
				t.Errorf("the window moved to %+v; want it left at %+v", got, at)
			}
		})
	}

	for _, tc := range []struct {
		name string
		mods tui.Mods
	}{{"plain", 0}, {"Alt", tui.ModAlt}} {
		t.Run(tc.name+" press on the title's text drags", func(t *testing.T) {
			h, w, asked := controlWindow(t, at)
			defer h.stop()
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tc.mods, X: at.X + 3, Y: at.Y})
			h.settle()
			var capture bool
			h.onLoop(func() { capture = w.Context().HasPointerCapture() })
			if !capture {
				t.Error("a press on the title's text began no drag: the path the controls must leave is not live here")
			}
			h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: at.X + 3, Y: at.Y})
			h.settle()
			if len(*asked) != 0 {
				t.Errorf("a press on the title's text asked %v", *asked)
			}
		})
	}
}
