package widget_test

import (
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

func TestWindowResizeByUsesPinnedHandleAndStateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		handle      core.Handle
		start, want tui.Rect
		dw, dh      int
	}{
		{"right", core.HandleBottomLeft, tui.Rect{X: 15, Y: 2, W: 5, H: 4}, tui.Rect{X: 12, Y: 2, W: 8, H: 4}, 3, 0},
		{"bottom", core.HandleTopRight, tui.Rect{X: 2, Y: 6, W: 6, H: 4}, tui.Rect{X: 2, Y: 3, W: 6, H: 7}, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := core.NewMinimizedWindows()
			w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMove(), core.WithWindowResize(),
				core.WithWindowResizeHandle(tc.handle), core.WithWindowMaximize(), core.WithWindowMinimize(model))
			f := core.NewFloat(w, core.WithAnchor(core.AtRect(tc.start)))
			host := core.NewOverlayHost(core.NewButton("base"))
			host.Attach(f)
			h := startApp(t, host, 20, 10)
			h.onLoop(f.Show)
			h.settle()
			h.onLoop(func() {
				if !w.ResizeBy(tc.dw, tc.dh) {
					t.Error("pinned growth refused")
				}
			})
			h.settle()
			if got := windowBounds(h, w); got != tc.want {
				t.Fatalf("growth: %+v, want %+v", got, tc.want)
			}
			h.onLoop(func() { w.MoveBy(100, 100) })
			h.settle()
			got := windowBounds(h, w)
			if got.X+got.W != 20 || got.Y+got.H != 10 {
				t.Fatalf("MoveBy did not clamp: %+v", got)
			}
			h.onLoop(func() {
				w.ToggleMaximize()
				if w.MoveBy(1, 1) || w.ResizeBy(1, 1) {
					t.Error("step accepted while maximized")
				}
				w.ToggleMaximize()
				w.Minimize()
				if w.MoveBy(1, 1) || w.ResizeBy(1, 1) {
					t.Error("step accepted while minimized")
				}
			})
		})
	}
}

func TestWindowInlineRemountCloseCannotCloseTheNewMount(t *testing.T) {
	var f *core.Float
	var w *core.WindowMod
	var events []core.WindowChangedEvent
	reentered := false
	w = core.NewWindowMod(editor.NewEditor(), core.WithWindowMove(), core.WithWindowChanged(func(e core.WindowChangedEvent) { events = append(events, e) }),
		core.WithWindowCloseHandler(func() bool { f.Hide(); f.Show(); reentered = w.MoveBy(1, 0); return true }))
	f = core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 30, 12)
	h.onLoop(f.Show)
	h.settle()
	var old tui.NodeID
	h.onLoop(func() {
		old = w.NodeID()
		if !w.Close() {
			t.Error("close refused")
		}
	})
	h.settle()
	h.onLoop(func() {
		if w.NodeID() == old || w.Core().State().Closed || reentered {
			t.Error("close transition stamped or reentered the remounted core")
		}
		if len(events) != 1 || events[0].Owner != old {
			t.Errorf("close events: %+v", events)
		}
		if !w.MoveBy(1, 0) {
			t.Error("fresh mount became inert")
		}
	})
}

func TestWindowClampedOutAndBackPreservesRequestAndNoCompletion(t *testing.T) {
	changes := 0
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMove(), core.WithWindowChanged(func(core.WindowChangedEvent) { changes++ }))
	request := tui.Rect{X: 30, Y: 10, W: 5, H: 3}
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(request)))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 20, 10)
	h.onLoop(f.Show)
	h.settle()
	r := windowBounds(h, w)
	x, y := r.X+1, r.Y+1
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: x - 3, Y: y - 2})
	h.barrier(nil)
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.barrier(nil)
	h.onLoop(func() {
		if got := w.Core().RequestedBounds(); got != request || changes != 0 {
			t.Errorf("request=%+v, completions=%d", got, changes)
		}
	})
}

