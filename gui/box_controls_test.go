package gui

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// A Box with window controls is painted natively: its cells, then one dot per control
// (TitleWindowControls) on its title row, in the control's colour, centred on its cells, at the
// title bar's radius. A Box without them is declined, so the usual frame stays cell-drawn.
func TestABoxsWindowControlsAreTheTitleBarsDots(t *testing.T) {
	box := core.NewBox(core.NewText("body"), core.WithTitle("pane"), core.WithBorder(style.BorderNormal))
	box.SetWindowControls([]core.WindowOperation{core.WindowMinimize, core.WindowClose})
	app, sb := styledApp(t, box, 24, 4, NativeStyle())
	var id tui.NodeID
	var controls []core.TabControl
	waitUntil(t, "the box's controls laid out", func() bool {
		onLoop(t, app, func() { id, controls = box.NodeID(), box.TitleWindowControls() })
		return len(controls) == 2
	})
	waitUntil(t, "the box's native placement", func() bool {
		return slices.ContainsFunc(sb.placements(), func(p tui.NativePlacement) bool { return p.Node == id })
	})
	rc := paintOn(t, app, sb, placementOf(t, sb, id))
	var dots []PaintCall
	for _, p := range rc.Calls {
		if p.Op == "FillEllipse" {
			dots = append(dots, p)
		}
	}
	if len(dots) != len(controls) {
		t.Fatalf("%d dots; want one per control %+v: %+v", len(dots), controls, rc.Calls)
	}
	radius := min(float32(16)*0.32, float32(8)*1.2) // the title bar's, on 8×16 cells
	for i, ctl := range controls {
		cx, cy := dots[i].Rect.X+dots[i].Rect.W/2, dots[i].Rect.Y+dots[i].Rect.H/2
		if want := (float32(ctl.X) + float32(ctl.W)/2) * 8; cx != want || cy != 8 || dots[i].Brush.Color != WindowControlColor(ctl.Op) {
			t.Errorf("dot %d: centre (%v, %v) colour %v; want (%v, 8) in %v", i, cx, cy, dots[i].Brush.Color, want, WindowControlColor(ctl.Op))
		}
		if r := dots[i].Rect.W / 2; r != radius {
			t.Errorf("dot %d: radius %v; want the title bar's %v", i, r, radius)
		}
	}

	onLoop(t, app, func() { box.SetWindowControls(nil) })
	waitUntil(t, "the box declined once it has no controls", func() bool {
		return !slices.ContainsFunc(sb.placements(), func(p tui.NativePlacement) bool { return p.Node == id })
	})
	if view, _, ok := paintBox(core.NewBox(core.NewText("b"))); ok || view != nil {
		t.Error("a Box without window controls is painted natively; it should stay cell-drawn")
	}
}
