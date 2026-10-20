package widget_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// AtLive places the float at the rectangle it reports at each placement: a change to it is followed
// at the next layout, with no new anchor set; while it reports false the float is where its
// otherwise anchor puts it. A live-anchored float is docked, not floating.
func TestALiveAnchoredFloatFollowsItsRectangle(t *testing.T) {
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	var x atomic.Int64
	var ok atomic.Bool
	x.Store(4)
	ok.Store(true)
	live := widget.AtLive(func() (tui.Rect, bool) { return tui.Rect{X: int(x.Load()), Y: 2, W: 8, H: 3}, ok.Load() }, widget.Left)
	f := widget.NewFloat(widget.NewBox(widget.NewButton("in"), widget.WithTitle("P")), widget.WithAnchor(live))
	host.Attach(f)
	h := startApp(t, host, 30, 8)
	defer h.stop()
	h.onLoop(f.Show)
	h.settle()
	col := func() int {
		for _, line := range strings.Split(h.tb.String(), "\n") {
			if i := strings.Index(line, "P"); i >= 0 && strings.Contains(line, "┌") {
				return strings.Index(line, "┌")
			}
		}
		return -1
	}
	if got := col(); got != 4 {
		t.Fatalf("live at column 4, the box is at %d:\n%s", got, h.tb.String())
	}
	x.Store(12)
	h.onLoop(f.RequestLayout)
	h.settle()
	if got := col(); got != 12 {
		t.Fatalf("live moved to 12, the box is at %d:\n%s", got, h.tb.String())
	}
	ok.Store(false)
	h.onLoop(f.RequestLayout)
	h.settle()
	if got := col(); got != 0 {
		t.Fatalf("live reports none: the box should be where Left puts it, column 0; it is at %d:\n%s", got, h.tb.String())
	}
	if f.Floating() {
		t.Fatal("a live-anchored float reports floating")
	}
}

// A window in a live-anchored Float (a Float with a window behaviour, laid out on the managed path)
// is placed at the live rectangle, and a move starts from it: one step right lands a cell right of
// where the live rectangle put it, the window then at an explicit rectangle, floating.
func TestAWindowInALiveAnchoredFloatMovesFromItsRectangle(t *testing.T) {
	w := widget.NewWindowMod(widget.NewBox(widget.NewButton("in"), widget.WithTitle("W")), widget.WithWindowMove())
	live := widget.AtLive(func() (tui.Rect, bool) { return tui.Rect{X: 5, Y: 1, W: 10, H: 4}, true }, widget.Left)
	f := widget.NewFloat(w, widget.WithAnchor(live))
	host := widget.NewOverlayHost(widget.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 30, 8)
	defer h.stop()
	h.onLoop(f.Show)
	h.settle()
	col := func() int {
		for _, line := range strings.Split(h.tb.String(), "\n") {
			if strings.Contains(line, "W") && strings.Contains(line, "┌") {
				return len([]rune(line[:strings.Index(line, "┌")]))
			}
		}
		return -1
	}
	if got := col(); got != 5 {
		t.Fatalf("the window should be at the live column 5; it is at %d:\n%s", got, h.tb.String())
	}
	h.onLoop(func() {
		w.Core().HandleAction(tui.ActionInvocation{Action: widget.WindowMoveByAction{DX: 1}, Origin: tui.OriginProgrammatic})
	})
	h.settle()
	if got := col(); got != 6 {
		t.Fatalf("one step right of the live column 5 is 6; the window is at %d:\n%s", got, h.tb.String())
	}
	if !f.Floating() {
		t.Fatal("moved off its live rectangle, the window should float")
	}
}

// placedAt is where the box titled title starts on the harness' screen: its frame's top-left corner.
func placedAt(h *harness, title string) (x, y int, ok bool) {
	for row, line := range strings.Split(h.tb.String(), "\n") {
		if strings.Contains(line, title) && strings.Contains(line, "┌") {
			return len([]rune(line[:strings.Index(line, "┌")])), row, true
		}
	}
	return 0, 0, false
}

// While its live rectangle reports none, a live-anchored float is placed by its fallback whole: an
// AtRect fallback at its rectangle, an AtFraction one at its fraction of the area, whether the float
// is a plain one or holds a window (the managed path).
func TestALiveAnchorWithoutARectangleKeepsItsWholeFallback(t *testing.T) {
	none := func() (tui.Rect, bool) { return tui.Rect{}, false }
	for _, c := range []struct {
		name     string
		fallback widget.Anchor
		window   bool
		x, y     int
	}{
		{"AtRect", widget.AtRect(tui.Rect{X: 7, Y: 2, W: 10, H: 4}), false, 7, 2},
		{"AtFraction", widget.AtFraction(50, 50, 30, 40), false, 15, 5},
		{"AtRect, a window", widget.AtRect(tui.Rect{X: 7, Y: 2, W: 10, H: 4}), true, 7, 2},
		{"AtFraction, a window", widget.AtFraction(50, 50, 30, 40), true, 15, 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			var content tui.Component = widget.NewBox(widget.NewButton("inside"), widget.WithTitle("FB"))
			if c.window {
				content = widget.NewWindowMod(content, widget.WithWindowMove())
			}
			f := widget.NewFloat(content, widget.WithAnchor(widget.AtLive(none, c.fallback)))
			host := widget.NewOverlayHost(widget.NewButton("base"))
			host.Attach(f)
			h := startApp(t, host, 30, 10)
			defer h.stop()
			h.onLoop(f.Show)
			h.settle()
			if x, y, ok := placedAt(h, "FB"); !ok || x != c.x || y != c.y {
				t.Fatalf("placed at (%d,%d) %v; want the fallback's (%d,%d):\n%s", x, y, ok, c.x, c.y, h.tb.String())
			}
			if f.Floating() {
				t.Fatal("a live-anchored float reports floating")
			}
		})
	}
}
