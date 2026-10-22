package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A Float has one window behaviour owner: a second core asking for its target while the first
// is live is refused, loudly, as WindowCore.Bind refuses two owners of one container. The point
// map the target hands back maps through where the owner is laid out, and passes a point
// through unchanged once the owner is laid out nowhere (hidden).
func TestAFloatHasOneWindowOwnerAndItsPointMapFollowsTheOwner(t *testing.T) {
	mod := NewWindowMod(NewText("x"), WithWindowMove())
	f := NewFloat(NewBox(mod)) // the box's frame lays the owner out off the Float's origin
	host := NewOverlayHost(NewBox(NewButton("base")))
	host.Attach(f)
	ih := startAppInternal(t, host, 40, 10)
	defer ih.stopInternal()

	ih.onLoopInternal(func() {
		f.Show()
	})
	ih.onLoopInternal(func() {
		if f.windowTarget == nil || f.windowTarget.core != mod.Core() || !mod.Core().live() {
			t.Fatalf("the WindowMod's live core is not the Float's owner: %+v", f.windowTarget)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Error("a second core took the Float's target while its owner is live")
				}
			}()
			f.WindowTargetFor(NewWindowCore(WithWindowMove()), mod.Context())
		}()

		_, toTarget := f.WindowTargetFor(mod.Core(), mod.Context()) // the owner again: no panic
		at := tui.Point{X: 1, Y: 1}
		r, ok := f.ctx.ResolveAnchor(mod.Context().NodeAnchor())
		if !ok || r.X == 0 && r.Y == 0 {
			t.Fatalf("the owner is shown at %+v (resolved %v): want it resolved off the Float's origin, so the map is observed", r, ok)
		}
		if got := toTarget(at); got != (tui.Point{X: r.X + 1, Y: r.Y + 1}) {
			t.Errorf("a point in the owner's frame maps to %+v, want %+v", got, tui.Point{X: r.X + 1, Y: r.Y + 1})
		}
		mod.SetVisible(false)
		if got := toTarget(at); got != at {
			t.Errorf("with the owner hidden, a point maps to %+v, want it unchanged", got)
		}
	})
}
