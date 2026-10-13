package widget

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// A widget's SetToolTip, before mount and after: hovered, it shows its card; the same text again
// leaves the card open; a new text closes it, the widget repainting so the App sees the change.
func TestBaseToolTipShowsAndAChangeClosesIt(t *testing.T) {
	label := NewText("the prose font")
	label.SetToolTip("the Rendered view and the HTML preview") // before mount
	h := startAppInternal(t, label, 60, 4)
	defer h.stopInternal()
	shown := func(want bool) bool {
		for range 300 {
			h.syncInternal()
			if strings.Contains(h.tb.String(), "the Rendered view") == want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 2, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !shown(true) {
		t.Fatalf("no card on hover:\n%s", h.tb.String())
	}
	h.onLoopInternal(func() { label.SetToolTip("the Rendered view and the HTML preview") })
	time.Sleep(50 * time.Millisecond)
	if !shown(true) {
		t.Fatal("the same text again closed the card")
	}
	h.onLoopInternal(func() { label.SetToolTip("changed") })
	if !shown(false) {
		t.Fatalf("a new text left the old card:\n%s", h.tb.String())
	}
	var tip string
	h.onLoopInternal(func() { tip = label.ToolTip() })
	if tip != "changed" {
		t.Errorf("ToolTip() = %q", tip)
	}
}
