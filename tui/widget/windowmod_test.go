package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

func windowBounds(h *harness, w *core.WindowMod) tui.Rect {
	var r tui.Rect
	h.onLoop(func() { r = w.Core().Bounds() })
	return r
}

func TestWindowDragPreemptsEditorAndEscapeRestoresPolicy(t *testing.T) {
	ed := editor.NewEditor()
	w := core.NewWindowMod(ed, core.WithWindowMove(), core.WithWindowResize())
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 3, Y: 2, W: 14, H: 6})))
	host := core.NewOverlayHost(core.NewButton("background"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(func() { f.Show(); h.app.FocusInto(ed) })
	h.settle()
	h.inject(key('i'))
	h.barrier(nil)
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 6, Y: 4})
	h.barrier(nil)
	var capture bool
	h.onLoop(func() { capture = w.Context().HasPointerCapture() })
	if !capture {
		t.Fatal("the window did not reserve its drag before the Editor")
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 11, Y: 7})
	h.barrier(nil)
	if got := windowBounds(h, w); got != (tui.Rect{X: 8, Y: 5, W: 14, H: 6}) {
		t.Fatalf("moved bounds: %+v", got)
	}
	h.inject(key(tui.KeyEscape))
	h.barrier(nil)
	if got := windowBounds(h, w); got != (tui.Rect{X: 3, Y: 2, W: 14, H: 6}) {
		t.Fatalf("cancelled bounds: %+v", got)
	}
	var mode editor.EditorMode
	h.onLoop(func() { mode = ed.Core().Mode(); capture = w.Context().HasPointerCapture() })
	if mode != editor.ModeInsert || capture {
		t.Fatalf("Escape reached content or capture survived: mode=%v capture=%v", mode, capture)
	}
	// A noncaptured Escape retains the Editor's normal behavior.
	h.inject(key(tui.KeyEscape))
	h.barrier(nil)
	h.onLoop(func() { mode = ed.Core().Mode() })
	if mode != editor.ModeNormal {
		t.Fatalf("ordinary Escape was stolen: mode=%v", mode)
	}
}

func TestWindowResizeBindingAndOneCompletion(t *testing.T) {
	var changes []core.WindowChangedEvent
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowResize(),
		core.WithWindowResizeBinding(tui.ModCtrl, tui.MouseRight),
		core.WithWindowKey("notes"), core.WithWindowChanged(func(e core.WindowChangedEvent) { changes = append(changes, e) }))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 12, H: 5})))
	host := core.NewOverlayHost(core.NewText("base"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModCtrl | tui.ModShift, X: 5, Y: 4})
	h.barrier(nil)
	var captured bool
	h.onLoop(func() { captured = w.Context().HasPointerCapture() })
	if captured {
		t.Fatal("extra modifiers matched the configured exact chord")
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModCtrl | tui.ModCapsLock, X: 5, Y: 4})
	h.barrier(nil)
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseRight, X: 9, Y: 6})
	h.barrier(nil)
	if got := windowBounds(h, w); got != (tui.Rect{X: 2, Y: 2, W: 16, H: 7}) {
		t.Fatalf("resized bounds: %+v", got)
	}
	var count int
	h.onLoop(func() { count = len(changes) })
	if count != 0 {
		t.Fatal("drag motion published completion")
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 9, Y: 6})
	h.barrier(nil)
	h.onLoop(func() { captured = w.Context().HasPointerCapture() })
	if !captured {
		t.Fatal("an unrelated release ended resize")
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: 9, Y: 6})
	h.barrier(nil)
	h.onLoop(func() {
		if len(changes) != 1 || changes[0].Operation != core.WindowResize || changes[0].Key != "notes" {
			t.Errorf("completion events: %+v", changes)
		}
	})
}

