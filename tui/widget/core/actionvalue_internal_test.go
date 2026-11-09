package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// ptrOnly is an action whose methods have a pointer receiver: its value is no Action.
type ptrOnly struct{}

func (*ptrOnly) ActionID() tui.ActionID { return "test.ptrOnly" }

// actionValue gives a pointer to an action value as the value, and leaves alone a value, a nil
// pointer, and a pointer whose element is no Action.
func TestActionValueTakesThePointedAtValue(t *testing.T) {
	step := ResizeStepAction{DX: 2}
	if got := actionValue(&step); got != tui.Action(step) {
		t.Errorf("actionValue(&step) = %#v, want the value %#v", got, step)
	}
	if got := actionValue(step); got != tui.Action(step) {
		t.Errorf("actionValue(step) = %#v, want it unchanged", got)
	}
	var nilStep *ResizeStepAction
	if got := actionValue(nilStep); got != tui.Action(nilStep) {
		t.Errorf("actionValue(nil pointer) = %#v, want the nil pointer unchanged", got)
	}
	p := &ptrOnly{}
	if got := actionValue(p); got != tui.Action(p) {
		t.Errorf("actionValue(&ptrOnly) = %#v, want the pointer unchanged: its element is no Action", got)
	}
}
