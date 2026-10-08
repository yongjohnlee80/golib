package widget_test

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
	"testing"
)

func windowBounds(h *harness, w *widget.WindowMod) tui.Rect {
	var r tui.Rect
	h.onLoop(func() { r = w.Core().Bounds() })
	return r
}

func TestWindowDragPreemptsEditorAndEscapeRestoresPolicy(t *testing.T) {
	editor := widget.NewEditor()
	w := widget.NewWindowMod(editor, widget.WithWindowMove(), widget.WithWindowResize())
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 3, Y: 2, W: 14, H: 6})))
	host := widget.NewOverlayHost(widget.NewButton("background"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(func() { f.Show(); h.app.FocusInto(editor) })
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
	var mode widget.EditorMode
	h.onLoop(func() { mode = editor.Mode(); capture = w.Context().HasPointerCapture() })
	if mode != widget.ModeInsert || capture {
		t.Fatalf("Escape reached content or capture survived: mode=%v capture=%v", mode, capture)
	}
	// A noncaptured Escape retains the Editor's normal behavior.
	h.inject(key(tui.KeyEscape))
	h.barrier(nil)
	h.onLoop(func() { mode = editor.Mode() })
	if mode != widget.ModeNormal {
		t.Fatalf("ordinary Escape was stolen: mode=%v", mode)
	}
}

func TestWindowResizeBindingAndOneCompletion(t *testing.T) {
	var changes []widget.WindowChangedEvent
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowResize(),
		widget.WithWindowResizeBinding(tui.ModCtrl, tui.MouseRight),
		widget.WithWindowKey("notes"), widget.WithWindowChanged(func(e widget.WindowChangedEvent) { changes = append(changes, e) }))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 12, H: 5})))
	host := widget.NewOverlayHost(widget.NewText("base"))
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
		if len(changes) != 1 || changes[0].Operation != widget.WindowResize || changes[0].Key != "notes" {
			t.Errorf("completion events: %+v", changes)
		}
	})
}

func TestWindowMinimizeRetainsMountAndOldDescriptorsExpireOnReopen(t *testing.T) {
	model := widget.NewMinimizedWindows()
	background := widget.NewButton("background")
	editor := widget.NewEditor()
	w := widget.NewWindowMod(editor, widget.WithWindowMinimize(model), widget.WithWindowClose(), widget.WithWindowKey("editor"))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 18, H: 7})))
	host := widget.NewOverlayHost(background)
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(func() { f.Show(); h.app.FocusInto(editor) })
	h.settle()
	var owner, child tui.NodeID
	var stale widget.MinimizedWindow
	h.onLoop(func() {
		owner, child = w.NodeID(), editor.NodeID()
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
		if editor.NodeID() != child || !editor.Context().Mounted() || editor.Context().Ctx().Err() != nil {
			t.Error("minimize ended the content lifetime")
		}
		if !stale.Restore() || w.NodeID() != owner || editor.NodeID() != child {
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
	model := widget.NewMinimizedWindows()
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMinimize(model), widget.WithWindowClose())
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 18, H: 7})))
	dialog := widget.NewFloat(widget.NewButton("confirm"), widget.WithModal(true))
	host := widget.NewOverlayHost(widget.NewButton("base"))
	host.Attach(f)
	host.Attach(dialog)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		w.Minimize()
		entry := model.Entries()[0]
		dialog.Show()
		if entry.InvokeInput(widget.WindowRestoreAction{}) {
			t.Error("taskbar input escaped the modal scope")
		}
		if !entry.Restore() {
			t.Error("application session restore was input-confined")
		}
		if w.InvokeInput(widget.WindowCloseAction{}) {
			t.Error("control close escaped the modal scope")
		}
		if !w.Close() {
			t.Error("application close-all was input-confined")
		}
	})
}

func TestWindowMaximizeWorkAreaAndRestorePreserveRequest(t *testing.T) {
	w := widget.NewWindowMod(widget.NewText("content"), widget.WithWindowMove(), widget.WithWindowMaximize(),
		widget.WithWindowWorkArea(func(r tui.Rect) tui.Rect { r.H--; return r }))
	want := tui.Rect{X: 30, Y: 10, W: 15, H: 7}
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(want)))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove(), widget.WithWindowMoveBinding(tui.ModAlt, tui.MouseRight))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 16, H: 6})))
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	host.Attach(f)
	h := startApp(t, host, 40, 15)
	h.onLoop(f.Show)
	h.settle()
	h.onLoop(func() {
		action := widget.WindowDragBeginAction{Operation: widget.WindowMove, At: tui.Point{X: 1, Y: 1}}
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
	w := widget.NewWindowMod(widget.NewEditor(), widget.WithWindowMove(), widget.WithWindowChanged(func(widget.WindowChangedEvent) { changes++ }))
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 16, H: 6})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
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
