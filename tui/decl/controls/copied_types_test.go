package controls_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A copy of TextField or Popup whose Build makes another widget, as a style or a consumer's
// variant does, wears a palette and is torn down without a panic: the copied Restyle and
// Destroyed hooks leave a widget that is not theirs alone. Before, Restyle asserted
// *widget.TextInput and Destroyed *popupNode, unchecked.
func TestACopiedTextFieldOrPopupDoesNotPanic(t *testing.T) {
	text := func(s string) func(tuidecl.Build) (tui.Component, []string, error) {
		return func(tuidecl.Build) (tui.Component, []string, error) { return widget.NewText(s), nil, nil }
	}
	field := controls.TextField
	field.Name, field.Build, field.Enums, field.Ctor = "MyField", text("my field"), nil, nil
	popup := controls.Popup
	popup.Name, popup.Build, popup.Ctor = "MyPopup", text("my popup"), nil
	popup.Children = tuidecl.Arity{} // the copy's Build makes a leaf: no content child to count

	s := decltest.Run(t, 40, 4,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nFlex { direction: Tui.Vertical\n"+
			" MyField { palette.base: \"white\"; palette.text: \"black\" }\n MyPopup { }\n}")),
		tuidecl.Types(field, popup))
	s.WaitForText(t, "my field")
	s.WaitForText(t, "my popup")
	done := make(chan error, 1)
	s.Program.Post(func() { done <- s.Program.Tree().Destroy() })
	if err := <-done; err != nil {
		t.Errorf("tearing the copies down: %v", err)
	}
}
