package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A trapping overlay's own node is a focus stop only while its content holds nothing focusable,
// read live: a modal Float's layer and a popup's layer both stop accepting focus when a control in
// their content becomes enabled, and accept it again when it is disabled. Frozen at Show, the
// Float's stop stayed a phantom Tab stop beside the control; the popup's was one always.
func TestATrappingOverlaysFallbackStopFollowsItsContent(t *testing.T) {
	floatBtn, popupBtn := NewButton("ok"), NewButton("go")
	floatBtn.SetEnabled(false)
	f := NewFloat(NewBox(floatBtn), WithModal(true))
	base := NewButton("base")
	host := NewOverlayHost(NewBox(base))
	host.Attach(f)
	ih := startAppInternal(t, host, 40, 10)
	defer ih.stopInternal()

	ih.onLoopInternal(func() {
		f.Show()
		if !f.layer.AcceptsFocus() {
			t.Error("a modal Float whose only control is disabled: its layer refuses focus, so the trap has no stop")
		}
		floatBtn.SetEnabled(true)
		if f.layer.AcceptsFocus() {
			t.Error("the Float's content gained an enabled control, and its layer still accepts focus: a phantom Tab stop")
		}
		floatBtn.SetEnabled(false)
		if !f.layer.AcceptsFocus() {
			t.Error("the control was disabled again, and the Float's layer does not take the stop back")
		}
		f.Hide()
	})

	ih.onLoopInternal(func() {
		p, err := host.openPopupAt("t", base, NewBox(popupBtn), tui.Point{}, nil)
		if err != nil {
			t.Fatalf("openPopupAt: %v", err)
		}
		if p.AcceptsFocus() {
			t.Error("a popup whose panel holds an enabled control accepts focus itself: a phantom Tab stop")
		}
		popupBtn.SetEnabled(false)
		if !p.AcceptsFocus() {
			t.Error("a popup whose panel holds nothing focusable refuses focus, so Escape cannot reach it")
		}
	})
}
