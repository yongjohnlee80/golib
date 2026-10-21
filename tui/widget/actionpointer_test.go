package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// An action arrives as a value or as a pointer to one, and every widget handler takes both, as
// WindowCore always did: a Split given &ResizeStepAction moves its divider as the value does.
// Before, the value-only handlers ignored the pointer silently.
func TestAPointerActionIsTheSameAction(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8) // horizontal: DX is its axis
	a0 := aCellsOn(t, h, s)
	var handled bool
	h.onLoop(func() {
		handled = s.Context().DoAction(&widget.ResizeStepAction{DX: 2, Unit: widget.StepCells})
	})
	h.barrier(sh)
	if !handled {
		t.Fatal("a Split refused &ResizeStepAction, which it handles as a value")
	}
	h.waitFor("the pointer step moved the divider", func() bool { return aCellsOn(t, h, s) == a0+2 })
}
