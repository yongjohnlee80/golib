package gui

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A floating panel (a Drawer moved off its edge floats AtFraction; one at Tui.Center is centred)
// gets a soft shadow around its content, under its border; a panel docked at an edge (a Drawer's
// Left, Right, Top or Bottom) gets none.
func TestFloatingPanelsGetAShadowAndDockedOnesDoNot(t *testing.T) {
	cases := []struct {
		name   string
		anchor widget.Anchor
		shadow bool
	}{
		{"moved off its edge", widget.AtFraction(25, 25, 50, 50), true},
		{"centred", widget.Center, true},
		{"dragging", widget.AtRect(tui.Rect{X: 4, Y: 2, W: 12, H: 4}), true},
		{"docked left", widget.Left, false},
		{"docked bottom", widget.Bottom, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			panel := widget.NewFloat(widget.NewBox(widget.NewText("panel"), widget.WithTitle("P")), widget.WithAnchor(c.anchor))
			host := widget.NewOverlayHost(widget.NewText("beneath"))
			host.Attach(panel)
			app, sb := styledApp(t, host, 30, 10, NativeStyle())
			onLoop(t, app, panel.Show)
			waitUntil(t, "the panel", func() bool { return panel.ContentRect().W > 0 && sb.Flushes() > 1 })
			paint := NativeStyle().painterFor(panel)
			if paint == nil {
				t.Fatal("NativeStyle has no painter for a Float")
			}
			if _, _, ok := paint(panel); ok != c.shadow {
				t.Fatalf("the painter took the panel: %v; want %v", ok, c.shadow)
			}
			var placed *tui.NativePlacement
			waitUntil(t, "a frame after the panel's layout", func() bool {
				for _, p := range sb.placements() {
					if p.Node == panel.NodeID() {
						placed = &p
					}
				}
				return !c.shadow || placed != nil
			})
			if !c.shadow {
				if placed != nil {
					t.Fatalf("a docked panel has a native placement: %+v", *placed)
				}
				return
			}
			if placed.Scope != tui.ScopeChrome {
				t.Fatalf("scope %v; want chrome, beneath the panel's cells", placed.Scope)
			}
			// Under the border: every cell of the panel's content is painted after the shadow.
			var content tui.Rect
			onLoop(t, app, func() { content = panel.ContentRect() })
			covered := 0
			for _, r := range placed.Covered {
				covered += r.Intersect(content).W * r.Intersect(content).H
			}
			if covered != content.W*content.H {
				t.Fatalf("the panel's cells cover %d of its %d cells over the shadow", covered, content.W*content.H)
			}
			rc := paintOn(t, app, sb, *placed)
			cs := Size{W: 8, H: 16}
			card := Rect{X: float32(content.X) * cs.W, Y: float32(content.Y) * cs.H, W: float32(content.W) * cs.W, H: float32(content.H) * cs.H}
			rings := 0
			for _, call := range rc.Calls {
				if call.Op != "FillRRect" {
					t.Fatalf("the shadow drew a %s", call.Op)
				}
				if a := call.Brush.Color.A; a == 0 || a == 0xff {
					t.Fatalf("a shadow ring of opacity %d; want translucent", a)
				}
				if call.Rect.X >= card.X || call.Rect.X+call.Rect.W <= card.X+card.W || call.Rect.Y+call.Rect.H <= card.Y+card.H {
					t.Fatalf("ring %+v does not reach past the panel %+v", call.Rect, card)
				}
				rings++
			}
			if rings == 0 {
				t.Fatal("no shadow drawn")
			}
		})
	}
}