func TestWindowStepDuringDragUsesTheCheckpointGeometry(t *testing.T) {
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMove(), core.WithWindowResize())
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 40, 15)
	h.onLoop(f.Show)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 12, Y: 6})
	h.barrier(nil)
	h.onLoop(func() { w.MoveBy(1, 0) })
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 3, Y: 2, W: 10, H: 4}) {
		t.Fatalf("step used dragged geometry: %+v", got)
	}
}

func TestWindowRestoreNormalizesExternallyRevealedFloat(t *testing.T) {
	model := core.NewMinimizedWindows()
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMinimize(model))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 30, 12)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		w.Minimize()
		f.SetVisible(true)
		if !w.Restore() || w.Core().State().Minimized || len(model.Entries()) != 0 {
			t.Error("already-visible restore left stale minimize state")
		}
	})
}

type windowReservationProbe struct {
	*core.WindowMod
	refuseBegin, refuseCancel, relaxedEscape bool
	accepted                                 bool
}

func (w *windowReservationProbe) HandleAction(inv tui.ActionInvocation) bool {
	switch inv.Action.(type) {
	case core.WindowDragBeginAction:
		if w.refuseBegin {
			return false
		}
		w.accepted = w.WindowMod.HandleAction(inv)
		return w.accepted
	case core.WindowDragCancelAction:
		if w.refuseCancel {
			return false
		}
	}
	return w.WindowMod.HandleAction(inv)
}
func (w *windowReservationProbe) ResolveCaptureEscape(e tui.KeyEvent) (tui.Action, bool) {
	if w.relaxedEscape {
		return core.WindowDragCancelAction{}, true
	}
	return w.WindowMod.ResolveCaptureEscape(e)
}

type windowFocusProbe struct {
	core.Base
	onFocus func()
	presses int
}

func (p *windowFocusProbe) AcceptsFocus() bool { return true }
func (p *windowFocusProbe) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: 10, H: 4})
}
func (p *windowFocusProbe) Render(tui.Surface) {}
func (p *windowFocusProbe) HandleEvent(ev tui.Event) bool {
	if e, ok := ev.(tui.FocusEvent); ok && e.Gained && p.onFocus != nil {
		p.onFocus()
	}
	if e, ok := ev.(tui.MouseEvent); ok && e.Kind == tui.MousePress {
		p.presses++
		return true
	}
	return false
}

func TestWindowBeginRechecksHiddenAncestorAfterFocus(t *testing.T) {
	child := &windowFocusProbe{}
	w := &windowReservationProbe{WindowMod: core.NewWindowMod(child, core.WithWindowMove())}
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	child.onFocus = func() { f.SetVisible(false) }
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 30, 12)
	h.onLoop(f.Show)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4})
	h.barrier(nil)
	h.onLoop(func() {
		if w.accepted || child.presses != 0 {
			t.Errorf("hidden begin accepted=%v replayed=%d", w.accepted, child.presses)
		}
	})
}

func TestWindowBeginPinsClosedAndAncestorVisibilityIndependently(t *testing.T) {
	for _, name := range []string{"closed", "ancestor"} {
		t.Run(name, func(t *testing.T) {
			child := &windowFocusProbe{}
			w := &windowReservationProbe{WindowMod: core.NewWindowMod(child, core.WithWindowMove(),
				core.WithWindowCloseHandler(func() bool { return true }))}
			f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
			host := core.NewOverlayHost(core.NewButton("base"))
			host.Attach(f)
			ancestor := core.NewBox(host)
			var root tui.Component = host
			if name == "ancestor" {
				root = ancestor
				child.onFocus = func() { ancestor.SetVisible(false) }
			} else {
				child.onFocus = func() { w.Close() }
			}
			h := startApp(t, root, 30, 12)
			h.onLoop(f.Show)
			h.settle()
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4})
			h.barrier(nil)
			h.onLoop(func() {
				if w.accepted {
					t.Error("focus transition still acquired capture")
				}
			})
		})
	}
}

