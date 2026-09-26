package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestEditorMenuActionsWorkAfterFocusMovesWithoutStealingItBack(t *testing.T) {
	ed := widget.NewEditor(widget.WithInitialText("one\ntwo"))
	menu := widget.NewTextInput()
	root := tui.NewFlex(tui.Vertical)
	root.Add(ed, menu)
	h := startApp(t, root, 30, 8)
	h.onLoop(func() {
		ed.Context().RequestFocus()
		menu.Context().RequestFocus() // a menu took focus before invoking its command
		if !menu.Context().Focused() || ed.Context().Focused() {
			t.Error("fixture did not move focus off the editor")
		}
		ed.Copy()
		ed.Cut()
		ed.Paste()
		if ed.Value() != "one\ntwo" || !menu.Context().Focused() {
			t.Errorf("menu commands returned %q or took focus from the menu", ed.Value())
		}
		if got, linewise := ed.Register(); got != "one" || !linewise {
			t.Errorf("menu commands did not use the editor register: (%q, %t)", got, linewise)
		}
	})
}
