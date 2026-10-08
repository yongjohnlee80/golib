package widget_test

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
	"testing"
	"time"
)

func TestWindowResizeByUsesPinnedHandleAndStateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		handle      widget.Handle
		start, want tui.Rect
		dw, dh      int
	}{
		{"right", widget.HandleBottomLeft, tui.Rect{X: 15, Y: 2, W: 5, H: 4}, tui.Rect{X: 12, Y: 2, W: 8, H: 4}, 3, 0},
		{"bottom", widget.HandleTopRight, tui.Rect{X: 2, Y: 6, W: 6, H: 4}, tui.Rect{X: 2, Y: 3, W: 6, H: 7}, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := widget.NewMinimizedWindows()
			w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove(), widget.WithWindowResize(),
				widget.WithWindowResizeHandle(tc.handle), widget.WithWindowMaximize(), widget.WithWindowMinimize(model))
			f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tc.start)))
			host := widget.NewOverlayHost(widget.NewButton("base"))
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
	var f *widget.Float
	var w *widget.WindowMod
	var events []widget.WindowChangedEvent
	reentered := false
	w = widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove(), widget.WithWindowChanged(func(e widget.WindowChangedEvent) { events = append(events, e) }),
		widget.WithWindowCloseHandler(func() bool { f.Hide(); f.Show(); reentered = w.MoveBy(1, 0); return true }))
	f = widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove(), widget.WithWindowChanged(func(widget.WindowChangedEvent) { changes++ }))
	request := tui.Rect{X: 30, Y: 10, W: 5, H: 3}
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(request)))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove(), widget.WithWindowResize())
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
	model := widget.NewMinimizedWindows()
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMinimize(model))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
	*widget.WindowMod
	refuseBegin, refuseCancel, relaxedEscape bool
	accepted                                 bool
}

func (w *windowReservationProbe) HandleAction(inv tui.ActionInvocation) bool {
	switch inv.Action.(type) {
	case widget.WindowDragBeginAction:
		if w.refuseBegin {
			return false
		}
		w.accepted = w.WindowMod.HandleAction(inv)
		return w.accepted
	case widget.WindowDragCancelAction:
		if w.refuseCancel {
			return false
		}
	}
	return w.WindowMod.HandleAction(inv)
}
func (w *windowReservationProbe) ResolveCaptureEscape(e tui.KeyEvent) (tui.Action, bool) {
	if w.relaxedEscape {
		return widget.WindowDragCancelAction{}, true
	}
	return w.WindowMod.ResolveCaptureEscape(e)
}

type windowFocusProbe struct {
	widget.Base
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
	w := &windowReservationProbe{WindowMod: widget.NewWindowMod(child, widget.WithWindowMove())}
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	child.onFocus = func() { f.SetVisible(false) }
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
			w := &windowReservationProbe{WindowMod: widget.NewWindowMod(child, widget.WithWindowMove(),
				widget.WithWindowCloseHandler(func() bool { return true }))}
			f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
			host := widget.NewOverlayHost(widget.NewButton("base"))
			host.Attach(f)
			ancestor := widget.NewBox(host)
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
	w := &windowReservationProbe{WindowMod: widget.NewWindowMod(child, widget.WithWindowMove()), refuseBegin: true}
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
	*widget.Editor
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
			editor := &windowRecordedEditor{Editor: widget.NewEditor()}
			w := &windowReservationProbe{WindowMod: widget.NewWindowMod(editor, widget.WithWindowMove()), refuseCancel: refuse, relaxedEscape: !refuse}
			f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
			host := widget.NewOverlayHost(widget.NewButton("base"))
			host.Attach(f)
			h := startApp(t, host, 30, 12)
			h.onLoop(func() { f.Show(); h.app.FocusInto(editor) })
			h.settle()
			if refuse {
				h.inject(key('i'))
				h.barrier(nil)
			}
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4})
			h.barrier(nil)
			h.onLoop(func() { editor.keys = nil })
			if refuse {
				h.inject(key(tui.KeyEscape))
				h.barrier(nil)
				h.onLoop(func() {
					if editor.Mode() != widget.ModeInsert || len(editor.keys) != 1 || editor.keys[0].Code != barrierKey {
						t.Errorf("refused Escape replayed: mode=%v keys=%+v", editor.Mode(), editor.keys)
					}
				})
			} else {
				h.inject(tui.KeyEvent{Code: tui.KeyEscape, Mods: tui.ModShift}, tui.KeyEvent{Code: tui.KeyEscape, Kind: tui.KeyRelease}, key('i'))
				h.barrier(nil)
				h.onLoop(func() {
					shift, release, ordinary := false, false, false
					for _, k := range editor.keys {
						shift = shift || (k.Code == tui.KeyEscape && k.Mods == tui.ModShift)
						release = release || (k.Code == tui.KeyEscape && k.Kind == tui.KeyRelease)
						ordinary = ordinary || k.Code == 'i'
					}
					if !shift || !release || !ordinary || !w.Context().HasPointerCapture() {
						t.Errorf("filtered keys=%+v capture=%v", editor.keys, w.Context().HasPointerCapture())
					}
				})
			}
		})
	}
}

func TestWindowHiddenAncestorFocusRepairUsesFreshVisibilityWithoutLayout(t *testing.T) {
	hiddenChild := widget.NewButton("hidden first")
	ancestor := widget.NewBox(hiddenChild)
	eligible := widget.NewButton("eligible")
	current := widget.NewButton("current")
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
	model := widget.NewMinimizedWindows()
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMinimize(model))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	dialog := widget.NewFloat(widget.NewButton("confirm"), widget.WithModal(true))
	host := widget.NewOverlayHost(widget.NewButton("base"))
	host.Attach(f)
	host.Attach(dialog)
	h := startApp(t, host, 30, 12)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		w.Minimize()
		dialog.Show()
		for _, origin := range []tui.ActionOrigin{tui.OriginKey, tui.OriginPointer, tui.OriginUser} {
			if w.HandleAction(tui.ActionInvocation{Action: widget.WindowRestoreAction{}, Origin: origin}) {
				t.Errorf("input origin %v escaped scope", origin)
			}
		}
		if !w.HandleAction(tui.ActionInvocation{Action: widget.WindowRestoreAction{}, Origin: tui.OriginProgrammatic}) {
			t.Error("application origin was confined")
		}
		dialog.Hide()
		w.Minimize()
		if !w.HandleAction(tui.ActionInvocation{Action: widget.WindowRestoreAction{}, Origin: tui.OriginKey}) {
			t.Error("eligible input origin refused")
		}
	})
}

func TestWindowUnmatchedRightPressDoesNotFocusContent(t *testing.T) {
	base := widget.NewButton("base")
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove())
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 10, H: 4})))
	host := widget.NewOverlayHost(base)
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
	widget.Base
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
	inner := &windowResolverFixture{child: widget.NewButton("target")}
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

	dialog := widget.NewFloat(widget.NewButton("modal"), widget.WithModal(true))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
