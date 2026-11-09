package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

func TestEditorMenuActionsWorkAfterFocusMovesWithoutStealingItBack(t *testing.T) {
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText("one\ntwo")))
	menu := core.NewTextInput()
	root := tui.NewFlex(tui.Vertical)
	root.Add(ed, menu)
	h := startApp(t, root, 30, 8)
	h.onLoop(func() {
		ed.Context().RequestFocus()
		menu.Context().RequestFocus() // a menu took focus before invoking its command
		if !menu.Context().Focused() || ed.Context().Focused() {
			t.Error("fixture did not move focus off the editor")
		}
		ed.Core().Copy()
		ed.Core().Cut()
		ed.Core().Paste()
		if ed.Core().Value() != "one\ntwo" || !menu.Context().Focused() {
			t.Errorf("menu commands returned %q or took focus from the menu", ed.Core().Value())
		}
		if got, linewise := ed.Core().Register(); got != "one" || !linewise {
			t.Errorf("menu commands did not use the editor register: (%q, %t)", got, linewise)
		}
	})
}
