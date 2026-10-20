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
