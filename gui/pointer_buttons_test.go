package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/tui"
)

// A drag reports the held button: the middle and the right as themselves, and of a chord the
// first in primary, middle, right order (the order presses are reported in).
func TestADragReportsTheHeldButton(t *testing.T) {
	m := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: 100, H: 40}}
	for _, c := range []struct {
		name string
		held pointer.Buttons
		want tui.MouseButton
	}{
		{"middle", pointer.ButtonTertiary, tui.MouseMiddle},
		{"right", pointer.ButtonSecondary, tui.MouseRight},
		{"middle and right", pointer.ButtonTertiary | pointer.ButtonSecondary, tui.MouseMiddle},
		{"left and right", pointer.ButtonPrimary | pointer.ButtonSecondary, tui.MouseLeft},
	} {
		t.Run(c.name, func(t *testing.T) {
			var p pointerState
			p.translate(pointer.Event{Kind: pointer.Press, Buttons: c.held, Position: f32.Pt(20, 40)}, m)
			got := p.translate(pointer.Event{Kind: pointer.Drag, Buttons: c.held, Position: f32.Pt(40, 40)}, m)
			if len(got) != 1 || got[0].Kind != tui.MouseMotion || got[0].Button != c.want {
				t.Fatalf("drag with %s held = %+v; want one motion with button %v", c.name, got, c.want)
			}
		})
	}
	if b := buttonOf(0); b != tui.MouseNone {
		t.Fatalf("buttonOf(none) = %v, want MouseNone", b)
	}
}
