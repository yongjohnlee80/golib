package widget_test

import (
	"slices"
	"strings"
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
	box := core.NewBox(core.NewText("body"), core.WithTitle("win"), core.WithBorder(style.BorderNormal))
	box.SetWindowControls([]core.WindowOperation{core.WindowMinimize, core.WindowMaximize, core.WindowClose})
	var asked []core.WindowOperation
	box.SetOnWindowControl(func(op core.WindowOperation) { asked = append(asked, op) })
	w := core.NewWindowMod(box, core.WithWindowMove(), core.WithWindowResize())
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(r)))
	host := core.NewOverlayHost(core.NewText("base"))
	host.Attach(f)
	tb := tui.NewTestBackend(50, 20, tui.WithTestCapabilities(tui.Capabilities{FrameDrag: true}))
	h := startAppOn(t, host, tb)
	h.onLoop(func() { f.Show() })
	h.waitFor("the window", func() bool { return strings.Contains(h.row(r.Y), " x ") })
	return h, w, &asked
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