func TestWindowRefusedReservationNeverReplaysThePress(t *testing.T) {
	child := &windowFocusProbe{}
	w := &windowReservationProbe{WindowMod: core.NewWindowMod(child, core.WithWindowMove()), refuseBegin: true}
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 30, 12)
	h.onLoop(f.Show)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4})
	h.barrier(nil)
	h.onLoop(func() {
		if child.presses != 0 {
			t.Error("refused reservation replayed onto content")
		}
	})
}

type windowRecordedEditor struct {
	*editor.Editor
	keys []tui.KeyEvent
}

func (e *windowRecordedEditor) HandleEvent(ev tui.Event) bool {
	if k, ok := ev.(tui.KeyEvent); ok {
		e.keys = append(e.keys, k)
	}
	return e.Editor.HandleEvent(ev)
}

func TestWindowCaptureEscapeFilteringAndRefusalAreNotReplayed(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "filter", true: "refusal"}[refuse], func(t *testing.T) {
			ed := &windowRecordedEditor{Editor: editor.NewEditor()}
			w := &windowReservationProbe{WindowMod: core.NewWindowMod(ed, core.WithWindowMove()), refuseCancel: refuse, relaxedEscape: !refuse}
			f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
			host := core.NewOverlayHost(core.NewButton("base"))
			host.Attach(f)
			h := startApp(t, host, 30, 12)
			h.onLoop(func() { f.Show(); h.app.FocusInto(ed) })
			h.settle()
			if refuse {
				h.inject(key('i'))
				h.barrier(nil)
			}
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4})
			h.barrier(nil)
			h.onLoop(func() { ed.keys = nil })
			if refuse {
				h.inject(key(tui.KeyEscape))
				h.barrier(nil)
				h.onLoop(func() {
					if ed.Core().Mode() != editor.ModeInsert || len(ed.keys) != 1 || ed.keys[0].Code != barrierKey {
						t.Errorf("refused Escape replayed: mode=%v keys=%+v", ed.Core().Mode(), ed.keys)
					}
				})
			} else {
				h.inject(tui.KeyEvent{Code: tui.KeyEscape, Mods: tui.ModShift}, tui.KeyEvent{Code: tui.KeyEscape, Kind: tui.KeyRelease}, key('i'))
				h.barrier(nil)
				h.onLoop(func() {
					shift, release, ordinary := false, false, false
					for _, k := range ed.keys {
						shift = shift || (k.Code == tui.KeyEscape && k.Mods == tui.ModShift)
						release = release || (k.Code == tui.KeyEscape && k.Kind == tui.KeyRelease)
						ordinary = ordinary || k.Code == 'i'
					}
					if !shift || !release || !ordinary || !w.Context().HasPointerCapture() {
						t.Errorf("filtered keys=%+v capture=%v", ed.keys, w.Context().HasPointerCapture())
					}
				})
			}
		})
	}
}

func TestWindowHiddenAncestorFocusRepairUsesFreshVisibilityWithoutLayout(t *testing.T) {
	hiddenChild := core.NewButton("hidden first")
	ancestor := core.NewBox(hiddenChild)
	eligible := core.NewButton("eligible")
	current := core.NewButton("current")
	root := tui.NewFlex(tui.Vertical)
	root.Add(ancestor, eligible, current)
	h := startApp(t, root, 30, 12)
	h.settle()
	h.onLoop(func() {
		h.app.FocusInto(current)
		ancestor.SetVisible(false)
		current.SetVisible(false)
		current.Context().InvalidateFocusability()
		if h.app.FocusWithin(hiddenChild) || !h.app.FocusWithin(eligible) {
			t.Error("repair nominated stale placement under a hidden ancestor")
		}
	})
}