func TestWindowMinimizeRetainsMountAndOldDescriptorsExpireOnReopen(t *testing.T) {
	model := core.NewMinimizedWindows()
	background := core.NewButton("background")
	ed := editor.NewEditor()
	w := core.NewWindowMod(ed, core.WithWindowMinimize(model), core.WithWindowClose(), core.WithWindowKey("editor"))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 18, H: 7})))
	host := core.NewOverlayHost(background)
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(func() { f.Show(); h.app.FocusInto(ed) })
	h.settle()
	var owner, child tui.NodeID
	var stale core.MinimizedWindow
	h.onLoop(func() {
		owner, child = w.NodeID(), ed.NodeID()
		if !w.Minimize() {
			t.Error("minimize refused")
		}
		if !h.app.FocusWithin(background) {
			t.Error("focus repair needed a taskbar")
		}
		entries := model.Entries()
		if len(entries) != 1 {
			t.Errorf("entries: %+v", entries)
			return
		}
		stale = entries[0]
		if ed.NodeID() != child || !ed.Context().Mounted() || ed.Context().Ctx().Err() != nil {
			t.Error("minimize ended the content lifetime")
		}
		if !stale.Restore() || w.NodeID() != owner || ed.NodeID() != child {
			t.Error("restore remounted content")
		}
		if len(model.Entries()) != 0 {
			t.Error("restore left a collector entry")
		}
		if !w.Minimize() || !w.Close() || f.Shown() {
			t.Error("close from minimized failed")
		}
		f.Show()
		if w.NodeID() == owner {
			t.Error("close/reopen retained the old mount identity")
		}
		if stale.Restore() || stale.Close() {
			t.Error("a stale descriptor controlled a new mount")
		}
	})
	h.settle()
	if !f.Visible() {
		t.Fatal("close left the reopened Float concealed")
	}
}

func TestWindowInputScopeDoesNotBlockApplicationOperations(t *testing.T) {
	model := core.NewMinimizedWindows()
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMinimize(model), core.WithWindowClose())
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 18, H: 7})))
	dialog := core.NewFloat(core.NewButton("confirm"), core.WithModal(true))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	host.Attach(dialog)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		w.Minimize()
		entry := model.Entries()[0]
		dialog.Show()
		if entry.InvokeInput(core.WindowRestoreAction{}) {
			t.Error("taskbar input escaped the modal scope")
		}
		if !entry.Restore() {
			t.Error("application session restore was input-confined")
		}
		if w.InvokeInput(core.WindowCloseAction{}) {
			t.Error("control close escaped the modal scope")
		}
		if !w.Close() {
			t.Error("application close-all was input-confined")
		}
	})
}

func TestWindowMaximizeWorkAreaAndRestorePreserveRequest(t *testing.T) {
	w := core.NewWindowMod(core.NewText("content"), core.WithWindowMove(), core.WithWindowMaximize(),
		core.WithWindowWorkArea(func(r tui.Rect) tui.Rect { r.H--; return r }))
	want := tui.Rect{X: 30, Y: 10, W: 15, H: 7}
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(want)))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		if !w.ToggleMaximize() {
			t.Error("maximize refused")
		}
	})
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{W: 50, H: 19}) {
		t.Fatalf("work-area maximize: %+v", got)
	}
	h.inject(tui.ResizeEvent{W: 20, H: 10})
	h.barrier(nil)
	h.onLoop(func() { w.ToggleMaximize() })
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 5, Y: 2, W: 15, H: 7}) {
		t.Fatalf("restored rect is not reachable: %+v", got)
	}
	h.inject(tui.ResizeEvent{W: 50, H: 20})
	h.barrier(nil)
	if got := windowBounds(h, w); got != want {
		t.Fatalf("layout rewrote the saved request: %+v", got)
	}
}

func TestWindowDisabledPointerAndNonPointerBeginDoNotAcquireCapture(t *testing.T) {
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMove(), core.WithWindowMoveBinding(tui.ModAlt, tui.MouseRight))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 16, H: 6})))
	base := core.NewButton("base")
	host := core.NewOverlayHost(base)
	host.Attach(f)
	h := startApp(t, host, 40, 15)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		action := core.WindowDragBeginAction{Operation: core.WindowMove, At: tui.Point{X: 1, Y: 1}}
		if w.InvokeInput(action) || w.Context().DoAction(action) {
			t.Error("non-pointer begin acquired an artificial pointer gesture")
		}
		w.Context().SetPointerPolicy(tui.PointerDisabled)
		h.app.FocusInto(base)
	})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, Mods: tui.ModAlt, X: 5, Y: 4})
	h.barrier(nil)
	h.onLoop(func() {
		if w.Context().HasPointerCapture() {
			t.Error("pointer-disabled owner reserved the drag")
		}
		if !h.app.FocusWithin(base) {
			t.Error("pointer-disabled reservation changed focus before capture was refused")
		}
	})
}

