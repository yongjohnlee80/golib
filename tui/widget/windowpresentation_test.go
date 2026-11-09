package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

func TestWindowTaskbarRestoresByKeyboardWithoutInlineChildRemoval(t *testing.T) {
	model := core.NewMinimizedWindows()
	ed := editor.NewEditor()
	w := core.NewWindowMod(ed, core.WithWindowMinimize(model), core.WithWindowLabel("Notes"))
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 15, H: 6})))
	bar := core.NewWindowTaskbar(model)
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, bar)
	dock.Add(core.NewButton("background"))
	host := core.NewOverlayHost(dock)
	host.Attach(f)
	h := startApp(t, host, 40, 15)
	h.onLoop(func() { f.Show() })
	h.settle()
	h.onLoop(func() { w.Minimize() })
	h.settle()
	h.waitFor("taskbar entry", func() bool { return strings.Contains(h.grid(), "Notes") })
	var original tui.NodeID
	h.onLoop(func() { original = ed.NodeID(); h.app.FocusInto(bar) })
	h.inject(key(tui.KeyEnter))
	h.barrier(nil)
	h.onLoop(func() {
		if w.Core().State().Minimized || len(model.Entries()) != 0 || ed.NodeID() != original {
			t.Error("taskbar restore lost the retained window")
		}
	})
	h.inject(key(tui.KeyTab))
	h.barrier(nil)
	h.onLoop(func() {
		if bar.Len() != 0 {
			t.Error("taskbar did not reconcile the removed entry")
		}
	})
}

func TestWindowButtonsRemainSeparateAndInvokeTheSharedOperation(t *testing.T) {
	body := tui.NewFlex(tui.Vertical)
	ed := editor.NewEditor()
	w := core.NewWindowMod(body, core.WithWindowMaximize(), core.WithWindowClose())
	buttons := core.NewWindowButtons(w.Core(), core.WithWindowButtonLabels("Close", "Zoom", "Hide"))
	body.Add(buttons)
	body.AddWeighted(ed, 1)
	f := core.NewFloat(w, core.WithAnchor(core.AtRect(tui.Rect{X: 2, Y: 2, W: 20, H: 8})))
	host := core.NewOverlayHost(core.NewButton("base"))
	host.Attach(f)
	h := startApp(t, host, 50, 20)
	h.onLoop(f.Show)
	h.settle()
	h.wantContains("Zoom")
	var zoom tui.Component
	h.onLoop(func() {
		for _, child := range buttons.All() {
			if b, ok := child.(*core.Button); ok && b.Label() == "Zoom" {
				zoom = b
			}
		}
		h.app.FocusInto(zoom)
	})
	h.inject(key(tui.KeyEnter))
	h.barrier(nil)
	h.onLoop(func() {
		if !w.Core().State().Maximized {
			t.Error("presentation did not invoke the shared maximize")
		}
	})
}