func TestWindowHandleActionOriginsAreInputScopedBothWays(t *testing.T) {
	model := core.NewMinimizedWindows()
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMinimize(model))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	dialog := core.NewFloat(core.NewButton("confirm"), core.WithModal(true))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	host.Attach(dialog)
	h := startApp(t, host, 30, 12)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		w.Minimize()
		dialog.Show()
		for _, origin := range []tui.ActionOrigin{tui.OriginKey, tui.OriginPointer, tui.OriginUser} {
			if w.HandleAction(tui.ActionInvocation{Action: core.WindowRestoreAction{}, Origin: origin}) {
				t.Errorf("input origin %v escaped scope", origin)
			}
		}
		if !w.HandleAction(tui.ActionInvocation{Action: core.WindowRestoreAction{}, Origin: tui.OriginProgrammatic}) {
			t.Error("application origin was confined")
		}
		dialog.Hide()
		w.Minimize()
		if !w.HandleAction(tui.ActionInvocation{Action: core.WindowRestoreAction{}, Origin: tui.OriginKey}) {
			t.Error("eligible input origin refused")
		}
	})
}

func TestWindowUnmatchedRightPressDoesNotFocusContent(t *testing.T) {
	base := core.NewButton("base")
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMove())
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := core.NewOverlayHost(base)
	host.Attach(f)
	h := startApp(t, host, 30, 12)
	h.onLoop(func() { f.Show(); h.app.FocusInto(base) })
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 5, Y: 4})
	h.barrier(nil)
	h.onLoop(func() {
		if !h.app.FocusWithin(base) {
			t.Error("unmatched right press acquired focus")
		}
	})
}

type windowPressAction struct{ preview int }

func (windowPressAction) ActionID() tui.ActionID { return "fixture.window.press" }

type windowResolverFixture struct {
	core.Base
	child            tui.Component
	counts, previews []int
	owner            tui.NodeID
}

func (w *windowResolverFixture) Init(ctx *tui.Context) { w.Base.Init(ctx); ctx.Mount(w.child) }
func (w *windowResolverFixture) Layout(c tui.Constraints) tui.Size {
	s := w.Context().LayoutChild(w.child, c)
	w.Context().PlaceChild(w.child, tui.Rect{W: s.W, H: s.H})
	return s
}
func (w *windowResolverFixture) Render(tui.Surface) {}
func (w *windowResolverFixture) ResolvePointerPress(e tui.MouseEvent) (tui.Action, bool) {
	return windowPressAction{preview: e.Count}, true
}
func (w *windowResolverFixture) HandleAction(inv tui.ActionInvocation) bool {
	if e, ok := inv.Source.(tui.MouseEvent); ok {
		w.counts = append(w.counts, e.Count)
		w.previews = append(w.previews, inv.Action.(windowPressAction).preview)
		w.owner = w.NodeID()
	}
	return true
}

func TestWindowReservationNearestOwnerOrdinalAndConfinement(t *testing.T) {
	inner := &windowResolverFixture{child: core.NewButton("target")}
	outer := &windowResolverFixture{child: inner}
	h := startAppOpts(t, outer, 30, 12, tui.WithDoubleClickWindow(time.Second))
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0, Count: 999},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0},
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0, Count: 999})
	h.barrier(nil)
	h.onLoop(func() {
		if len(outer.counts) != 0 || len(inner.counts) != 2 || inner.counts[0] != 1 || inner.counts[1] != 2 || inner.previews[0] != 1 || inner.previews[1] != 2 || inner.owner != inner.NodeID() {
			t.Errorf("owners/counts: outer=%v inner=%v preview=%v owner=%v", outer.counts, inner.counts, inner.previews, inner.owner)
		}
	})

	dialog := core.NewFloat(core.NewButton("modal"), core.WithModal(true))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(dialog)
	scopeOuter := &windowResolverFixture{child: host}
	h2 := startApp(t, scopeOuter, 30, 12)
	h2.onLoop(dialog.Show)
	h2.settle()
	h2.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 15, Y: 6})
	h2.barrier(nil)
	h2.onLoop(func() {
		if len(scopeOuter.counts) != 0 {
			t.Error("reservation consulted an ancestor outside the modal ceiling")
		}
	})
}