func TestWindowCaptureLossKeepsReachedGeometryWithoutCompletion(t *testing.T) {
	changes := 0
	w := core.NewWindowMod(editor.NewEditor(), core.WithWindowMove(), core.WithWindowChanged(func(core.WindowChangedEvent) { changes++ }))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 16, H: 6})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 40, 15)
	h.onLoop(f.Show)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Mods: tui.ModAlt, X: 5, Y: 4},
		tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: 9, Y: 7})
	h.barrier(nil)
	h.inject(tui.FocusEvent{Terminal: true, Gained: false})
	h.barrier(nil)
	if got := windowBounds(h, w); got != (tui.Rect{X: 6, Y: 5, W: 16, H: 6}) {
		t.Fatalf("capture loss rolled back geometry: %+v", got)
	}
	h.onLoop(func() {
		if w.Context().HasPointerCapture() || changes != 0 {
			t.Errorf("capture=%v completions=%d", w.Context().HasPointerCapture(), changes)
		}
	})
}

// A window floated AtFraction is at its percentages of the overlay, follows a resize of it, and a
// keyboard move takes it from there to a rectangle of cells.
func TestAWindowAtAFractionFollowsTheOverlay(t *testing.T) {
	w := core.NewWindowMod(core.NewText("notes"), core.WithWindowMove(), core.WithWindowResize())
	f := core.NewFloat(w, core.WithAnchor(core.AtFraction(10, 20, 40, 50)))
	host := core.NewOverlayHost(core.NewText("base"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 5, Y: 4, W: 20, H: 10}) {
		t.Fatalf("at 10,20,40,50 of 50×20: %+v", got)
	}
	var requested tui.Rect
	h.onLoop(func() { requested = w.Core().RequestedBounds() })
	if requested != (tui.Rect{X: 5, Y: 4, W: 20, H: 10}) {
		t.Errorf("requested %+v, want the fraction's cells", requested)
	}
	h.tb.InjectResize(100, 40)
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 10, Y: 8, W: 40, H: 20}) {
		t.Fatalf("after the overlay doubled: %+v", got)
	}
	h.onLoop(func() {
		if !w.Core().MoveBy(5, 0) {
			t.Error("moveBy refused")
		}
	})
	h.settle()
	if got := windowBounds(h, w); got != (tui.Rect{X: 15, Y: 8, W: 40, H: 20}) {
		t.Errorf("moved: %+v", got)
	}
}

// A window maximised to a work area that reserves a row is not floating, so a native style draws
// no shadow for it, though its content does not fill the whole area; restored, it floats again.
func TestAMaximisedWindowIsNotFloating(t *testing.T) {
	w := core.NewWindowMod(core.NewText("content"), core.WithWindowMove(), core.WithWindowMaximize(),
		core.WithWindowWorkArea(func(r tui.Rect) tui.Rect { r.H--; return r }))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 5, Y: 2, W: 15, H: 5})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 30, 10)
	h.onLoop(f.Show)
	h.settle()
	var floating bool
	h.onLoop(func() { floating = f.Floating() })
	if !floating {
		t.Fatal("a window at a rectangle is not floating")
	}
	h.onLoop(func() { w.ToggleMaximize() })
	h.settle()
	h.onLoop(func() { floating = f.Floating() })
	if floating {
		t.Fatal("a window maximised to its work area is floating")
	}
	h.onLoop(func() { w.ToggleMaximize() })
	h.settle()
	h.onLoop(func() { floating = f.Floating() })
	if !floating {
		t.Fatal("a window restored from maximised is not floating")
	}
}

// TestAFloatLandsTheSameWithAndWithoutAWindowMod: one placement for every Float. An AtRect that
// overhangs the area is kept inside it, whether or not a WindowMod is bound in the Float
// (tui-widget-shell-D1).
func TestAFloatLandsTheSameWithAndWithoutAWindowMod(t *testing.T) {
	overhang := tui.Rect{X: 40, Y: 15, W: 20, H: 6}
	plain := core.NewFloat(core.NewButton("plain"), core.WithAnchor(core.AtRect(overhang)))
	managed := core.NewFloat(core.NewWindowMod(core.NewButton("managed"), core.WithWindowMove()),
		core.WithAnchor(core.AtRect(overhang)))
	host := core.NewOverlayHost(core.NewButton("background"))
	host.Attach(plain)
	host.Attach(managed)
	h := startApp(t, host, 50, 20)
	defer h.stop()
	h.onLoop(func() { plain.Show(); managed.Show() })
	h.settle()
	var p, m tui.Rect
	h.onLoop(func() { p, m = plain.ContentRect(), managed.ContentRect() })
	want := tui.Rect{X: 30, Y: 14, W: 20, H: 6}
	if p != want || m != want {
		t.Errorf("AtRect %+v in 50x20: plain at %+v, managed at %+v; want both at %+v", overhang, p, m, want)
	}
}
